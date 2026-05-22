package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "blocked analyst read attempt", Input: map[string]any{"subject": map[string]any{"blocked": true, "role": "analyst", "mfa": true}, "resource": map[string]any{"sensitivity": "normal", "required_role": "analyst"}, "request": map[string]any{"action": "read"}}},
		{Name: "admin without MFA on high sensitivity resource", Input: map[string]any{"subject": map[string]any{"blocked": false, "role": "admin", "mfa": false}, "resource": map[string]any{"sensitivity": "high", "required_role": "admin"}, "request": map[string]any{"action": "admin"}}},
		{Name: "MFA analyst normal read", Input: map[string]any{"subject": map[string]any{"blocked": false, "role": "analyst", "mfa": true}, "resource": map[string]any{"sensitivity": "normal", "required_role": "analyst"}, "request": map[string]any{"action": "read"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "iam_access", scenario)
		fmt.Printf("access route=%v decision=%s risk_score=%.0f\n", result.Attributes["route"], result.Effect, result.Score)
	}
	runner.Batch(program, "iam_access", "iam_access_batch")
	runner.Gate(program, "iam_access_bundle")
	runner.Platform(program, "iam_access", "iam_access_bundle", scenarios[2])
}
