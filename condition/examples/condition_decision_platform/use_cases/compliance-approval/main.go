package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "compliance-approval")
	runner.Evaluate(svc, "compliance-approval", "compliance_approval", runner.Scenario{
		Name:  "clear request",
		Input: map[string]any{"request": map[string]any{"sanctions_hit": false, "export_controlled": false}},
	})
	runner.Audits(svc)
}
