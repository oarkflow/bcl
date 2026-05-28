package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestCompletePlatformBadLoginUsesProgressiveJitter(t *testing.T) {
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	var resp *http.Response
	for i := 1; i <= 3; i++ {
		resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	}
	assertEnforcement(t, resp, http.StatusTooManyRequests, "rate_limit", "rate_limit_2m", "120")

	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusTooManyRequests, "rate_limit", "rate_limit_2m", "120")

	now = now.Add(2*time.Minute + time.Second)
	if err := runtime.store.DeleteExpiredChainStates(context.Background(), time.Now().UTC().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
		assertGraceFailure(t, resp)
	}
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusTooManyRequests, "rate_limit", "rate_limit_5m", "300")

	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusTooManyRequests, "rate_limit", "rate_limit_5m", "300")

	now = now.Add(5*time.Minute + time.Second)
	if err := runtime.store.DeleteExpiredChainStates(context.Background(), time.Now().UTC().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
		assertGraceFailure(t, resp)
	}
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusForbidden, "block", "block_30m", "1800")

	now = now.Add(30*time.Minute + time.Second)
	if err := runtime.store.DeleteExpiredChainStates(context.Background(), time.Now().UTC().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
		assertGraceFailure(t, resp)
	}
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusForbidden, "suspend", "suspend_24h", "86400")

	now = now.Add(24*time.Hour + time.Second)
	if err := runtime.store.DeleteExpiredChainStates(context.Background(), time.Now().UTC().Add(365*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
		assertGraceFailure(t, resp)
	}
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil))
	assertEnforcement(t, resp, http.StatusLocked, "ban", "lock_ban", "")

	eventsResp, eventsBody := doTestRequest(t, app, mustRequest(http.MethodGet, "/_events?actor=alice", "", "", nil))
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("_events status=%d body=%s", eventsResp.StatusCode, eventsBody)
	}
	var failedLoginEvents []map[string]any
	if err := json.Unmarshal([]byte(eventsBody), &failedLoginEvents); err != nil {
		t.Fatalf("_events JSON decode failed: %v body=%s", err, eventsBody)
	}
	gotFailedLogins := 0
	for _, event := range failedLoginEvents {
		if event["chain"] == "account_risk_chain" && event["event"] == "failed_login" {
			gotFailedLogins++
		}
	}
	if gotFailedLogins != 15 {
		t.Fatalf("blocked cooldown attempts should not create fresh failed_login events, got %d events: %s", gotFailedLogins, eventsBody)
	}

	stateResp, stateBody := doTestRequest(t, app, mustRequest(http.MethodGet, "/_state?actor=alice", "", "", nil))
	if stateResp.StatusCode != http.StatusOK {
		t.Fatalf("_state status=%d body=%s", stateResp.StatusCode, stateBody)
	}
	if !strings.Contains(stateBody, "failed_login_escalation") || !strings.Contains(stateBody, "lock_ban") {
		t.Fatalf("_state missing escalation state: %s", stateBody)
	}
}

func assertGraceFailure(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("X-Condition-State") != "" || resp.Header.Get("Retry-After") != "" {
		t.Fatalf("grace failure should remain a normal failed login, status=%d action=%q state=%q retry=%q",
			resp.StatusCode,
			resp.Header.Get("X-Condition-Action"),
			resp.Header.Get("X-Condition-State"),
			resp.Header.Get("Retry-After"),
		)
	}
}

func assertEnforcement(t *testing.T, resp *http.Response, status int, action, step, retryAfter string) {
	t.Helper()
	if resp.StatusCode != status || resp.Header.Get("X-Condition-Action") != action || !strings.Contains(resp.Header.Get("X-Condition-State"), step) || resp.Header.Get("Retry-After") != retryAfter {
		t.Fatalf("status=%d action=%q state=%q retry=%q, want status=%d action=%q step=%q retry=%q",
			resp.StatusCode,
			resp.Header.Get("X-Condition-Action"),
			resp.Header.Get("X-Condition-State"),
			resp.Header.Get("Retry-After"),
			status,
			action,
			step,
			retryAfter,
		)
	}
}

func TestCompletePlatformSuccessfulLoginResetsFailureWindow(t *testing.T) {
	runtime, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	for i := 0; i < 2; i++ {
		doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"bad"}`, nil))
	}
	resp, _ := doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"correct"}`, nil))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Condition-Action") != "successful_login" {
		t.Fatalf("successful login status=%d action=%q", resp.StatusCode, resp.Header.Get("X-Condition-Action"))
	}
	resp, _ = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"bad"}`, nil))
	if resp.StatusCode == http.StatusTooManyRequests || resp.Header.Get("X-Condition-Action") == "rate_limit" {
		t.Fatalf("reset did not clear failure window: status=%d action=%q", resp.StatusCode, resp.Header.Get("X-Condition-Action"))
	}
}

func TestCompletePlatformCredentials(t *testing.T) {
	runtime, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	resp, body := doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"correct"}`, nil))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Condition-Action") != "successful_login" {
		t.Fatalf("valid JSON credential status=%d action=%q body=%s", resp.StatusCode, resp.Header.Get("X-Condition-Action"), body)
	}

	resp, body = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/x-www-form-urlencoded", "grant_type=password&username=bob&password=correct", nil))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Condition-Action") != "successful_login" {
		t.Fatalf("valid form credential status=%d action=%q body=%s", resp.StatusCode, resp.Header.Get("X-Condition-Action"), body)
	}

	resp, body = doTestRequest(t, app, mustRequest(http.MethodPost, "/login", "application/json", `{"username":"charlie","password":"correct"}`, nil))
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("X-Condition-Action") != "failed_login" {
		t.Fatalf("unknown credential status=%d action=%q body=%s", resp.StatusCode, resp.Header.Get("X-Condition-Action"), body)
	}
}

func TestCompletePlatformAdminUnexpected4xxAndErrorEscalation(t *testing.T) {
	runtime, err := newRuntime()
	if err != nil {
		t.Fatal(err)
	}
	app := newApp(runtime)

	adminResp, _ := doTestRequest(t, app, mustRequest(http.MethodGet, "/admin/reports", "", "", map[string]string{"X-Actor": "analyst-1", "X-Role": "analyst"}))
	if adminResp.StatusCode != http.StatusForbidden || adminResp.Header.Get("X-Condition-Action") != "admin_denied" {
		t.Fatalf("admin denial status=%d action=%q", adminResp.StatusCode, adminResp.Header.Get("X-Condition-Action"))
	}

	missingResp, _ := doTestRequest(t, app, mustRequest(http.MethodGet, "/documents/missing", "", "", nil))
	if missingResp.StatusCode != http.StatusNotFound || missingResp.Header.Get("X-Condition-Action") != "unexpected_4xx" {
		t.Fatalf("unexpected 4xx status=%d action=%q", missingResp.StatusCode, missingResp.Header.Get("X-Condition-Action"))
	}

	var endpointResp *http.Response
	for i := 0; i < 5; i++ {
		endpointResp, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/fail/endpoint", "", "", nil))
	}
	if endpointResp.Header.Get("X-Condition-Action") != "escalate" {
		t.Fatalf("endpoint 5xx action=%q, want escalate", endpointResp.Header.Get("X-Condition-Action"))
	}

	var appResp *http.Response
	for i := 0; i < 5; i++ {
		appResp, _ = doTestRequest(t, app, mustRequest(http.MethodGet, "/fail/app/database", "", "", nil))
	}
	if appResp.Header.Get("X-Condition-Action") != "escalate" {
		t.Fatalf("app 5xx action=%q, want escalate", appResp.Header.Get("X-Condition-Action"))
	}

	eventsResp, eventsBody := doTestRequest(t, app, mustRequest(http.MethodGet, "/_events", "", "", nil))
	if eventsResp.StatusCode != http.StatusOK {
		t.Fatalf("_events status=%d body=%s", eventsResp.StatusCode, eventsBody)
	}
	var events []map[string]any
	if err := json.Unmarshal([]byte(eventsBody), &events); err != nil || len(events) == 0 {
		t.Fatalf("_events did not return useful JSON: len=%d err=%v body=%s", len(events), err, eventsBody)
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
