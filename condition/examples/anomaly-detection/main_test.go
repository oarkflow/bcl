package main

import (
	"encoding/json"
	"fmt"
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

	resp, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "300")

	now = now.Add(60 * time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "240")

	now = now.Add(5*time.Minute + time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "step_up", "identity_session/step_up", "300")

	now = now.Add(5*time.Minute + time.Second)
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"}}`, nil))
	assertAnomalyEnforcement(t, resp, http.StatusUnauthorized, "terminate_session", "identity_session/terminate_session", "1800")
}

func TestAnomalySessionRiskIgnoresRequestBodySecurityClaims(t *testing.T) {
	runtime, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	resp, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"body-only"},"session":{"impossible_travel":true,"token_reuse":true,"mfa_bypass_attempt":true},"device":{"changed":true,"trusted":false},"network":{"asn_changed":true,"ip_reputation":99}}`, nil))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Condition-Action") != "healthy" {
		t.Fatalf("body-owned session risk should not trigger: status=%d action=%q", resp.StatusCode, resp.Header.Get("X-Condition-Action"))
	}
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

	restrictedAdmin, _ := doTestRequest(t, app, mustRequest(http.MethodGet, "/admin/risk-console", "", "", map[string]string{
		"X-Actor":   "analyst-eu",
		"X-Role":    "analyst",
		"X-Region":  "eu",
		"X-Country": "DE",
	}))
	assertAnomalyEnforcement(t, restrictedAdmin, http.StatusForbidden, "block", "geo_region_policy/block_region", "3600")

	paymentSpike, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/payments/authorize", "application/json", `{"actor":{"id":"buyer-2"},"geo":{"country":"US","region":"us"},"payment":{"amount_vs_avg":6,"velocity_10m":1,"new_method":false,"shipping_country":"US","billing_country":"US"}}`, nil))
	assertAnomalyEnforcement(t, paymentSpike, http.StatusPaymentRequired, "challenge", "commerce_risk/challenge_payment", "600")

	firstSupport, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"tenant-user-1"},"tenant":{"verified":false},"support":{"ticket_spam":true,"severity":1}}`, nil))
	if firstSupport.StatusCode != http.StatusOK || firstSupport.Header.Get("X-Condition-Action") != "business_rule_violation" {
		t.Fatalf("first support anomaly status=%d action=%q", firstSupport.StatusCode, firstSupport.Header.Get("X-Condition-Action"))
	}
	secondSupport, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"tenant-user-1"},"tenant":{"verified":false},"support":{"ticket_spam":true,"severity":1}}`, nil))
	assertAnomalyEnforcement(t, secondSupport, http.StatusConflict, "hold", "business_abuse/hold_business", "1800")

	reopenLoop, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"support-loop-1"},"tenant":{"verified":true},"support":{"reopen_count":4,"severity":3}}`, nil))
	if reopenLoop.StatusCode != http.StatusOK || reopenLoop.Header.Get("X-Condition-Action") != "business_rule_violation" {
		t.Fatalf("support reopen anomaly status=%d action=%q", reopenLoop.StatusCode, reopenLoop.Header.Get("X-Condition-Action"))
	}

	logisticsMismatch, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/logistics/shipments", "application/json", `{"actor":{"id":"shipper-1"},"logistics":{"route_country_mismatch":true,"restricted_destination":false,"hazmat":false,"carrier_certified":true,"reroute_attempts":0}}`, nil))
	if logisticsMismatch.StatusCode != http.StatusOK || logisticsMismatch.Header.Get("X-Condition-Action") != "business_rule_violation" {
		t.Fatalf("logistics mismatch anomaly status=%d action=%q", logisticsMismatch.StatusCode, logisticsMismatch.Header.Get("X-Condition-Action"))
	}

	policyChange, _ := doTestRequest(t, app, mustRequest(http.MethodGet, "/admin/risk-console", "application/json", `{"actor":{"id":"risk-admin","role":"risk_admin"},"geo":{"country":"US","region":"us"},"business":{"business_hours":true,"policy_change":true,"maintenance_window":false}}`, nil))
	if policyChange.StatusCode != http.StatusOK || policyChange.Header.Get("X-Condition-Action") != "business_rule_violation" {
		t.Fatalf("policy change anomaly status=%d action=%q", policyChange.StatusCode, policyChange.Header.Get("X-Condition-Action"))
	}

	data, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/data/query", "application/json", `{"actor":{"id":"analyst-1"},"tenant":{"region":"us","verified":true},"geo":{"country":"DE","region":"eu"},"data":{"pii_export":true,"bulk_rows":25000,"restricted_dataset":true,"cross_region":true}}`, nil))
	assertAnomalyEnforcement(t, data, http.StatusLocked, "quarantine", "data_exfiltration/quarantine_access", "1800")

	apiBehavior, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/api/resource", "application/json", `{"actor":{"id":"api-client"},"api":{"unusual_method_mix":true,"endpoint_burst":25,"error_ratio":0.1}}`, nil))
	if apiBehavior.StatusCode != http.StatusOK || apiBehavior.Header.Get("X-Condition-Action") != "api_behavior_anomaly" {
		t.Fatalf("api behavior anomaly status=%d action=%q", apiBehavior.StatusCode, apiBehavior.Header.Get("X-Condition-Action"))
	}

	var apiResp *http.Response
	for i := 0; i < 5; i++ {
		apiResp, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/api/resource?mode=fail", "", "", nil))
	}
	if apiResp.Header.Get("X-Condition-Action") != "escalate" || !strings.Contains(apiResp.Header.Get("X-Condition-State"), "api_health/escalate_api_health") {
		t.Fatalf("api escalation action=%q state=%q", apiResp.Header.Get("X-Condition-Action"), apiResp.Header.Get("X-Condition-State"))
	}
}

func TestAnomalySecondWaveDomains(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	account, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/accounts/update-profile", "application/json", `{"actor":{"id":"acct-1"},"account":{"mfa_disabled":false}}`, nil))
	assertAnomalyEnforcement(t, account, http.StatusUnauthorized, "step_up", "account_takeover/step_up_account", "600")

	now = now.Add(10*time.Minute + time.Second)
	fraud, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/fraud/claim", "application/json", `{"actor":{"id":"claimant-1"},"fraud":{"mule_indicator":true,"duplicate_identity":true}}`, nil))
	assertAnomalyEnforcement(t, fraud, http.StatusPaymentRequired, "challenge", "fraud_risk/challenge_claim", "900")

	insiderFirst, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/insider/export", "application/json", `{"actor":{"id":"employee-1"},"insider":{"privileged_export":true,"sensitive_dataset":true,"records_exported":15000}}`, nil))
	if insiderFirst.StatusCode != http.StatusOK || insiderFirst.Header.Get("X-Condition-Action") != "insider_anomaly" {
		t.Fatalf("first insider anomaly status=%d action=%q", insiderFirst.StatusCode, insiderFirst.Header.Get("X-Condition-Action"))
	}
	insiderSecond, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/insider/export", "application/json", `{"actor":{"id":"employee-1"},"insider":{"privileged_export":true,"sensitive_dataset":true,"records_exported":15000}}`, nil))
	assertAnomalyEnforcement(t, insiderSecond, http.StatusLocked, "quarantine", "insider_risk/quarantine_insider_export", "1800")

	bot, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/bots/challenge", "application/json", `{"actor":{"id":"bot-client"},"bot":{"credential_stuffing":true,"captcha_failures":4,"request_entropy":92}}`, nil))
	assertAnomalyEnforcement(t, bot, http.StatusTooManyRequests, "challenge", "bot_defense/challenge_bot", "120")

	vendorFirst, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/supply-chain/vendor-change", "application/json", `{"actor":{"id":"buyer-ops"},"vendor":{"bank_changed":true,"new_payee":true,"purchase_order_vs_avg":6}}`, nil))
	if vendorFirst.StatusCode != http.StatusOK || vendorFirst.Header.Get("X-Condition-Action") != "notify" {
		t.Fatalf("first vendor anomaly status=%d action=%q", vendorFirst.StatusCode, vendorFirst.Header.Get("X-Condition-Action"))
	}
	vendorSecond, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/supply-chain/vendor-change", "application/json", `{"actor":{"id":"buyer-ops"},"vendor":{"bank_changed":true,"new_payee":true,"purchase_order_vs_avg":6}}`, nil))
	assertAnomalyEnforcement(t, vendorSecond, http.StatusConflict, "hold", "vendor_risk/hold_vendor_change", "3600")

	compliance, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/compliance/screen", "application/json", `{"actor":{"id":"screening-1"},"compliance":{"sanctions_hit":true,"pep_hit":true}}`, nil))
	assertAnomalyEnforcement(t, compliance, http.StatusUnavailableForLegalReasons, "hold", "compliance_risk/hold_compliance", "3600")

	blockedByPre, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/accounts/update-profile", "application/json", `{"actor":{"id":"acct-1"},"account":{"mfa_disabled":false}}`, nil))
	assertAnomalyEnforcement(t, blockedByPre, http.StatusUnauthorized, "step_up", "account_takeover/step_up_account", "600")
}

func TestAnomalyAggregationDomains(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	var registration *http.Response
	for i := 1; i <= 4; i++ {
		body := fmt.Sprintf(`{"actor":{"id":"reg-%d"},"account":{"email_domain":"example%d.com"}}`, i, i)
		registration, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/accounts/register", "application/json", body, nil))
	}
	if registration.Header.Get("X-Condition-Action") != "registration_velocity_anomaly" {
		t.Fatalf("registration velocity action=%q", registration.Header.Get("X-Condition-Action"))
	}

	var traffic *http.Response
	for i := 0; i < 5; i++ {
		traffic, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/traffic/ok", "", "", nil))
	}
	if traffic.Header.Get("X-Condition-Action") != "traffic_anomaly" {
		t.Fatalf("traffic spike action=%q", traffic.Header.Get("X-Condition-Action"))
	}

	var missing *http.Response
	for i := 0; i < 3; i++ {
		missing, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/traffic/missing", "", "", nil))
	}
	if missing.Header.Get("X-Condition-Action") != "ratio_anomaly" && missing.Header.Get("X-Condition-Action") != "success_drop_anomaly" {
		t.Fatalf("unexpected 4xx aggregation action=%q", missing.Header.Get("X-Condition-Action"))
	}

	var failed *http.Response
	for i := 0; i < 3; i++ {
		failed, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/traffic/fail", "", "", nil))
	}
	if failed.Header.Get("X-Condition-Action") != "consecutive_failure_anomaly" {
		t.Fatalf("consecutive failure action=%q", failed.Header.Get("X-Condition-Action"))
	}

	var signal *http.Response
	for i := 1; i <= 5; i++ {
		body := fmt.Sprintf(`{"actor":{"id":"signal-%d"},"signal":{"source":"worker-%d"}}`, i, i)
		signal, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/signals/logs", "application/json", body, nil))
	}
	if signal.Header.Get("X-Condition-Action") != "signal_anomaly" {
		t.Fatalf("generic signal aggregation action=%q", signal.Header.Get("X-Condition-Action"))
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
		if path == "/_threat-model" {
			model, ok := decoded.(map[string]any)
			assets, _ := model["assets"].([]any)
			if !ok || model["id"] != "anomaly_detection" || len(assets) == 0 {
				t.Fatalf("threat model should come from BCL block, got %#v", decoded)
			}
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
