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
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
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
