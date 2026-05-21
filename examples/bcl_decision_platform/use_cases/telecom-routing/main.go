package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "unchecked high-priority SMS", Input: map[string]any{"recipient": map[string]any{"country": "NP", "phone_hash": "p1"}, "message": map[string]any{"compliance_checked": false, "priority": "high"}, "provider": map[string]any{"active": true, "supports_country": true}}},
		{Name: "unsupported destination fallback", Input: map[string]any{"recipient": map[string]any{"country": "BR", "phone_hash": "p2"}, "message": map[string]any{"compliance_checked": true, "priority": "normal"}, "provider": map[string]any{"active": true, "supports_country": false}}},
		{Name: "premium compliant route", Input: map[string]any{"recipient": map[string]any{"country": "US", "phone_hash": "p3"}, "message": map[string]any{"compliance_checked": true, "priority": "high"}, "provider": map[string]any{"active": true, "supports_country": true}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "telecom_routing", scenario)
		fmt.Printf("telecom path=%s route=%v severity=%v\n", runner.RankID(result), result.Attributes["route"], result.Metadata["severity"])
	}
	runner.Batch(program, "telecom_routing", "telecom_routing_batch")
	runner.Gate(program, "telecom_routing_bundle")
}
