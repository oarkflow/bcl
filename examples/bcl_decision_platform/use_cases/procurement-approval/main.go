package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "software request without budget", Input: map[string]any{"request": map[string]any{"amount": int64(12000), "category": "software", "department": "engineering"}, "budget": map[string]any{"available": int64(3000)}, "vendor": map[string]any{"approved": true, "risk_tier": "low"}}},
		{Name: "large hardware purchase", Input: map[string]any{"request": map[string]any{"amount": int64(9000), "category": "hardware", "department": "operations"}, "budget": map[string]any{"available": int64(15000)}, "vendor": map[string]any{"approved": true, "risk_tier": "high"}}},
		{Name: "small office purchase", Input: map[string]any{"request": map[string]any{"amount": int64(3000), "category": "office", "department": "people"}, "budget": map[string]any{"available": int64(10000)}, "vendor": map[string]any{"approved": true, "risk_tier": "low"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "procurement_approval", scenario)
		fmt.Printf("procurement route=%v approver=%s queue=%v\n", result.Attributes["route"], runner.RankID(result), result.Attributes["queue"])
	}
	runner.Batch(program, "procurement_approval", "procurement_approval_batch")
	runner.Gate(program, "procurement_approval_bundle")
}
