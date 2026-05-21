package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "malware generation prompt", Input: map[string]any{"ai": map[string]any{"intent": "malware_generation", "risk_score": int64(95), "confidence": 0.94}}},
		{Name: "uncertain dual-use security request", Input: map[string]any{"ai": map[string]any{"intent": "dual_use_security", "risk_score": int64(78), "confidence": 0.72}}},
		{Name: "benign productivity help", Input: map[string]any{"ai": map[string]any{"intent": "benign_help", "risk_score": int64(12), "confidence": 0.95}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "ai_safety", scenario)
		fmt.Printf("safety action=%v safety_score=%.0f reason=%s\n", result.Attributes["route"], result.Score, result.ReasonCode)
	}
	runner.Batch(program, "ai_safety", "ai_safety_batch")
	runner.Gate(program, "ai_safety_bundle")
}
