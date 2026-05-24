package condition

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oarkflow/bcl"
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
