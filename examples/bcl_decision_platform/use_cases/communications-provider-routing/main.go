package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "unchecked SMS campaign", Input: map[string]any{"user": map[string]any{"id": "u-1", "country": "NP", "tier": "standard"}, "message": map[string]any{"channel": "sms", "type": "marketing", "compliance_checked": false, "quality_floor": 0.98}}},
		{Name: "Nepal OTP over best SMS provider", Input: map[string]any{"user": map[string]any{"id": "u-2", "country": "NP", "tier": "premium"}, "message": map[string]any{"channel": "sms", "type": "otp", "compliance_checked": true, "quality_floor": 0.98}}},
		{Name: "US transactional email", Input: map[string]any{"user": map[string]any{"id": "u-3", "country": "US", "tier": "standard"}, "message": map[string]any{"channel": "email", "type": "transactional", "compliance_checked": true, "quality_floor": 0.99}}},
		{Name: "unsupported SMS destination", Input: map[string]any{"user": map[string]any{"id": "u-4", "country": "IR", "tier": "standard"}, "message": map[string]any{"channel": "sms", "type": "otp", "compliance_checked": true, "quality_floor": 0.98}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "communications_provider_routing", scenario)
		fmt.Printf("provider route=%v selected=%s score=%.3f reason=%q reason_code=%s\n", result.Attributes["route"], runner.RankID(result), runner.RankScore(result), result.Reason, result.ReasonCode)
	}
	runner.Batch(program, "communications_provider_routing", "communications_provider_routing_batch")
	runner.Gate(program, "communications_provider_routing_bundle")
}
