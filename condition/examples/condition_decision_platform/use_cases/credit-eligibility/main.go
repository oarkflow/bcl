package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "credit-eligibility")
	runner.Evaluate(svc, "credit-eligibility", "credit_eligibility", runner.Scenario{
		Name: "prime approval",
		Input: map[string]any{
			"applicant": map[string]any{"credit_score": int64(742), "blacklisted": false},
			"loan":      map[string]any{"debt_to_income": 0.28, "amount": int64(50000)},
		},
	})
	runner.Audits(svc)
}
