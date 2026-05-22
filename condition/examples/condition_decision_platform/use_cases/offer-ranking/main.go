package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "offer-ranking")
	runner.Evaluate(svc, "offer-ranking", "offer_ranking", runner.Scenario{
		Name:  "premium offer",
		Input: map[string]any{"customer": map[string]any{"segment": "premium", "opted_out": false}},
	})
	runner.Audits(svc)
}
