package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oarkflow/authz"
	authzstores "github.com/oarkflow/authz/stores"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

func TestServerPublishEvaluateAndAudit(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" }
  }
}`
	publish := map[string]any{"name": "demo", "source": source}
	body, _ := json.Marshal(publish)
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish status %d: %s", rec.Code, rec.Body.String())
	}

	evaluate := map[string]any{"decision": "access", "input": map[string]any{"request": map[string]any{"ok": true}}}
	body, _ = json.Marshal(evaluate)
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/audits", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audits status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServerEvaluateChain(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "chain-demo" {
  decision_table "access" {
    default allow
    hit_policy first
    row "allow" {
      when { principal.id != "" }
      then { outcome { decision allow reason "ok" attributes { action "allow" } metadata { severity "low" } } }
      reason "ok"
      reason_code "OK"
    }
  }
  chain "auth_guard" {
    entity "principal.id"
    decision "access"
    watch "failed_login" {
      event "failed_login"
      window "10m"
      step "rate_limit" { threshold 1 action "rate_limit" severity "medium" ttl "5m" }
    }
  }
}`
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{Name: "chain-demo", Source: source}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"event": "failed_login",
		"input": map[string]any{"principal": map[string]any{"id": "user-1"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/chain-demo/chains/auth_guard/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("chain evaluate status %d: %s", rec.Code, rec.Body.String())
	}
	var resp condition.ChainEvaluateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "rate_limit" || len(resp.Evaluation.Events) == 0 || resp.Audit.Operation != "chain_evaluate" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestServerEvaluateLifecycle(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "lifecycle-demo" {
  routes "http" {
    route "api" { method "GET" pattern "/api/{id}" metadata { category "api" } }
  }
  decision_table "observe" {
    default allow
    hit_policy first
    row "server-error" {
      when { response.status >= 500 }
      then { outcome { decision require_review reason "server error" attributes { action "server_error" } metadata { severity "high" } } }
      reason "server error"
      reason_code "SERVER_ERROR"
    }
  }
  chain "errors" {
    entity "request.actor_key"
    watch "endpoint_errors" {
      event "server_error"
      window "10m"
      step "notify" { threshold 1 action "notify" severity "high" ttl "5m" }
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "post" {
      decision "observe"
      chain "errors"
    }
  }
}`
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{Name: "lifecycle-demo", Source: source}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"phase":    "post",
		"method":   "GET",
		"path":     "/api/123",
		"input":    map[string]any{"request": map[string]any{"actor_key": "app"}},
		"response": map[string]any{"status": 500},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/lifecycle-demo/lifecycles/http_request/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("lifecycle evaluate status %d: %s", rec.Code, rec.Body.String())
	}
	var resp condition.LifecycleEvaluateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Evaluation.Route.Matched || resp.Evaluation.Route.Params["id"] != "123" || resp.Evaluation.FinalAction != "notify" || resp.Audit.Operation != "lifecycle_evaluate" {
		t.Fatalf("response = %#v", resp)
	}
	actionsReq := httptest.NewRequest(http.MethodGet, "/v1/actions?action=notify", nil)
	actionsReq.Header.Set("X-Roles", "condition-admin")
	actionsRec := httptest.NewRecorder()
	handler.ServeHTTP(actionsRec, actionsReq)
	if actionsRec.Code != http.StatusOK {
		t.Fatalf("actions status %d: %s", actionsRec.Code, actionsRec.Body.String())
	}
	var actions []storage.ActionDeliveryRecord
	if err := json.Unmarshal(actionsRec.Body.Bytes(), &actions); err != nil {
		t.Fatal(err)
	}
	if len(actions) == 0 || actions[len(actions)-1].Action != "notify" {
		t.Fatalf("actions = %#v", actions)
	}
	compactBody, _ := json.Marshal(storage.RetentionRequest{Definition: "lifecycle-demo", Before: time.Now().Add(time.Hour)})
	compactReq := httptest.NewRequest(http.MethodPost, "/v1/state/compact", bytes.NewReader(compactBody))
	compactReq.Header.Set("Content-Type", "application/json")
	compactReq.Header.Set("X-Roles", "condition-admin")
	compactRec := httptest.NewRecorder()
	handler.ServeHTTP(compactRec, compactReq)
	if compactRec.Code != http.StatusOK {
		t.Fatalf("compact status %d: %s", compactRec.Code, compactRec.Body.String())
	}
	var compact storage.RetentionResult
	if err := json.Unmarshal(compactRec.Body.Bytes(), &compact); err != nil {
		t.Fatal(err)
	}
	if compact.ActionDeliveries == 0 {
		t.Fatalf("compact = %#v", compact)
	}
	coverageReq := httptest.NewRequest(http.MethodGet, "/v1/definitions/lifecycle-demo/route-coverage", nil)
	coverageReq.Header.Set("X-Roles", "condition-admin")
	coverageRec := httptest.NewRecorder()
	handler.ServeHTTP(coverageRec, coverageReq)
	if coverageRec.Code != http.StatusOK {
		t.Fatalf("coverage status %d: %s", coverageRec.Code, coverageRec.Body.String())
	}
	var coverage condition.RouteCoverageReport
	if err := json.Unmarshal(coverageRec.Body.Bytes(), &coverage); err != nil {
		t.Fatal(err)
	}
	if !coverage.Passed || len(coverage.Routes) == 0 {
		t.Fatalf("coverage = %#v", coverage)
	}
}

func TestServerEvaluatePopulatesRuntimeContextFromHeaders(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-header-context" {
      when { all { context.tenant.id == "tenant-1" context.subject.id == "user-1" context.request.id == "req-1" session.id == "sess-1" session.attrs.mfa == true } }
      then { decision allow reason "header context accepted" }
      reason "header context accepted"
      reason_code "HEADER_CONTEXT"
    }
  }
}`
	if _, err := svc.Publish(condition.ContextWithTenant(context.Background(), "tenant-1"), condition.PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"decision": "access"})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/runtime/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	req.Header.Set("X-Subject-ID", "user-1")
	req.Header.Set("X-Tenant-ID", "tenant-1")
	req.Header.Set("X-Request-ID", "req-1")
	req.Header.Set("X-Session-ID", "sess-1")
	req.Header.Set("X-Session-MFA", "true")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body.String())
	}
	if reasonCodeFromEvaluateResponse(t, rec.Body.Bytes()) != "HEADER_CONTEXT" {
		t.Fatalf("response = %s", rec.Body.String())
	}
}

func TestServerEvaluateHeaderContextOverridesBodyContext(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-header-context" {
      when { all { context.tenant.id == "header-tenant" session.attrs.mfa == true } }
      then { decision allow reason "header context accepted" }
      reason "header context accepted"
      reason_code "HEADER_CONTEXT"
    }
  }
}`
	if _, err := svc.Publish(condition.ContextWithTenant(context.Background(), "header-tenant"), condition.PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"decision": "access",
		"input": map[string]any{
			"context": map[string]any{"tenant": map[string]any{"id": "body-tenant"}},
			"session": map[string]any{"attrs": map[string]any{"mfa": false}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/runtime/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	req.Header.Set("X-Tenant-ID", "header-tenant")
	req.Header.Set("X-Session-MFA", "true")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body.String())
	}
	if reasonCodeFromEvaluateResponse(t, rec.Body.Bytes()) != "HEADER_CONTEXT" {
		t.Fatalf("response = %s", rec.Body.String())
	}
}

func TestServerEvaluateExposesSanitizedHeadersToRequestHeaderFunction(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-header-function" {
      when { all { context.request.header("X-Plan") == "enterprise" context.request.header("Authorization", "redacted") == "redacted" } }
      then { decision allow reason "header function accepted" }
      reason "header function accepted"
      reason_code "HEADER_FUNCTION"
    }
  }
}`
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"decision": "access"})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/runtime/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	req.Header.Set("X-Plan", "enterprise")
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body.String())
	}
	if reasonCodeFromEvaluateResponse(t, rec.Body.Bytes()) != "HEADER_FUNCTION" {
		t.Fatalf("response = %s", rec.Body.String())
	}
}

func reasonCodeFromEvaluateResponse(t *testing.T, payload []byte) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	report, _ := body["report"].(map[string]any)
	decision, _ := report["decision"].(map[string]any)
	reason, _ := decision["reason_code"].(string)
	return reason
}

func TestServerRequiresAuthz(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	req := httptest.NewRequest(http.MethodGet, "/v1/definitions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestServerRejectsOversizedBody(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc, WithMaxBody(8)).Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader([]byte(`{"source":"too large"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestServerOnlyTrustsForwardedIPFromConfiguredProxy(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	srv := New(svc)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	if got := srv.remoteIP(req); got != "10.0.0.10" {
		t.Fatalf("remote ip without trusted proxy = %q", got)
	}

	srv = New(svc, WithTrustedProxies([]string{"10.0.0.0/24"}))
	if got := srv.remoteIP(req); got != "203.0.113.10" {
		t.Fatalf("remote ip with trusted proxy = %q", got)
	}
}

func TestWithTrustedProxiesAcceptsSingleIP(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	srv := New(svc, WithTrustedProxies([]string{"10.0.0.10"}))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("X-Real-IP", "203.0.113.11")
	if got := srv.remoteIP(req); got != "203.0.113.11" {
		t.Fatalf("remote ip = %q", got)
	}
}

func TestServerRolePermissions(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	req := httptest.NewRequest(http.MethodGet, "/v1/audits", nil)
	req.Header.Set("X-Roles", "condition-auditor")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auditor status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader([]byte(`{"name":"x","source":"module \"x\" {}"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-auditor")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("auditor publish status = %d, want 403", rec.Code)
	}
}

func TestServerDynamicRouteAuthzUsesCanonicalPattern(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	source := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" }
  }
}`
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{Name: "demo", Source: source}); err != nil {
		t.Fatal(err)
	}

	engine := authz.NewEngine(
		authzstores.NewMemoryPolicyStore(),
		authzstores.NewMemoryRoleStore(),
		authzstores.NewMemoryACLStore(),
		authzstores.NewMemoryAuditStore(),
		authz.WithRoleMembershipStore(authzstores.NewMemoryRoleMembershipStore()),
	)
	if err := engine.CreateRole(context.Background(), &authz.Role{
		ID:   "route-evaluator",
		Name: "Route Evaluator",
		Permissions: []authz.Permission{
			{Action: authz.Action(http.MethodPost), Resource: "route:POST:/v1/definitions/:name/evaluate"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	handler := New(svc, WithAuthzEngine(engine)).Handler()
	body, _ := json.Marshal(map[string]any{"decision": "access", "input": map[string]any{"request": map[string]any{"ok": true}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "route-evaluator")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("evaluate status %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/tests", nil)
	req.Header.Set("X-Roles", "route-evaluator")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tests status = %d, want 403", rec.Code)
	}
}

func TestRoutePatternResourceUsesFiberStyleAndParams(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	srv := New(svc)
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/workflows/manual/start", nil)
	resource := srv.routeResourceFromRequest(req)
	if resource.ID != "POST:/v1/definitions/:name/workflows/:workflow/start" {
		t.Fatalf("resource id = %q", resource.ID)
	}
	if resource.Attrs["name"] != "demo" || resource.Attrs["workflow"] != "manual" {
		t.Fatalf("attrs = %#v", resource.Attrs)
	}
}

func TestRouteMatcherSupportsFiberAndWildcardPatterns(t *testing.T) {
	params, ok := matchPathPattern("/tenants/:tenant_id/files/*path", "/tenants/acme/files/a/b/c")
	if !ok {
		t.Fatal("fiber-style wildcard route did not match")
	}
	if params["tenant_id"] != "acme" || params["path"] != "a/b/c" {
		t.Fatalf("params = %#v", params)
	}
	if _, ok := matchPathPattern("/tenants/:tenant_id/files/*path", "/tenants/acme"); ok {
		t.Fatal("short path unexpectedly matched")
	}
}

func TestServerLifecycleValidationReportsAndAuditVerify(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	sourceV1 := `module "demo" { decision_table "access" { default deny hit_policy first row "allow" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" } } }`
	sourceV2 := `module "demo" { decision_table "access" { default deny hit_policy first row "deny" { when { request.ok == true } then { decision deny reason "changed" } reason "changed" reason_code "CHANGED" } } }`

	body, _ := json.Marshal(map[string]any{"name": "demo", "source": sourceV1})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"name": "demo", "version": "1", "source": sourceV1})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish v1 %d: %s", rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"name": "demo", "version": "2", "source": sourceV2})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish v2 %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/definitions/demo/versions", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("versions %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/versions/1/activate", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"version": "2"})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/rollback", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rollback %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/audits/verify", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit verify %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServerWorkflowAndReports(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc).Handler()
	source := `module "workflow-demo" {
  decision_table "case" { default require_review row "review" { when { case.open == true } then { decision require_review } } }
  workflow "manual" {
    start "intake"
    stage "intake" { queue "risk-intake" rule "to_senior" { next_stage "senior" } }
    stage "senior" { role "risk-manager" }
  }
}`
	body, _ := json.Marshal(map[string]any{"name": "workflow-demo", "source": source})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"input": map[string]any{"case": map[string]any{"open": true}}})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/workflow-demo/workflows/manual/start", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("workflow start %d: %s", rec.Code, rec.Body.String())
	}
	var run condition.WorkflowRun
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/workflows/"+run.ID, nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow get %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/reports", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reports %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServerMetricsAndRateLimit(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	handler := New(svc, WithRateLimit(1, time.Minute)).Handler()

	req := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	req.Header.Set("X-Roles", "condition-auditor")
	req.Header.Set("X-Subject-ID", "auditor-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	req.Header.Set("X-Roles", "condition-auditor")
	req.Header.Set("X-Subject-ID", "auditor-1")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit status = %d, body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}
}

func TestServerProductionLifecycleEndpoints(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{
		StrictValidation:          true,
		StrictEvaluation:          true,
		RequireTests:              true,
		RequireActivationApproval: true,
	})
	handler := New(svc).Handler()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.bcl"), []byte(`bcl { version "1.0" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `bcl { version "1.0" strict true }
module "demo" {
  source "./module.bcl"
  decision_schema "access" { effects [allow, deny] default deny strategy first_match }
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" }
  }
  test "ok" { decision "access" input { request.ok true } expect { effect "allow" reason_code "OK" diagnostics "none" } }
}`
	body, _ := json.Marshal(map[string]any{"name": "demo", "version": "1", "source": source, "base_dir": dir})
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish version %d: %s", rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"decision": "access", "input": map[string]any{"request": map[string]any{"ok": true}}})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unactivated evaluate status = %d, body = %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"approved_by": "ops"})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/versions/1/approve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/versions/1/activate", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"reason": "incident"})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/disable", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"decision": "access", "input": map[string]any{"request": map[string]any{"ok": true}}})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/evaluate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled evaluate status = %d, body = %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"reason": "resolved"})
	req = httptest.NewRequest(http.MethodPost, "/v1/definitions/demo/enable", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable %d: %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/readiness", nil)
	req.Header.Set("X-Roles", "condition-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("readiness %d: %s", rec.Code, rec.Body.String())
	}
}

func TestServerUsesServiceConfigDefaults(t *testing.T) {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{MaxRequestBytes: 8})
	handler := New(svc).Handler()
	req := httptest.NewRequest(http.MethodPost, "/v1/definitions", bytes.NewReader([]byte(`{"source":"too large"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Roles", "condition-admin")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
