package condition

import (
	"context"
	"os"
	"path/filepath"
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
