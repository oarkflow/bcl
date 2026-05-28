package condition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/audit"
	"github.com/oarkflow/condition/pkg/storage"
)

const demoSource = `module "demo" {
  decision_schema "access" { effects [allow, deny] default deny strategy first_match }
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" {
      when { request.ok == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  test "ok" {
    decision "access"
    input { request.ok true }
    expect { effect "allow" reason_code "OK" }
  }
}`

func TestServicePublishEvaluateTestAndAudit(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource, RunTests: true}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.Effect != "allow" {
		t.Fatalf("effect = %q", resp.Report.Decision.Effect)
	}
	report, err := svc.Test(ctx, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("test report = %#v", report)
	}
	if err := svc.VerifyAudits(ctx); err != nil {
		t.Fatal(err)
	}
	if report.Audit == nil || report.Audit.Operation != "test" {
		t.Fatalf("missing test audit: %#v", report.Audit)
	}
}

func TestServiceFailsClosedWhenAuditAppendFails(t *testing.T) {
	ctx := context.Background()
	svc := NewService(failingAuditStore{Store: storage.NewMemoryStore()}, Config{})
	resp, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource, RunTests: true})
	if err == nil {
		t.Fatalf("expected audit failure, got response %#v", resp)
	}
	if !strings.Contains(err.Error(), "audit append failed") {
		t.Fatalf("err = %v", err)
	}
}

type failingAuditStore struct {
	storage.Store
}

func (s failingAuditStore) AppendAudit(context.Context, audit.Envelope) error {
	return errors.New("audit sink unavailable")
}

func TestServiceEvaluateUsesRuntimeContextAndSession(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-runtime-context" {
      when { all { context.required("tenant.id") == "tenant-from-context" session.attrs.mfa == true } }
      then { decision allow reason "runtime context accepted" }
      reason "runtime context accepted"
      reason_code "RUNTIME_CONTEXT"
    }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}
	evalCtx := WithContextValue(ctx, "tenant.id", "tenant-from-context")
	evalCtx = WithSessionValue(evalCtx, "attrs.mfa", true)
	resp, err := svc.Evaluate(evalCtx, "runtime", EvaluateRequest{Decision: "access"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.Effect != "allow" || resp.Report.Decision.ReasonCode != "RUNTIME_CONTEXT" {
		t.Fatalf("decision = %#v", resp.Report.Decision)
	}
}

func TestServiceEvaluateRuntimeContextOverridesInputContext(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-context-wins" {
      when { all { context.tenant.id == "ctx-tenant" session.attrs.mfa == true } }
      then { decision allow reason "context wins" }
      reason "context wins"
      reason_code "CONTEXT_WINS"
    }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}
	evalCtx := WithContextValue(ctx, "tenant.id", "ctx-tenant")
	evalCtx = WithSessionValue(evalCtx, "attrs.mfa", true)
	resp, err := svc.Evaluate(evalCtx, "runtime", EvaluateRequest{
		Decision: "access",
		Input: map[string]any{
			"context": map[string]any{"tenant": map[string]any{"id": "input-tenant"}},
			"session": map[string]any{"attrs": map[string]any{"mfa": false}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.ReasonCode != "CONTEXT_WINS" {
		t.Fatalf("decision = %#v", resp.Report.Decision)
	}
}

func TestServiceEvaluateSupportsRequestHeaderFunction(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-header" {
      when { context.request.header("X-Plan") == "enterprise" }
      then { decision allow reason "header accepted" }
      reason "header accepted"
      reason_code "HEADER_ACCEPTED"
    }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}
	evalCtx := WithRequestValue(ctx, "headers", map[string]any{"x-plan": "enterprise"})
	resp, err := svc.Evaluate(evalCtx, "runtime", EvaluateRequest{Decision: "access"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.ReasonCode != "HEADER_ACCEPTED" {
		t.Fatalf("decision = %#v", resp.Report.Decision)
	}
}

func TestServiceEvaluateInputContextStillWorks(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "runtime" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-input-context" {
      when { all { context.tenant.id == "input-tenant" session.attrs.mfa == true } }
      then { decision allow reason "input context accepted" }
      reason "input context accepted"
      reason_code "INPUT_CONTEXT"
    }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "runtime", Source: source}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Evaluate(ctx, "runtime", EvaluateRequest{
		Decision: "access",
		Input: map[string]any{
			"context": map[string]any{"tenant": map[string]any{"id": "input-tenant"}},
			"session": map[string]any{"attrs": map[string]any{"mfa": true}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.ReasonCode != "INPUT_CONTEXT" {
		t.Fatalf("decision = %#v", resp.Report.Decision)
	}
}

func TestServiceVersionActivationAndRollback(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	v1 := demoSource
	v2 := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "deny-ok" { when { request.ok == true } then { decision deny reason "changed" } reason "changed" reason_code "CHANGED" }
  }
}`
	if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "1", Source: v1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "2", Source: v2}); err != nil {
		t.Fatal(err)
	}
	versions, err := svc.ListVersions(ctx, "demo", "development")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %d", len(versions))
	}
	if _, err := svc.Activate(ctx, "demo", "2", "development"); err != nil {
		t.Fatal(err)
	}
	eval, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.ReasonCode != "CHANGED" {
		t.Fatalf("reason code = %q", eval.Report.Decision.ReasonCode)
	}
	if _, err := svc.Rollback(ctx, "demo", "1", "development"); err != nil {
		t.Fatal(err)
	}
	eval, err = svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.ReasonCode != "OK" {
		t.Fatalf("reason code after rollback = %q", eval.Report.Decision.ReasonCode)
	}
}

func TestServiceRequiresApprovalBeforeActivation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{RequireActivationApproval: true})
	if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "1", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(ctx, "demo", "1", "development"); err == nil {
		t.Fatal("expected activation to require approval")
	}
	approval, err := svc.Approve(ctx, "demo", "1", ApprovalRequest{ApprovedBy: "risk-owner", Reason: "reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	if approval.Definition.Metadata["approved"] != true {
		t.Fatalf("approval metadata = %#v", approval.Definition.Metadata)
	}
	if _, err := svc.Activate(ctx, "demo", "1", "development"); err != nil {
		t.Fatal(err)
	}
}

func TestServiceDisableBlocksEvaluationUntilEnabled(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Disable(ctx, "demo", DisableRequest{Reason: "incident"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}}); err == nil {
		t.Fatal("expected disabled definition to reject evaluation")
	}
	if _, err := svc.Enable(ctx, "demo", DisableRequest{Reason: "resolved"}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.Effect != "allow" {
		t.Fatalf("effect = %q", resp.Report.Decision.Effect)
	}
}

func TestServiceEvaluateSupportsShadowCandidate(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	candidate := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "deny-ok" { when { request.ok == true } then { decision deny reason "shadow" } reason "shadow" reason_code "SHADOW" }
  }
}`
	resp, err := svc.Evaluate(ctx, "demo", EvaluateRequest{
		Decision:              "access",
		Input:                 map[string]any{"request": map[string]any{"ok": true}},
		ShadowCandidateSource: candidate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.Effect != "allow" {
		t.Fatalf("base effect = %q", resp.Report.Decision.Effect)
	}
	if resp.Shadow == nil || len(resp.Shadow.ChangedCases) != 1 || resp.Shadow.EffectTransitions["allow->deny"] != 1 {
		t.Fatalf("shadow report = %#v", resp.Shadow)
	}
}

func TestServiceProductionReadinessReport(t *testing.T) {
	svc := NewService(storage.NewMemoryStore(), Config{
		StrictValidation:          true,
		StrictEvaluation:          true,
		RequireTests:              true,
		RequireActivationApproval: true,
	})
	report := svc.ProductionReadiness(context.Background())
	if !report.Ready {
		t.Fatalf("report = %#v", report)
	}
	for _, check := range []string{"store_available", "audit_chain_valid", "strict_validation_enabled", "strict_evaluation_enabled", "tests_or_gates_required", "activation_approval_required"} {
		if !report.Checks[check] {
			t.Fatalf("missing check %q in %#v", check, report)
		}
	}
}

func TestServiceLifecycleControlsPersistAcrossStores(t *testing.T) {
	t.Run("file", func(t *testing.T) {
		store, err := storage.NewFileStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		assertLifecycleControlsPersist(t, store)
	})
	t.Run("sqlite", func(t *testing.T) {
		store, err := storage.NewSQLiteStore(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		assertLifecycleControlsPersist(t, store)
	})
}

func assertLifecycleControlsPersist(t *testing.T, store storage.Store) {
	t.Helper()
	ctx := context.Background()
	svc := NewService(store, Config{RequireActivationApproval: true})
	if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "1", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, "demo", "1", ApprovalRequest{ApprovedBy: "ops"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Activate(ctx, "demo", "1", "development"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Disable(ctx, "demo", DisableRequest{Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}}); err == nil {
		t.Fatal("expected disabled persisted definition to block evaluation")
	}
	record, err := store.GetActiveDefinition(ctx, "demo", "development")
	if err != nil {
		t.Fatal(err)
	}
	if record.Metadata["approved"] != true || record.Metadata["disabled"] != true {
		t.Fatalf("metadata = %#v", record.Metadata)
	}
}

func TestServiceValidationReport(t *testing.T) {
	svc := NewService(storage.NewMemoryStore(), Config{})
	report, err := svc.Validate(context.Background(), ValidationRequest{Name: "empty", Source: `module "empty" {}`})
	if err != nil {
		t.Fatal(err)
	}
	if report.Publishable || report.Valid {
		t.Fatalf("report = %#v", report)
	}
}

func TestServiceProductionValidationRejectsMissingVersionAndModuleSource(t *testing.T) {
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true})
	report, err := svc.Validate(context.Background(), ValidationRequest{Name: "demo", Source: demoSource})
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || report.Publishable {
		t.Fatalf("strict report should not be valid: %#v", report)
	}
	if !diagnosticsContain(report.Diagnostics, "missing bcl version declaration") {
		t.Fatalf("expected missing version diagnostic: %#v", report.Diagnostics)
	}
	if !diagnosticsContain(report.Diagnostics, "module requires source in strict mode") {
		t.Fatalf("expected missing module source diagnostic: %#v", report.Diagnostics)
	}
}

func TestServiceProductionPublishFailureDoesNotActivateDefinition(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true})
	resp, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource})
	if err == nil {
		t.Fatalf("expected publish failure, got response %#v", resp)
	}
	if resp == nil || resp.Audit.Operation != "publish_failed" {
		t.Fatalf("expected publish_failed audit, got %#v", resp)
	}
	if _, getErr := svc.GetDefinition(ctx, "demo"); getErr == nil {
		t.Fatal("failed publish should not save or activate a definition")
	}
	if verifyErr := svc.VerifyAudits(ctx); verifyErr != nil {
		t.Fatal(verifyErr)
	}
}

func TestServiceProductionValidationRequiresTests(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.bcl"), []byte(`bcl { version "1.0" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `bcl {
  version "1.0"
  strict true
}

module "demo" {
  source "./module.bcl"
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, RequireTests: true})
	report, err := svc.Validate(context.Background(), ValidationRequest{Name: "demo", Source: source, BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || report.Publishable {
		t.Fatalf("test-required report should not be valid: %#v", report)
	}
	if report.Tests == nil || report.Tests.Passed {
		t.Fatalf("expected failing test report: %#v", report.Tests)
	}
	if !diagnosticsContain(report.Diagnostics, "definition requires at least one test or decision gate") {
		t.Fatalf("expected required test diagnostic: %#v", report.Diagnostics)
	}
}

func TestServiceProductionValidationPublishesStrictTestedDefinition(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.bcl"), []byte(`bcl { version "1.0" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `bcl {
  version "1.0"
  strict true
}

module "demo" {
  source "./module.bcl"
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" {
      when { request.ok == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  test "ok" {
    decision "access"
    input { request.ok true }
    expect { effect "allow" reason_code "OK" diagnostics "none" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, RequireTests: true})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: source, BaseDir: dir}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.Effect != "allow" {
		t.Fatalf("effect = %q", resp.Report.Decision.Effect)
	}
}

func TestServiceStrictEvaluationRejectsInputDiagnostics(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.bcl"), []byte(`bcl { version "1.0" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `bcl {
  version "1.0"
  strict true
}

schema access {
  required request object
}

module "demo" {
  source "./module.bcl"
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" {
      when { request.ok == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  test "ok" {
    decision "access"
    input { request.ok true }
    expect { effect "allow" reason_code "OK" diagnostics "none" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, StrictEvaluation: true, RequireTests: true})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: source, BaseDir: dir}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{}})
	if err == nil {
		t.Fatalf("expected strict evaluation error, got response %#v", resp)
	}
	if resp == nil || resp.Report == nil || !diagnosticsContain(resp.Report.Diagnostics, "evaluation produced diagnostics") {
		t.Fatalf("expected strict evaluation diagnostic, got resp=%#v err=%v", resp, err)
	}
}

func TestServiceTenantIsolation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ContextWithTenant(ctx, "tenant-a"), PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Evaluate(ContextWithTenant(ctx, "tenant-b"), "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}}); err == nil {
		t.Fatal("expected tenant-b to be isolated from tenant-a definition")
	}
	resp, err := svc.Evaluate(ContextWithTenant(ctx, "tenant-a"), "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Report.Decision.ReasonCode != "OK" {
		t.Fatalf("decision = %#v", resp.Report.Decision)
	}
}

func TestServiceFixedRuntimeTime(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{Runtime: RuntimePolicy{FixedTime: "2026-05-26T12:00:00Z"}})
	source := `module "time-demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-fixed-time" {
      when { now() == "2026-05-26T12:00:00Z" }
      then { decision allow reason "fixed" }
      reason "fixed"
      reason_code "FIXED_TIME"
    }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "time-demo", Source: source}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		resp, err := svc.Evaluate(ctx, "time-demo", EvaluateRequest{Decision: "access"})
		if err != nil {
			t.Fatal(err)
		}
		if resp.Report.Decision.ReasonCode != "FIXED_TIME" {
			t.Fatalf("decision = %#v", resp.Report.Decision)
		}
	}
}

func TestServiceExternalDatasetDeniedByDefault(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "external" {
  decision_table "access" { default deny hit_policy first }
  dataset "batch" {
    source { adapter file path "./batch.jsonl" format jsonl }
  }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "external", Source: source}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Evaluate(ctx, "external", EvaluateRequest{Decision: "access"}); err == nil || !strings.Contains(err.Error(), "disallowed adapter") {
		t.Fatalf("expected disallowed adapter error, got %v", err)
	}
}

func TestServiceCanaryPromotesPassingCandidate(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Canary(ctx, "demo", CanaryRequest{
		SimulationRequest: SimulationRequest{
			CandidateSource: demoSource,
			Decision:        "access",
			Cases:           []bcl.DecisionBatchCase{{ID: "ok", Input: map[string]any{"request": map[string]any{"ok": true}}}},
		},
		Promote:        true,
		PromoteVersion: "2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Passed || resp.Promotion == nil || resp.Promotion.Definition.Version != "2" {
		t.Fatalf("canary response = %#v", resp)
	}
	active, err := svc.GetDefinition(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if active.Version != "2" {
		t.Fatalf("active version = %q", active.Version)
	}
}

func TestServiceSimulateCompare(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		t.Fatal(err)
	}
	candidate := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "deny-ok" { when { request.ok == true } then { decision deny reason "changed" } }
  }
}`
	resp, err := svc.Simulate(ctx, "demo", SimulationRequest{
		CandidateSource: candidate,
		Decision:        "access",
		Cases:           []bcl.DecisionBatchCase{{ID: "ok", Input: map[string]any{"request": map[string]any{"ok": true}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Compare.ChangedCases) != 1 {
		t.Fatalf("changed cases = %#v", resp.Compare.ChangedCases)
	}
}

func diagnosticsContain(diags []bcl.Diagnostic, text string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, text) {
			return true
		}
	}
	return false
}

func TestServiceReloadKeepsLastKnownGood(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "decision.bcl")
	if err := os.WriteFile(path, []byte(demoSource), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Path: path, RunTests: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow reason "ok" } reason "ok" reason_code "OK" }
  }
  test "bad_expectation" {
    decision "access"
    input { request.ok true }
    expect { effect "deny" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.Reload(ctx, ReloadRequest{Name: "demo", RunTests: true})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Reloaded || !resp.KeptLast {
		t.Fatalf("reload response = %#v", resp)
	}
	eval, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.Effect != "allow" {
		t.Fatalf("effect = %q", eval.Report.Decision.Effect)
	}
}

func TestServicePublishSourceBaseDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "decision.schema"), []byte(`schema access { required request object }`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `import "./decision.schema"
module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "allow-ok" { when { request.ok == true } then { decision allow } }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: source, BaseDir: dir}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceWorkflowStartAdvance(t *testing.T) {
	ctx := context.Background()
	source := `module "workflow-demo" {
  decision_table "case" {
    default require_review
    row "review" { when { case.open == true } then { decision require_review } }
  }
  workflow "manual" {
    start "intake"
    stage "intake" {
      queue "risk-intake"
      sla "15m"
      rule "to_senior" { next_stage "senior" }
    }
    stage "senior" {
      role "risk-manager"
      sla "2h"
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "workflow-demo", Source: source}); err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartWorkflow(ctx, "workflow-demo", "manual", map[string]any{"case": map[string]any{"open": true}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Stage != "intake" || run.Assignment["queue"] != "risk-intake" {
		t.Fatalf("run = %#v", run)
	}
	run, err = svc.AdvanceWorkflow(ctx, run.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Stage != "senior" || run.Assignment["role"] != "risk-manager" {
		t.Fatalf("advanced run = %#v", run)
	}
	stored, err := svc.GetWorkflowRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Stage != "senior" {
		t.Fatalf("stored = %#v", stored)
	}
}

const chainSource = `module "chain-demo" {
  decision_table "api_endpoint_guard" {
    default allow
    hit_policy first
    row "admin-denied" {
      when { request.admin_denied == true }
      then { outcome { decision deny reason "admin route denied" attributes { action "admin_denied" } metadata { severity "high" category "abuse" } } }
      reason "admin route denied"
      reason_code "ADMIN_DENIED"
    }
    row "auth-allow" {
      when { request.admin_denied != true }
      then { outcome { decision allow reason "ok" attributes { action "allow" } metadata { severity "low" } } }
      reason "ok"
      reason_code "OK"
    }
  }

  decision_table "http_auth_guard" {
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
    decision "api_endpoint_guard"
    decision "http_auth_guard"

    watch "failed_login_escalation" {
      event "failed_login"
      window "10m"
      metadata { category "abuse" }
      step "rate_limit" { threshold 3 action "rate_limit" severity "medium" ttl "5m" }
      step "warn" { threshold 5 action "warning" severity "high" ttl "10m" }
      step "temporary_block" { threshold 6 action "temporary_block" severity "critical" ttl "30m" }
    }

    watch "repeat_blocks" {
      event "temporary_block"
      window "30d"
      step "suspend_account" { threshold 3 action "suspend" severity "critical" ttl "24h" }
    }

    watch "admin_abuse" {
      event "admin_denied"
      window "30d"
      step "admin_temporary_block" { threshold 3 action "temporary_block" severity "critical" ttl "30m" }
    }
  }
}`

func TestServiceEvaluateChainEscalatingWatch(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "chain-demo", Source: chainSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	input := map[string]any{"principal": map[string]any{"id": "user-1"}, "request": map[string]any{}}
	var resp *ChainEvaluateResponse
	for i := 0; i < 3; i++ {
		var err error
		resp, err = svc.EvaluateChain(ctx, "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input, Event: "failed_login"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resp.Evaluation.FinalAction != "rate_limit" || resp.Evaluation.FinalEffect != "require_review" {
		t.Fatalf("after 3 failed logins = %#v", resp.Evaluation)
	}
	for i := 0; i < 2; i++ {
		var err error
		resp, err = svc.EvaluateChain(ctx, "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input, Event: "failed_login"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resp.Evaluation.FinalAction != "warning" || resp.Evaluation.FinalEffect != "allow" {
		t.Fatalf("after 5 failed logins = %#v", resp.Evaluation)
	}
	resp, err := svc.EvaluateChain(ctx, "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input, Event: "failed_login"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "temporary_block" || resp.Evaluation.FinalEffect != "deny" {
		t.Fatalf("after 6 failed logins = %#v", resp.Evaluation)
	}
	hasAbuseMetadata := false
	for _, state := range resp.Evaluation.StateAfter {
		if state.Metadata["category"] == "abuse" {
			hasAbuseMetadata = true
		}
	}
	if !hasAbuseMetadata {
		t.Fatalf("missing abuse metadata in chain state: %#v", resp.Evaluation.StateAfter)
	}
	for i := 0; i < 2; i++ {
		resp, err = svc.EvaluateChain(ctx, "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input, Event: "failed_login"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resp.Evaluation.FinalAction != "suspend" || resp.Evaluation.FinalEffect != "deny" {
		t.Fatalf("repeat temporary blocks should suspend = %#v", resp.Evaluation)
	}
}

func TestServiceEvaluateChainCrossDecisionEventsAndTenantIsolation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(conditionTenant(ctx, "a"), PublishRequest{Name: "chain-demo", Source: chainSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if resp, err := svc.Publish(conditionTenant(ctx, "b"), PublishRequest{Name: "chain-demo", Source: chainSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	input := map[string]any{"principal": map[string]any{"id": "user-1"}, "request": map[string]any{"admin_denied": true}}
	var resp *ChainEvaluateResponse
	for i := 0; i < 3; i++ {
		var err error
		resp, err = svc.EvaluateChain(conditionTenant(ctx, "a"), "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resp.Evaluation.FinalAction != "temporary_block" {
		t.Fatalf("tenant a final action = %#v", resp.Evaluation)
	}
	resp, err := svc.EvaluateChain(conditionTenant(ctx, "b"), "chain-demo", "auth_guard", ChainEvaluateRequest{Input: input})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction == "temporary_block" {
		t.Fatalf("tenant b should not inherit tenant a chain state: %#v", resp.Evaluation)
	}
}

const lifecycleSource = `module "lifecycle-demo" {
  routes "http" {
    route "admin_reports" {
      method "GET"
      pattern "/admin/reports"
      metadata { policy_type "rbac" category "admin" }
    }
    route "health" {
      method "GET"
      pattern "/healthz"
      metadata { policy_type "public" category "global" }
    }
  }

  decision_table "pre_guard" {
    default allow
    hit_policy first
    row "deny-admin" {
      when { route.metadata.policy_type == "rbac" && principal.admin != true }
      then { outcome { decision deny reason "admin required" attributes { action "admin_denied" } metadata { severity "high" category "authorization" } } }
      reason "admin required"
      reason_code "ADMIN_DENIED"
    }
    row "allow" {
      when { true == true }
      then { outcome { decision allow reason "ok" attributes { action "allow" } metadata { severity "low" } } }
      reason "ok"
      reason_code "OK"
    }
  }

  decision_table "post_observe" {
    default allow
    hit_policy first
    row "server-error" {
      when { response.status >= 500 }
      then { outcome { decision require_review reason "server error observed" attributes { action "server_error" sink "event" } metadata { severity "high" category "observability" } } obligation notify_ops { channel "pager" } }
      reason "server error observed"
      reason_code "SERVER_ERROR"
    }
    row "ok" {
      when { response.status < 500 }
      then { outcome { decision allow reason "healthy" attributes { action "healthy" } metadata { severity "low" } } }
      reason "healthy"
      reason_code "HEALTHY"
    }
  }

  chain "response_error_chain" {
    entity "request.actor_key"
    watch "endpoint_5xx" {
      event "server_error"
      window "10m"
      metadata { category "observability" scope "endpoint" }
      step "log" { threshold 1 action "log" severity "medium" ttl "5m" }
      step "escalate" { threshold 3 action "escalate" severity "critical" ttl "30m" }
    }
  }

  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "pre" {
      decision "pre_guard"
    }
    phase "post" {
      decision "post_observe"
      chain "response_error_chain"
    }
  }
}`

func TestServiceEvaluateLifecyclePreAndPostEscalation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	pre, err := svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
		Phase:  "pre",
		Method: "GET",
		Path:   "/admin/reports",
		Input:  map[string]any{"principal": map[string]any{"admin": false}, "request": map[string]any{"actor_key": "u-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pre.Evaluation.Route.Matched || pre.Evaluation.Route.ID != "admin_reports" {
		t.Fatalf("route match = %#v", pre.Evaluation.Route)
	}
	if pre.Evaluation.FinalEffect != "deny" || pre.Evaluation.FinalAction != "admin_denied" {
		t.Fatalf("pre result = %#v", pre.Evaluation)
	}
	var post *LifecycleEvaluateResponse
	for i := 0; i < 3; i++ {
		post, err = svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
			Phase:    "post",
			Method:   "GET",
			Path:     "/admin/reports",
			Input:    map[string]any{"request": map[string]any{"actor_key": "u-1"}},
			Response: map[string]any{"status": 500, "duration_ms": 12},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if post.Evaluation.FinalAction != "escalate" || post.Evaluation.FinalEffect != "require_review" {
		t.Fatalf("post escalation = %#v", post.Evaluation)
	}
	if post.Evaluation.TraceID == "" || post.Evaluation.AuditID == "" {
		t.Fatalf("missing trace/audit ids: %#v", post.Evaluation)
	}
	if len(post.Evaluation.Actions) == 0 {
		t.Fatalf("missing lifecycle actions: %#v", post.Evaluation)
	}
}

func TestServiceEvaluateLifecycleTenantIsolationAndWebhookAllowlist(t *testing.T) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{Runtime: RuntimePolicy{AllowedActionSinks: []string{"webhook"}, AllowedWebhookHosts: []string{"example.invalid"}, AllowedWebhookMethods: []string{"POST"}}})
	if resp, err := svc.Publish(conditionTenant(ctx, "a"), PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if resp, err := svc.Publish(conditionTenant(ctx, "b"), PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.EvaluateLifecycle(conditionTenant(ctx, "a"), "lifecycle-demo", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/admin/reports", Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}, Response: map[string]any{"status": 500}}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := svc.EvaluateLifecycle(conditionTenant(ctx, "b"), "lifecycle-demo", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/admin/reports", Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}, Response: map[string]any{"status": 500}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction == "escalate" {
		t.Fatalf("tenant b inherited tenant a lifecycle state: %#v", resp.Evaluation)
	}
}

func TestServiceEvaluateLifecycleUsesHostActionHandler(t *testing.T) {
	ctx := context.Background()
	called := 0
	svc := NewService(storage.NewMemoryStore(), Config{ActionHandlers: map[string]ActionHandler{
		"escalate": func(ctx context.Context, action LifecycleAction) (ActionResult, error) {
			called++
			if action.Name != "escalate" {
				return ActionResult{}, errors.New("unexpected action")
			}
			return ActionResult{Handled: true, Status: "sent", Metadata: map[string]any{"provider": "test"}}, nil
		},
	}})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	var resp *LifecycleEvaluateResponse
	var err error
	for i := 0; i < 3; i++ {
		resp, err = svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
			Phase:    "post",
			Method:   "GET",
			Path:     "/admin/reports",
			Input:    map[string]any{"request": map[string]any{"actor_key": "u-action"}},
			Response: map[string]any{"status": 500},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if called != 1 {
		t.Fatalf("handler calls = %d, want 1; actions=%#v", called, resp.Evaluation.Actions)
	}
	found := false
	for _, action := range resp.Evaluation.Actions {
		if action.Name == "escalate" && action.Handled && action.Result != nil && action.Result.Status == "sent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing handled escalate action: %#v", resp.Evaluation.Actions)
	}
	deliveries, err := svc.ListActionDeliveries(ctx, storage.ActionDeliveryQuery{Action: "escalate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "sent" || !deliveries[0].Handled {
		t.Fatalf("deliveries = %#v", deliveries)
	}
	incidents, err := svc.ListIncidents(ctx, storage.IncidentQuery{Action: "escalate", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 1 || incidents[0].Count != 1 {
		t.Fatalf("incidents = %#v", incidents)
	}
}

func TestServiceEvaluateLifecycleTimeTravelExpiresStateAndWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewService(storage.NewMemoryStore(), Config{Clock: func() time.Time { return now }})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	var resp *LifecycleEvaluateResponse
	var err error
	for i := 0; i < 3; i++ {
		resp, err = svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
			Phase:    "post",
			Method:   "GET",
			Path:     "/admin/reports",
			Input:    map[string]any{"request": map[string]any{"actor_key": "u-clock"}},
			Response: map[string]any{"status": 500},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if resp.Evaluation.FinalAction != "escalate" {
		t.Fatalf("expected escalation before expiry, got %#v", resp.Evaluation)
	}
	now = now.Add(31 * time.Minute)
	resp, err = svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
		Phase:    "post",
		Method:   "GET",
		Path:     "/admin/reports",
		Input:    map[string]any{"request": map[string]any{"actor_key": "u-clock"}},
		Response: map[string]any{"status": 500},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "log" {
		t.Fatalf("expected window reset to first step after time travel, got %#v", resp.Evaluation)
	}
}

func TestServiceValidateRouteConflictDiagnostics(t *testing.T) {
	ctx := context.Background()
	source := `module "route-diags" {
  decision_table "access" {
    default allow
    row "ok" {
      when { true == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  routes "http" {
    route "users" { method "GET" pattern "/users/{id}" }
    route "users" { method "GET" pattern "/users/:name" }
    route "bad" { method "GET" pattern "/files/{rest...}/x" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	report, err := svc.Validate(ctx, ValidationRequest{Name: "route-diags", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"duplicate route id", "duplicate route GET /users", "invalid pattern"} {
		if !diagnosticsContain(report.Diagnostics, want) {
			t.Fatalf("missing %q diagnostic: %#v", want, report.Diagnostics)
		}
	}
	if report.Publishable {
		t.Fatalf("expected route conflicts to block publish: %#v", report.Diagnostics)
	}
}

func TestServiceValidatePolicyPackageAndActionContracts(t *testing.T) {
	ctx := context.Background()
	source := `module "contracts" {
  policy_package "security" {
    owner "platform-security"
    domain "auth"
    capabilities ["state", "routes", "actions"]
    actions ["allow", "deny", "notify"]
    state true
  }
  action_catalog "default" {
    action "allow" { sink "event" severity "low" }
    action "deny" { sink "event" severity "high" approval "required" retries 1 }
    action "notify" { sinks ["event", "webhook"] severity "high" approval "required" retries 3 }
  }
  output_contract "guard_output" {
    actions ["allow", "deny", "notify"]
    severities ["low", "high", "critical"]
  }
  chain "notify_chain" {
    entity "request.actor_key"
    watch "risk" {
      event "risk"
      window "10m"
      step "notify" { threshold 1 action "notify" severity "high" ttl "5m" }
    }
  }
  decision_table "access" {
    contract "guard_output"
    default deny
    row "notify" {
      when { request.risky == true }
      then { outcome { decision require_review reason "risky" attributes { action "notify" } metadata { severity "high" } } }
      reason "risky"
      reason_code "RISKY"
    }
    row "ok" {
      when { true == true }
      then { outcome { decision allow reason "ok" attributes { action "allow" } metadata { severity "low" } } }
      reason "ok"
      reason_code "OK"
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	report, err := svc.Validate(ctx, ValidationRequest{Name: "contracts", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Publishable {
		t.Fatalf("expected contracts source to publish: %#v", report.Diagnostics)
	}

	bad := strings.Replace(source, `action "notify" { sinks ["event", "webhook"] severity "high" approval "required" retries 3 }`, "", 1)
	report, err = svc.Validate(ctx, ValidationRequest{Name: "contracts", Source: bad})
	if err != nil {
		t.Fatal(err)
	}
	if !diagnosticsContain(report.Diagnostics, `unknown action "notify"`) {
		t.Fatalf("missing unknown action diagnostic: %#v", report.Diagnostics)
	}
}

func TestServiceValidateStandardFactsAndExplainPackage(t *testing.T) {
	ctx := context.Background()
	base := `module "facts" {
  standard_facts "http" {
    fact "request" { schema "http_request" }
    fact "response" { schema "http_response" }
    fact "risk" { schema "risk_fact" }
  }
  decision_table "access" {
    default allow
    row "ok" {
      when { true == true }
      then { decision allow reason "ok" attributes { action "allow" } }
      reason "ok"
      reason_code "OK"
    }
  }
  routes "http" {
    route "users" { method "GET" pattern "/users/{id}" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "facts", Source: base}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	badFacts := strings.Replace(base, `fact "risk" { schema "risk_fact" }`, `fact "device" { schema "device_fact" }`, 1)
	report, err := svc.Validate(ctx, ValidationRequest{Name: "facts", Source: badFacts})
	if err != nil {
		t.Fatal(err)
	}
	if !diagnosticsContain(report.Diagnostics, `unsupported fact "device"`) {
		t.Fatalf("missing unsupported fact diagnostic: %#v", report.Diagnostics)
	}

	candidate := strings.Replace(base, `route "users" { method "GET" pattern "/users/{id}" }`, `route "users" { method "GET" pattern "/users/{id}" }
    route "projects" { method "GET" pattern "/projects/{id}" }`, 1)
	candidate = strings.Replace(candidate, `decision_table "access"`, `chain "access_chain" {
    entity "request.actor_key"
    watch "allow_watch" {
      event "allow"
      window "10m"
      step "notify" { threshold 1 action "notify" severity "medium" ttl "5m" }
    }
  }
  decision_table "access"`, 1)
	resp, err := svc.ExplainPackage(ctx, "facts", PackageExplainRequest{CandidateSource: candidate})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(resp.Report.Routes.Added, "http.projects") || !containsString(resp.Report.Chains.Added, "access_chain") || !containsString(resp.Report.Actions.Added, "notify") {
		t.Fatalf("unexpected package explain report: %#v", resp.Report)
	}
}

func TestServiceEvaluateLifecycleInjectsResponseClassification(t *testing.T) {
	ctx := context.Background()
	source := `module "classifier" {
  response_classifier "http" {
    healthy_below 400
    unhealthy_at_or_above 500
    expected_client_statuses [400, 401, 403]
  }
  routes "http" {
    route "items" { method "GET" pattern "/items" }
  }
  decision_table "observe" {
    default allow
    row "unexpected-client-error" {
      when { response.class == "unexpected_client_error" }
      then { decision require_review reason "unexpected 4xx" attributes { action "unexpected_4xx" } }
      reason "unexpected 4xx"
      reason_code "UNEXPECTED_4XX"
    }
    row "healthy" {
      when { response.healthy == true }
      then { decision allow reason "healthy" attributes { action "healthy" } }
      reason "healthy"
      reason_code "HEALTHY"
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "post" {
      decision "observe"
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "classifier", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	resp, err := svc.EvaluateLifecycle(ctx, "classifier", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/items", Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}, Response: map[string]any{"status": 404}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "unexpected_4xx" || resp.Evaluation.FinalEffect != "require_review" {
		t.Fatalf("expected 404 to be unhealthy unexpected 4xx: %#v", resp.Evaluation)
	}
	resp, err = svc.EvaluateLifecycle(ctx, "classifier", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/items", Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}, Response: map[string]any{"status": 401}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "healthy" || resp.Evaluation.FinalEffect != "allow" {
		t.Fatalf("expected 401 to be expected healthy client response: %#v", resp.Evaluation)
	}
}

func TestServiceEvaluateLifecycleAppliesPolicyOverlays(t *testing.T) {
	ctx := conditionTenant(context.Background(), "tenant-a")
	source := `module "overlay-demo" {
  environment "prod"
  routes "http" {
    route "documents" { method "GET" pattern "/documents/{id}" metadata { category "documents" access "standard" } }
  }
  policy_overlay "prod_defaults" {
    layer "environment"
    environment "prod"
    metadata { region "us" }
  }
  policy_overlay "tenant_strict" {
    layer "tenant"
    tenant "tenant-a"
    metadata { tenant_policy "strict" }
  }
  policy_overlay "restricted_documents" {
    layer "route"
    route "documents"
    metadata { access "restricted" }
  }
  decision_table "guard" {
    default allow
    row "restricted" {
      when { all { route.access == "restricted" route.tenant_policy == "strict" route.region == "us" } }
      then { decision require_review reason "restricted tenant route" metadata { severity "high" } }
      reason "restricted tenant route"
      reason_code "RESTRICTED_ROUTE"
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "pre" { decision "guard" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{Environment: "prod"})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "overlay-demo", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	resp, err := svc.EvaluateLifecycle(ctx, "overlay-demo", "http_request", LifecycleEvaluateRequest{Phase: "pre", Method: "GET", Path: "/documents/123", Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalReason != "restricted tenant route" {
		t.Fatalf("evaluation = %#v", resp.Evaluation)
	}
	if resp.Evaluation.Route.Metadata["access"] != "restricted" || resp.Evaluation.Route.Metadata["tenant_policy"] != "strict" || resp.Evaluation.Route.Metadata["region"] != "us" {
		t.Fatalf("route metadata = %#v", resp.Evaluation.Route.Metadata)
	}
}

func TestServiceRunsLifecycleTestBlocks(t *testing.T) {
	ctx := context.Background()
	source := `module "lifecycle-tests" {
  routes "http" {
    route "items" { method "GET" pattern "/items/{id}" }
    route "items_post" { method "POST" pattern "/items/{id}" }
  }
  decision_table "observe" {
    default allow
    row "body-and-headers" {
      priority 100
      when { all { request.headers.x_request_id == "req-123" request.body_json.username == "alice" response.headers.content_type == "application/json" response.body_json.error == "upstream timeout" response.body_format == "json" } }
      then { decision require_review reason "envelope matched" attributes { action "notify" } metadata { severity "high" } }
      reason "envelope matched"
      reason_code "ENVELOPE_MATCHED"
    }
    row "form-body" {
      priority 90
      when { all { request.body_form.grant_type == "password" request.body_format == "form" } }
      then { decision require_review reason "form envelope matched" attributes { action "notify" } metadata { severity "high" } }
      reason "form envelope matched"
      reason_code "FORM_ENVELOPE_MATCHED"
    }
    row "error" {
      priority 10
      when { response.status >= 500 }
      then { decision require_review reason "server error" attributes { action "notify" } metadata { severity "high" } }
      reason "server error"
      reason_code "SERVER_ERROR"
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "post" { decision "observe" }
    phase "error" { decision "observe" }
  }
  lifecycle_test "server error emits notify" {
    lifecycle "http_request"
    phase "post"
    method "GET"
    path "/items/123"
    input { request.actor_key "u-1" }
    response { status 500 }
    expect {
      final_action "notify"
      final_reason "server error"
      route "items"
    }
  }
  lifecycle_test "error phase emits notify" {
    lifecycle "http_request"
    phase "error"
    method "GET"
    path "/items/123"
    input { request.actor_key "u-1" }
    request {
      headers { x_request_id "req-123" content_type "application/json" }
      body { username "alice" }
      format "json"
    }
    response {
      status 503
      headers { content_type "application/json" }
      body { error "upstream timeout" }
      format "json"
    }
    expect {
      final_action "notify"
      final_reason "envelope matched"
      route "items"
    }
  }
  lifecycle_test "form request body is available" {
    lifecycle "http_request"
    phase "post"
    method "POST"
    path "/items/123"
    input { request.actor_key "u-1" }
    request {
      headers { content_type "application/x-www-form-urlencoded" }
      body { grant_type "password" username "alice" }
      format "form"
    }
    response { status 200 format "text" body "ok" }
    expect {
      final_action "notify"
      final_reason "form envelope matched"
      route "items_post"
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	resp, err := svc.Publish(ctx, PublishRequest{Name: "lifecycle-tests", Source: source, RunTests: true})
	if err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if resp.Tests == nil || len(resp.Tests.LifecycleScenarios) != 3 || !resp.Tests.LifecycleScenarios[0].Passed || !resp.Tests.LifecycleScenarios[1].Passed || !resp.Tests.LifecycleScenarios[2].Passed {
		t.Fatalf("tests = %#v", resp.Tests)
	}
}

func TestServiceRouteCoverageReportsUncoveredRoutes(t *testing.T) {
	ctx := context.Background()
	source := `module "coverage" {
  routes "http" {
    route "covered" { method "GET" pattern "/covered" }
    route "also_covered" { method "GET" pattern "/also-covered" }
  }
  routes "jobs" {
    route "uncovered" { method "POST" pattern "/jobs" }
  }
  decision_table "guard" {
    default allow
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "pre" { decision "guard" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "coverage", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	report, err := svc.RouteCoverage(ctx, "coverage")
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.Uncovered) != 1 || report.Uncovered[0] != "jobs.uncovered" {
		t.Fatalf("coverage = %#v", report)
	}
}

func TestServiceEvaluateLifecycleDryRunActions(t *testing.T) {
	ctx := context.Background()
	called := 0
	svc := NewService(storage.NewMemoryStore(), Config{ActionHandlers: map[string]ActionHandler{
		"notify": func(context.Context, LifecycleAction) (ActionResult, error) {
			called++
			return ActionResult{Handled: true, Status: "sent"}, nil
		},
	}})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "lifecycle-demo", Source: lifecycleSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	resp, err := svc.EvaluateLifecycle(ctx, "lifecycle-demo", "http_request", LifecycleEvaluateRequest{
		Phase:    "post",
		Method:   "GET",
		Path:     "/admin/reports",
		Input:    map[string]any{"request": map[string]any{"actor_key": "dry-run"}},
		Response: map[string]any{"status": 500},
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("dry run called handler %d times", called)
	}
	found := false
	for _, action := range resp.Evaluation.Actions {
		if action.Result != nil && action.Result.Status == "dry_run" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing dry-run action result: %#v", resp.Evaluation.Actions)
	}
	deliveries, err := svc.ListActionDeliveries(ctx, storage.ActionDeliveryQuery{Status: "dry_run"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) == 0 {
		t.Fatal("missing dry-run delivery record")
	}
}

func TestServiceActionRetryMetadata(t *testing.T) {
	ctx := context.Background()
	source := `module "retry-actions" {
  action_catalog "default" {
    action "notify" { sink "webhook" severity "high" retries 2 }
  }
  routes "http" {
    route "ops" { method "GET" pattern "/ops" }
  }
  decision_table "observe" {
    default allow
    row "notify" {
      when { true == true }
      then { decision require_review reason "notify" attributes { action "notify" sink "webhook" } metadata { severity "high" } }
      reason "notify"
      reason_code "NOTIFY"
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "post" { decision "observe" }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{ActionHandlers: map[string]ActionHandler{
		"notify": func(context.Context, LifecycleAction) (ActionResult, error) {
			return ActionResult{}, errors.New("provider unavailable")
		},
	}})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "retry-actions", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	_, err := svc.EvaluateLifecycle(ctx, "retry-actions", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/ops", Input: map[string]any{"request": map[string]any{"actor_key": "ops"}}})
	if err != nil {
		t.Fatal(err)
	}
	deliveries, err := svc.ListActionDeliveries(ctx, storage.ActionDeliveryQuery{Action: "notify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "retry_scheduled" || deliveries[0].MaxAttempts != 3 || deliveries[0].NextAttempt == nil {
		t.Fatalf("delivery = %#v", deliveries)
	}
	deadLetterSvc := NewService(storage.NewMemoryStore(), Config{ActionHandlers: svc.Config().ActionHandlers})
	deadLetterSource := strings.Replace(source, `retries 2`, `retries 0`, 1)
	if resp, err := deadLetterSvc.Publish(ctx, PublishRequest{Name: "dead-letter-actions", Source: deadLetterSource}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if _, err := deadLetterSvc.EvaluateLifecycle(ctx, "dead-letter-actions", "http_request", LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/ops", Input: map[string]any{"request": map[string]any{"actor_key": "ops"}}}); err != nil {
		t.Fatal(err)
	}
	deadLetters, err := deadLetterSvc.ListActionDeliveries(ctx, storage.ActionDeliveryQuery{Status: "dead_letter"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("dead letters = %#v", deadLetters)
	}
}

func TestServiceActionRuntimeAllowlists(t *testing.T) {
	ctx := context.Background()
	source := `module "allowlisted-actions" {
  environment "prod"
  action_catalog "default" {
    action "notify" { sink "webhook" severity "high" retries 2 }
  }
  routes "http" {
    route "ops" { method "GET" pattern "/ops" }
  }
  decision_table "observe" {
    default allow
    row "notify" {
      when { true == true }
      then { decision require_review reason "notify" attributes { action "notify" sink "webhook" } metadata { severity "high" } }
      reason "notify"
      reason_code "NOTIFY"
    }
  }
  lifecycle "http_request" {
    entity "request.actor_key"
    routes "http"
    phase "post" { decision "observe" }
  }
}`
	called := 0
	svc := NewService(storage.NewMemoryStore(), Config{
		Environment: "prod",
		Runtime: RuntimePolicy{ActionAllowlists: []ActionAllowlist{
			{TenantID: "tenant-a", Environment: "prod", Actions: []string{"notify"}, Sinks: []string{"webhook"}},
		}},
		ActionHandlers: map[string]ActionHandler{
			"webhook": func(context.Context, LifecycleAction) (ActionResult, error) {
				called++
				return ActionResult{Handled: true, Status: "webhook_delivered"}, nil
			},
		},
	})
	if resp, err := svc.Publish(conditionTenant(ctx, "tenant-a"), PublishRequest{Name: "allowlisted-actions", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if resp, err := svc.Publish(conditionTenant(ctx, "tenant-b"), PublishRequest{Name: "allowlisted-actions", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	req := LifecycleEvaluateRequest{Phase: "post", Method: "GET", Path: "/ops", Input: map[string]any{"request": map[string]any{"actor_key": "ops"}}}
	allowed, err := svc.EvaluateLifecycle(conditionTenant(ctx, "tenant-a"), "allowlisted-actions", "http_request", req)
	if err != nil {
		t.Fatal(err)
	}
	if len(allowed.Evaluation.Actions) != 1 || !allowed.Evaluation.Actions[0].Handled || called != 1 {
		t.Fatalf("allowed action = %#v called=%d", allowed.Evaluation.Actions, called)
	}
	denied, err := svc.EvaluateLifecycle(conditionTenant(ctx, "tenant-b"), "allowlisted-actions", "http_request", req)
	if err != nil {
		t.Fatal(err)
	}
	if len(denied.Evaluation.Actions) != 1 || denied.Evaluation.Actions[0].Handled || denied.Evaluation.Actions[0].Result == nil || denied.Evaluation.Actions[0].Result.Status != "action_not_allowlisted" {
		t.Fatalf("denied action = %#v", denied.Evaluation.Actions)
	}
	if called != 1 {
		t.Fatalf("handler should not run for denied tenant, called=%d", called)
	}
	deliveries, err := svc.ListActionDeliveries(conditionTenant(ctx, "tenant-b"), storage.ActionDeliveryQuery{Action: "notify"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "action_not_allowlisted" {
		t.Fatalf("deliveries = %#v", deliveries)
	}
}

func TestServiceChainWatchAnalytics(t *testing.T) {
	ctx := context.Background()
	source := `module "analytics" {
  decision_table "noop" {
    default allow
    row "ok" {
      when { true == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  chain "latency_chain" {
    entity "request.actor_key"
    watch "latency" {
      event "response_observed"
      window "10m"
      distinct "attributes.user_id"
      field "attributes.duration_ms"
      metrics ["count", "rate", "min", "max", "avg", "p95", "p99"]
      step "notify" { threshold 3 action "notify" severity "high" ttl "5m" }
    }
  }
}`
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := NewService(storage.NewMemoryStore(), Config{Clock: func() time.Time { return now }})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "analytics", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	for i, duration := range []int{100, 200, 900} {
		if err := svc.Store().AppendChainEvent(ctx, storage.ChainEventRecord{
			ID:          newID("evt"),
			Definition:  "analytics",
			Environment: "development",
			Chain:       "latency_chain",
			EntityKey:   "actor-1",
			EventType:   "response_observed",
			Attributes:  map[string]any{"duration_ms": duration, "user_id": fmt.Sprintf("u-%d", i%2)},
			CreatedAt:   now.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := svc.EvaluateChain(ctx, "analytics", "latency_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "actor-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Evaluation.FinalAction != "notify" || len(resp.Evaluation.StateAfter) == 0 {
		t.Fatalf("evaluation = %#v", resp.Evaluation)
	}
	attrs := resp.Evaluation.StateAfter[0].Attributes
	if attrs["distinct_count"] != 2 || attrs["p95"] != float64(900) || attrs["avg"] != float64(400) {
		t.Fatalf("analytics attrs = %#v", attrs)
	}
}

func TestServiceChainWatchDecayCooldownAndReset(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	source := `module "risk-decay" {
  decision_table "noop" {
    default allow
    row "ok" {
      when { true == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  chain "risk_chain" {
    entity "request.actor_key"
    watch "risk_score" {
      event "risk_signal"
      window "24h"
      decay "1h"
      cooldown "10m"
      reset "remediated"
      step "notify" { threshold 1 action "notify" severity "high" ttl "30m" }
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{Clock: func() time.Time { return now }})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "risk-decay", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	if err := svc.Store().AppendChainEvent(ctx, storage.ChainEventRecord{ID: "risk-1", Definition: "risk-decay", Environment: "development", Chain: "risk_chain", EntityKey: "u-1", EventType: "risk_signal", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	first, err := svc.EvaluateChain(ctx, "risk-decay", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Evaluation.Events) != 1 || first.Evaluation.StateAfter[0].Metadata["risk_score"] == nil {
		t.Fatalf("first = %#v", first.Evaluation)
	}
	second, err := svc.EvaluateChain(ctx, "risk-decay", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Evaluation.Events) != 0 {
		t.Fatalf("cooldown emitted events: %#v", second.Evaluation.Events)
	}
	now = now.Add(time.Hour)
	if err := svc.Store().AppendChainEvent(ctx, storage.ChainEventRecord{ID: "risk-2", Definition: "risk-decay", Environment: "development", Chain: "risk_chain", EntityKey: "u-1", EventType: "risk_signal", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	third, err := svc.EvaluateChain(ctx, "risk-decay", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	score, ok := floatAny(third.Evaluation.StateAfter[0].Metadata["risk_score"])
	if !ok || score <= 2 {
		t.Fatalf("expected decayed score above 2, state=%#v", third.Evaluation.StateAfter)
	}
	if err := svc.Store().AppendChainEvent(ctx, storage.ChainEventRecord{ID: "reset-1", Definition: "risk-decay", Environment: "development", Chain: "risk_chain", EntityKey: "u-1", EventType: "remediated", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reset, err := svc.EvaluateChain(ctx, "risk-decay", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if reset.Evaluation.StateAfter[0].Action != "" || reset.Evaluation.StateAfter[0].Metadata["risk_score"] != 0.0 {
		t.Fatalf("reset = %#v", reset.Evaluation.StateAfter)
	}
}

func TestServiceCompositeWatchSuppression(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	source := `module "composite" {
  decision_table "noop" {
    default allow
    row "ok" {
      when { true == true }
      then { decision allow reason "ok" }
      reason "ok"
      reason_code "OK"
    }
  }
  chain "risk_chain" {
    entity "request.actor_key"
    watch "account_takeover" {
      event "failed_login"
      events ["failed_login", "admin_denied"]
      window "10m"
      metadata { suppress true }
      step "notify" { threshold 1 action "notify" severity "high" ttl "5m" }
    }
  }
}`
	svc := NewService(storage.NewMemoryStore(), Config{Clock: func() time.Time { return now }})
	if resp, err := svc.Publish(ctx, PublishRequest{Name: "composite", Source: source}); err != nil {
		t.Fatalf("%v: %#v", err, resp)
	}
	for _, eventType := range []string{"failed_login", "admin_denied"} {
		if err := svc.Store().AppendChainEvent(ctx, storage.ChainEventRecord{ID: newID("evt"), Definition: "composite", Environment: "development", Chain: "risk_chain", EntityKey: "u-1", EventType: eventType, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := svc.EvaluateChain(ctx, "composite", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Evaluation.FinalAction != "notify" || len(first.Evaluation.Events) != 1 {
		t.Fatalf("first evaluation = %#v", first.Evaluation)
	}
	second, err := svc.EvaluateChain(ctx, "composite", "risk_chain", ChainEvaluateRequest{Input: map[string]any{"request": map[string]any{"actor_key": "u-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Evaluation.Events) != 0 {
		t.Fatalf("suppression failed, emitted events: %#v", second.Evaluation.Events)
	}
}

func conditionTenant(ctx context.Context, tenant string) context.Context {
	return ContextWithTenant(ctx, tenant)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
