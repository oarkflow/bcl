package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "sla-escalation")
	runner.Evaluate(svc, "sla-escalation", "sla_escalation", runner.Scenario{
		Name:  "breached SLA",
		Input: map[string]any{"ticket": map[string]any{"minutes_open": int64(65), "sla_minutes": int64(60), "warning_minutes": int64(45)}},
	})
	runner.Audits(svc)
}
