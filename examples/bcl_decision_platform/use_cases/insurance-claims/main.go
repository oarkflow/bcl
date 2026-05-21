package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "inactive auto policy claim", Input: map[string]any{"policy": map[string]any{"active": false, "type": "auto"}, "claim": map[string]any{"amount": int64(1000), "fraud_score": int64(10), "documentation_complete": true, "loss_type": "auto"}}},
		{Name: "high fraud home claim", Input: map[string]any{"policy": map[string]any{"active": true, "type": "home"}, "claim": map[string]any{"amount": int64(18000), "fraud_score": int64(82), "documentation_complete": true, "loss_type": "home"}}},
		{Name: "small complete auto claim", Input: map[string]any{"policy": map[string]any{"active": true, "type": "auto"}, "claim": map[string]any{"amount": int64(2500), "fraud_score": int64(12), "documentation_complete": true, "loss_type": "auto"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "insurance_claims", scenario)
		fmt.Printf("claims route=%v adjuster=%s severity=%v\n", result.Attributes["route"], runner.RankID(result), result.Metadata["severity"])
	}
	runner.Batch(program, "insurance_claims", "insurance_claims_batch")
	runner.Gate(program, "insurance_claims_bundle")
}
