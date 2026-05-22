package main

import "github.com/oarkflow/condition/examples/condition_decision_platform/use_cases/internal/runner"

func main() {
	svc := runner.Service(runner.CallerFile(), "provider-routing")
	runner.Evaluate(svc, "provider-routing", "provider_routing", runner.Scenario{
		Name:  "SMS Nepal route",
		Input: map[string]any{"request": map[string]any{"channel": "sms", "country": "NP"}},
	})
	runner.Audits(svc)
}
