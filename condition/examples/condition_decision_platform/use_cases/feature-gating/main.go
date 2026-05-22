package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "feature-gating")
	runner.Evaluate(svc, "feature-gating", "feature_gating", runner.Scenario{
		Name: "enterprise beta access",
		Input: map[string]any{
			"subject": map[string]any{"tenant_disabled": false, "plan": "enterprise"},
			"feature": map[string]any{"enabled": true, "beta": false},
		},
	})
	runner.Audits(svc)
}
