package reload

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

func TestWatcherReloadsImportedBCLFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir, root := writeReloadFixture(t, allowDecisionSource())
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "demo", Path: root}); err != nil {
		t.Fatal(err)
	}

	reloads := make(chan *condition.ReloadResponse, 4)
	go func() {
		_ = (Watcher{Service: svc}).Watch(ctx, condition.ReloadRequest{Name: "demo", DebounceMillis: 50}, func(resp *condition.ReloadResponse) {
			reloads <- resp
		})
	}()
	time.Sleep(150 * time.Millisecond)

	imported := filepath.Join(dir, "rules", "access.bcl")
	if err := os.WriteFile(imported, []byte(denyDecisionSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := waitReload(t, reloads)
	if resp == nil || !resp.Reloaded {
		t.Fatalf("reload response = %#v", resp)
	}
	if !strings.HasSuffix(resp.ChangedPath, "access.bcl") && !strings.HasSuffix(resp.DependencyPath, "access.bcl") {
		t.Fatalf("expected imported path in response, got changed=%q dependency=%q", resp.ChangedPath, resp.DependencyPath)
	}
	eval, err := svc.Evaluate(ctx, "demo", condition.EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.Effect != "deny" {
		t.Fatalf("effect after imported reload = %q", eval.Report.Decision.Effect)
	}
}

func TestWatcherKeepsLastKnownGoodForInvalidImportedBCL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir, root := writeReloadFixture(t, allowDecisionSourceWithTest())
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "demo", Path: root, RunTests: true}); err != nil {
		t.Fatal(err)
	}

	reloads := make(chan *condition.ReloadResponse, 4)
	go func() {
		_ = (Watcher{Service: svc}).Watch(ctx, condition.ReloadRequest{Name: "demo", RunTests: true, DebounceMillis: 50}, func(resp *condition.ReloadResponse) {
			reloads <- resp
		})
	}()
	time.Sleep(150 * time.Millisecond)

	if err := os.WriteFile(filepath.Join(dir, "rules", "access.bcl"), []byte(denyDecisionSourceWithFailingTest()), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := waitReload(t, reloads)
	if resp == nil || resp.Reloaded || !resp.KeptLast {
		t.Fatalf("reload response = %#v", resp)
	}
	eval, err := svc.Evaluate(ctx, "demo", condition.EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.Effect != "allow" {
		t.Fatalf("last known good effect = %q", eval.Report.Decision.Effect)
	}
}

func TestWatcherReloadsRootAndDebouncesBursts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir, root := writeReloadFixture(t, allowDecisionSource())
	if err := os.WriteFile(filepath.Join(dir, "rules", "deny.bcl"), []byte(denyDecisionSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "demo", Path: root}); err != nil {
		t.Fatal(err)
	}

	reloads := make(chan *condition.ReloadResponse, 8)
	var count atomic.Int32
	go func() {
		_ = (Watcher{Service: svc}).Watch(ctx, condition.ReloadRequest{Name: "demo", DebounceMillis: 100}, func(resp *condition.ReloadResponse) {
			count.Add(1)
			reloads <- resp
		})
	}()
	time.Sleep(150 * time.Millisecond)

	rootSource := rootSource(`./rules/deny.bcl`)
	if err := os.WriteFile(root, []byte(rootSource), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte(rootSource+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := waitReload(t, reloads)
	if resp == nil || !resp.Reloaded {
		t.Fatalf("reload response = %#v", resp)
	}
	time.Sleep(250 * time.Millisecond)
	if count.Load() != 1 {
		t.Fatalf("reload callback count = %d, want 1", count.Load())
	}
	eval, err := svc.Evaluate(ctx, "demo", condition.EvaluateRequest{Decision: "access", Input: map[string]any{"request": map[string]any{"ok": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if eval.Report.Decision.Effect != "deny" {
		t.Fatalf("effect after root reload = %q", eval.Report.Decision.Effect)
	}
}

func TestWatcherRejectsSourceOnlyDefinition(t *testing.T) {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "demo", Source: rootSourceInline(allowDecisionSource())}); err != nil {
		t.Fatal(err)
	}
	err := (Watcher{Service: svc}).Watch(ctx, condition.ReloadRequest{Name: "demo"}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot be file-watched") {
		t.Fatalf("watch error = %v", err)
	}
}

func writeReloadFixture(t *testing.T, imported string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	rules := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rules, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "decision.bcl")
	if err := os.WriteFile(root, []byte(rootSource(`./rules/access.bcl`)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "access.bcl"), []byte(imported), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, root
}

func rootSource(path string) string {
	return `bcl {
  version "1.0"
}

version "1"
import "` + path + `"
`
}

func rootSourceInline(imported string) string {
	return `bcl {
  version "1.0"
}

version "1"
` + imported
}

func allowDecisionSource() string {
	return `decision_table "access" {
  default deny
  hit_policy first
  row "allow-ok" {
    when {
      request.ok == true
    }
    then {
      decision allow
      reason "ok"
    }
    reason "ok"
    reason_code "OK"
  }
}`
}

func denyDecisionSource() string {
	return `decision_table "access" {
  default allow
  hit_policy first
  row "deny-ok" {
    when {
      request.ok == true
    }
    then {
      decision deny
      reason "no"
    }
    reason "no"
    reason_code "NO"
  }
}`
}

func allowDecisionSourceWithTest() string {
	return allowDecisionSource() + `

test "allow_ok" {
  decision "access"
  input {
    request.ok true
  }
  expect {
    effect "allow"
  }
}`
}

func denyDecisionSourceWithFailingTest() string {
	return denyDecisionSource() + `

test "allow_ok" {
  decision "access"
  input {
    request.ok true
  }
  expect {
    effect "allow"
  }
}`
}

func waitReload(t *testing.T, reloads <-chan *condition.ReloadResponse) *condition.ReloadResponse {
	t.Helper()
	select {
	case resp := <-reloads:
		return resp
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reload")
		return nil
	}
}
