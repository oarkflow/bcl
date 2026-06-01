package condition

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oarkflow/bcl/condition/pkg/storage"
)

func BenchmarkServicePublishStrict(b *testing.B) {
	ctx := context.Background()
	baseDir := benchmarkModuleDir(b)
	for i := 0; i < b.N; i++ {
		svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, RequireTests: true})
		if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "1", Source: strictDemoSource(), BaseDir: baseDir, RunTests: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceEvaluateStrict(b *testing.B) {
	ctx := context.Background()
	baseDir := benchmarkModuleDir(b)
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, StrictEvaluation: true, RequireTests: true})
	if _, err := svc.PublishVersion(ctx, PublishRequest{Name: "demo", Version: "1", Source: strictDemoSource(), BaseDir: baseDir, RunTests: true}); err != nil {
		b.Fatal(err)
	}
	if _, err := svc.Approve(ctx, "demo", "1", ApprovalRequest{ApprovedBy: "bench"}); err != nil {
		b.Fatal(err)
	}
	if _, err := svc.Activate(ctx, "demo", "1", "development"); err != nil {
		b.Fatal(err)
	}
	input := map[string]any{"request": map[string]any{"ok": true}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceEvaluateWithShadow(b *testing.B) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Source: demoSource}); err != nil {
		b.Fatal(err)
	}
	candidate := `module "demo" {
  decision_table "access" {
    default deny
    hit_policy first
    row "deny-ok" { when { request.ok == true } then { decision deny reason "shadow" } reason "shadow" reason_code "SHADOW" }
  }
}`
	input := map[string]any{"request": map[string]any{"ok": true}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: input, ShadowCandidateSource: candidate}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceEvaluateStrictConcurrent(b *testing.B) {
	ctx := context.Background()
	baseDir := benchmarkModuleDir(b)
	svc := NewService(storage.NewMemoryStore(), Config{StrictValidation: true, StrictEvaluation: true, RequireTests: true})
	if _, err := svc.Publish(ctx, PublishRequest{Name: "demo", Version: "1", Source: strictDemoSource(), BaseDir: baseDir, RunTests: true}); err != nil {
		b.Fatal(err)
	}
	input := map[string]any{"request": map[string]any{"ok": true}}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: input}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkServiceExternalDatasetDenied(b *testing.B) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	source := `module "external" {
  decision_table "access" { default deny hit_policy first }
  dataset "batch" { source { adapter file path "./batch.jsonl" format jsonl } }
}`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "external", Source: source}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.Evaluate(ctx, "external", EvaluateRequest{Decision: "access"})
	}
}

func BenchmarkServiceTenantPartitionedEvaluate(b *testing.B) {
	svc := NewService(storage.NewMemoryStore(), Config{})
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if _, err := svc.Publish(ContextWithTenant(context.Background(), tenant), PublishRequest{Name: "demo", Source: demoSource}); err != nil {
			b.Fatal(err)
		}
	}
	ctx := ContextWithTenant(context.Background(), "tenant-b")
	input := map[string]any{"request": map[string]any{"ok": true}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Evaluate(ctx, "demo", EvaluateRequest{Decision: "access", Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceLargeDecisionTable(b *testing.B) {
	ctx := context.Background()
	svc := NewService(storage.NewMemoryStore(), Config{})
	var rows strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&rows, `row "r-%d" { when { request.code == "%d" } then { decision allow reason "hit" } reason "hit" reason_code "HIT_%d" }`, i, i, i)
		rows.WriteByte('\n')
	}
	source := `module "large" { decision_table "access" { default deny hit_policy first ` + rows.String() + ` } }`
	if _, err := svc.Publish(ctx, PublishRequest{Name: "large", Source: source}); err != nil {
		b.Fatal(err)
	}
	input := map[string]any{"request": map[string]any{"code": "499"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := svc.Evaluate(ctx, "large", EvaluateRequest{Decision: "access", Input: input}); err != nil {
			b.Fatal(err)
		}
	}
}

func strictDemoSource() string {
	return `bcl {
  version "1.0"
  strict true
}

module "demo" {
  source "./module.bcl"
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
    expect { effect "allow" reason_code "OK" diagnostics "none" }
  }
}`
}

func benchmarkModuleDir(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "module.bcl"), []byte(`bcl { version "1.0" }`), 0o644); err != nil {
		b.Fatal(err)
	}
	return dir
}
