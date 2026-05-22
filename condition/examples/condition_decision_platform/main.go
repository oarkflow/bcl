package main

import (
	"context"
	"path/filepath"

	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example"})
	useCases := []struct {
		Name     string
		Slug     string
		Decision string
		Input    map[string]any
	}{
		{"Fraud AML", "fraud-aml", "fraud_aml", map[string]any{"customer": map[string]any{"blocked": false, "risk_score": int64(91)}, "transaction": map[string]any{"amount": int64(125000), "country": "NP"}}},
		{"Credit eligibility", "credit-eligibility", "credit_eligibility", map[string]any{"applicant": map[string]any{"credit_score": int64(742), "blacklisted": false}, "loan": map[string]any{"debt_to_income": 0.28, "amount": int64(50000)}}},
		{"Provider routing", "provider-routing", "provider_routing", map[string]any{"request": map[string]any{"channel": "sms", "country": "NP"}}},
		{"SLA escalation", "sla-escalation", "sla_escalation", map[string]any{"ticket": map[string]any{"minutes_open": int64(65), "sla_minutes": int64(60), "warning_minutes": int64(45)}}},
	}
	for _, uc := range useCases {
		_, err := svc.Publish(ctx, condition.PublishRequest{Name: uc.Slug, Path: filepath.Join("examples", "condition_decision_platform", "use_cases", uc.Slug, "decision.bcl"), RunTests: true})
		if err != nil {
			panic(err)
		}
		resp, err := svc.Evaluate(ctx, uc.Slug, condition.EvaluateRequest{Decision: uc.Decision, Input: uc.Input, IncludeFeatures: true})
		if err != nil {
			panic(err)
		}
		println(uc.Name + ": " + resp.Report.Decision.Effect)
	}
}
