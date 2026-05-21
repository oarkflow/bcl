package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "blacklisted retail card payment", Input: map[string]any{"customer": map[string]any{"blacklisted": true, "country": "US"}, "transaction": map[string]any{"amount": int64(200), "channel": "card"}}},
		{Name: "VIP wire through AML corridor", Input: map[string]any{"customer": map[string]any{"blacklisted": false, "country": "NP", "tier": "vip"}, "transaction": map[string]any{"amount": int64(125000), "channel": "wire"}}},
		{Name: "low-risk domestic card payment", Input: map[string]any{"customer": map[string]any{"blacklisted": false, "country": "US"}, "transaction": map[string]any{"amount": int64(500), "channel": "card"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "fraud_aml", scenario)
		fmt.Printf("fraud queue=%v ranked_queue=%s score=%.0f reason=%s\n", result.Attributes["queue"], runner.RankID(result), result.Score, result.ReasonCode)
	}
	runner.Batch(program, "fraud_aml", "fraud_aml_batch")
	runner.Gate(program, "fraud_aml_bundle")
}
