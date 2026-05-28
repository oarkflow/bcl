package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestAnomalySessionHijackingProgressesAndPreBlocks(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	resp, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"impossible_travel":true},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "300")

	now = now.Add(60 * time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"impossible_travel":false},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "240")

	now = now.Add(5*time.Minute + time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"token_reuse":true},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "300")

	now = now.Add(5*time.Minute + time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"mfa_bypass_attempt":true},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "terminate_session", "identity_session/terminate_session", "1800")
}

func TestAnomalyGeoCommerceBusinessDataAndAPI(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	blockedCountry, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/payments/authorize", "application/json", `{"actor":{"id":"buyer-1"},"geo":{"country":"IR","region":"blocked"},"payment":{"amount_vs_avg":1,"velocity_10m":1,"new_method":false,"shipping_country":"IR","billing_country":"IR"}}`, nil))
	assertAnomalyEnforcement(t, blockedCountry, http.StatusForbidden, "block", "geo_region_policy/block_region", "3600")

	paymentSpike, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/payments/authorize", "application/json", `{"actor":{"id":"buyer-2"},"geo":{"country":"US","region":"us"},"payment":{"amount_vs_avg":6,"velocity_10m":1,"new_method":false,"shipping_country":"US","billing_country":"US"}}`, nil))
	assertAnomalyEnforcement(t, paymentSpike, http.StatusPaymentRequired, "challenge", "commerce_risk/challenge_payment", "600")

	firstSupport, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"tenant-user-1"},"tenant":{"verified":false},"support":{"ticket_spam":true,"severity":1}}`, nil))
	if firstSupport.StatusCode != http.StatusOK || firstSupport.Header.Get("X-Condition-Action") != "business_rule_violation" {
		t.Fatalf("first support anomaly status=%d action=%q", firstSupport.StatusCode, firstSupport.Header.Get("X-Condition-Action"))
	}
	secondSupport, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"tenant-user-1"},"tenant":{"verified":false},"support":{"ticket_spam":true,"severity":1}}`, nil))
	assertAnomalyEnforcement(t, secondSupport, http.StatusConflict, "hold", "business_abuse/hold_business", "1800")

	data, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/data/query", "application/json", `{"actor":{"id":"analyst-1"},"tenant":{"region":"us","verified":true},"geo":{"country":"DE","region":"eu"},"data":{"pii_export":true,"bulk_rows":25000,"restricted_dataset":true,"cross_region":true}}`, nil))
	assertAnomalyEnforcement(t, data, http.StatusLocked, "quarantine", "data_exfiltration/quarantine_access", "1800")

	var apiResp *http.Response
	for i := 0; i < 5; i++ {
		apiResp, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/api/resource?mode=fail", "", "", nil))
	}
	if apiResp.Header.Get("X-Condition-Action") != "escalate" || !strings.Contains(apiResp.Header.Get("X-Condition-State"), "api_health/escalate_api_health") {
		t.Fatalf("api escalation action=%q state=%q", apiResp.Header.Get("X-Condition-Action"), apiResp.Header.Get("X-Condition-State"))
	}
}

func TestAnomalyOperationalEndpointsReturnJSON(t *testing.T) {
	runtime, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)
	for _, path := range []string{"/_threat-model", "/_state?actor=alice", "/_events", "/_actions", "/_incidents", "/_coverage", "/_readiness"} {
		resp, body := doTestRequest(t, app, mustRequest(http.MethodGet, path, "", "", nil))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, body)
		}
		var decoded any
		if err := json.Unmarshal([]byte(body), &decoded); err != nil {
			t.Fatalf("%s did not return JSON: %v body=%s", path, err, body)
		}
	}
}

func assertAnomalyEnforcement(t *testing.T, resp *http.Response, status int, action, state, retryAfter string) {
	t.Helper()
	if resp.StatusCode != status || resp.Header.Get("X-Condition-Action") != action || !strings.Contains(resp.Header.Get("X-Condition-State"), state) || resp.Header.Get("Retry-After") != retryAfter {
		t.Fatalf("status=%d action=%q state=%q retry=%q, want status=%d action=%q state~%q retry=%q",
			resp.StatusCode,
			resp.Header.Get("X-Condition-Action"),
			resp.Header.Get("X-Condition-State"),
			resp.Header.Get("Retry-After"),
			status,
			action,
			state,
			retryAfter,
		)
	}
}

func doTestRequest(t *testing.T, app *fiber.App, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}
