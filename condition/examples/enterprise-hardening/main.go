package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"

	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

func main() {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{
		Environment:      "production",
		DefaultTenant:    "default",
		StrictValidation: true,
		StrictEvaluation: true,
		RequireTests:     true,
		Runtime: condition.RuntimePolicy{
			FixedTime:              "2026-05-26T00:00:00Z",
			AllowedDatasetAdapters: []string{"file"},
			AllowedHTTPMethods:     []string{"GET"},
			ExternalTimeout:        2 * time.Second,
		},
	})

	for _, tenant := range []string{"acme", "globex"} {
		tenantCtx := condition.ContextWithTenant(ctx, tenant)
		if _, err := svc.Publish(tenantCtx, condition.PublishRequest{
			Name:     "enterprise-hardening",
			Version:  "1",
			Path:     filepath.Join(dir, "decision.bcl"),
			RunTests: true,
			Metadata: map[string]any{
				"approved": true,
				"owner":    tenant + "-security",
			},
		}); err != nil {
			log.Fatal(err)
		}
	}

	evalCtx := condition.ContextWithTenant(ctx, "acme")
	eval, err := svc.Evaluate(evalCtx, "enterprise-hardening", condition.EvaluateRequest{
		Decision: "access",
		Input: map[string]any{
			"request": map[string]any{
				"user":   map[string]any{"active": true},
				"action": "read",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("tenant=%s effect=%s reason_code=%s\n", eval.Audit.TenantID, eval.Report.Decision.Effect, eval.Report.Decision.ReasonCode)

	canary, err := svc.Canary(evalCtx, "enterprise-hardening", condition.CanaryRequest{
		SimulationRequest: condition.SimulationRequest{
			CandidatePath: filepath.Join(dir, "candidate.bcl"),
			Decision:      "access",
			Dataset:       "canary_cases",
		},
		RequireNoErrors: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("canary_passed=%v changed_cases=%d\n", canary.Passed, canary.ChangedCases)

	if _, err := svc.Evaluate(condition.ContextWithTenant(ctx, "initech"), "enterprise-hardening", condition.EvaluateRequest{Decision: "access"}); err != nil {
		fmt.Printf("isolated_tenant_error=%s\n", err)
	}
}
