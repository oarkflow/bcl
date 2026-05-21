package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "reported stolen card", Input: map[string]any{"card": map[string]any{"stolen": true, "country": "US"}, "payment": map[string]any{"amount": int64(40), "risk_score": int64(90), "cross_border": false}, "customer": map[string]any{"trusted": true}}},
		{Name: "cross-border risky payment", Input: map[string]any{"card": map[string]any{"stolen": false, "country": "US"}, "payment": map[string]any{"amount": int64(800), "risk_score": int64(72), "cross_border": true}, "customer": map[string]any{"trusted": false}}},
		{Name: "trusted low-risk payment", Input: map[string]any{"card": map[string]any{"stolen": false, "country": "US"}, "payment": map[string]any{"amount": int64(35), "risk_score": int64(12), "cross_border": false}, "customer": map[string]any{"trusted": true}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "payment_risk", scenario)
		fmt.Printf("payment route=%v auth_risk=%.0f severity=%v\n", result.Attributes["route"], result.Score, result.Metadata["severity"])
	}
	runner.Batch(program, "payment_risk", "payment_risk_batch")
	runner.Gate(program, "payment_risk_bundle")
}
