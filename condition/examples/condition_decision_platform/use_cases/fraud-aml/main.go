package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "fraud-aml")
	runner.Evaluate(svc, "fraud-aml", "fraud_aml", runner.Scenario{
		Name: "high-risk payment review",
		Input: map[string]any{
			"customer":    map[string]any{"blocked": false, "risk_score": int64(91)},
			"transaction": map[string]any{"amount": int64(125000), "country": "NP"},
		},
	})
	runner.Audits(svc)
}
