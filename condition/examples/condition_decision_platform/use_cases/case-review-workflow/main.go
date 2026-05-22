package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "case-review-workflow")
	runner.Evaluate(svc, "case-review-workflow", "case_review_workflow", runner.Scenario{
		Name:  "critical senior review",
		Input: map[string]any{"case": map[string]any{"amount": int64(125000), "severity": "critical", "status": "open"}},
	})
	runner.Audits(svc)
}
