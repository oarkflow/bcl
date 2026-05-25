package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type PlanCatalog struct {
	Features map[string][]string
	Quotas   map[string]int
}

type TenantSubscription struct {
	ID    string
	Plan  string
	Usage map[string]int
}

type FeatureRequest struct {
	ID      string
	Feature string
	Units   int
}

type EntitlementCase struct {
	Name    string
	Tenant  TenantSubscription
	Request FeatureRequest
	Catalog PlanCatalog
}

type EntitlementContext struct {
	PlanAllowsFeature bool
	UsagePercent      int
	RemainingUnits    int
	Facts             map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "subscription-entitlements", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range entitlementChecks() {
		ent := resolveEntitlement(c)
		resp, err := svc.Evaluate(ctx, "subscription-entitlements", condition.EvaluateRequest{Decision: "entitlement_check", Input: ent.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Request.ID, c.Name)
		fmt.Printf("  entitlement: plan=%s allows=%v usage=%d%% remaining=%d\n", c.Tenant.Plan, ent.PlanAllowsFeature, ent.UsagePercent, ent.RemainingUnits)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		enforceEntitlement(c, ent, decision.Effect)
	}
}

func entitlementChecks() []EntitlementCase {
	catalog := PlanCatalog{Features: map[string][]string{"starter": {"basic_reports"}, "pro": {"basic_reports", "api"}, "enterprise": {"basic_reports", "api", "advanced_ai"}}, Quotas: map[string]int{"starter": 1000, "pro": 10000, "enterprise": 100000}}
	return []EntitlementCase{
		{Name: "pro api call", Tenant: TenantSubscription{ID: "ten-1", Plan: "pro", Usage: map[string]int{"api": 4200}}, Request: FeatureRequest{ID: "feat-1", Feature: "api", Units: 1}, Catalog: catalog},
		{Name: "starter wants advanced ai", Tenant: TenantSubscription{ID: "ten-2", Plan: "starter", Usage: map[string]int{"advanced_ai": 0}}, Request: FeatureRequest{ID: "feat-2", Feature: "advanced_ai", Units: 1}, Catalog: catalog},
		{Name: "pro quota exhausted", Tenant: TenantSubscription{ID: "ten-3", Plan: "pro", Usage: map[string]int{"api": 10050}}, Request: FeatureRequest{ID: "feat-3", Feature: "api", Units: 1}, Catalog: catalog},
	}
}

func resolveEntitlement(c EntitlementCase) EntitlementContext {
	quota := c.Catalog.Quotas[c.Tenant.Plan]
	used := c.Tenant.Usage[c.Request.Feature]
	usagePercent := 0
	if quota > 0 {
		usagePercent = used * 100 / quota
	}
	allows := contains(c.Catalog.Features[c.Tenant.Plan], c.Request.Feature)
	return EntitlementContext{
		PlanAllowsFeature: allows,
		UsagePercent:      usagePercent,
		RemainingUnits:    quota - used,
		Facts: map[string]any{
			"tenant":  map[string]any{"id": c.Tenant.ID, "plan": c.Tenant.Plan, "plan_allows_feature": allows, "usage_percent": usagePercent},
			"feature": map[string]any{"name": c.Request.Feature, "enabled": true},
		},
	}
}

func enforceEntitlement(c EntitlementCase, ent EntitlementContext, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  issue signed entitlement token, units_remaining=%d\n", ent.RemainingUnits)
	case "require_review":
		fmt.Printf("  throttle request and create upgrade recommendation\n")
	default:
		fmt.Printf("  show upgrade wall for feature %s\n", c.Request.Feature)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
