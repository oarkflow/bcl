package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "discount below margin floor", Input: map[string]any{"customer": map[string]any{"segment": "standard", "loyalty_years": int64(1)}, "cart": map[string]any{"subtotal": int64(200), "requested_discount": int64(20), "margin_after_discount": int64(8)}, "inventory": map[string]any{"demand": "normal", "stock_level": int64(20)}}},
		{Name: "enterprise large discount request", Input: map[string]any{"customer": map[string]any{"segment": "enterprise", "loyalty_years": int64(4)}, "cart": map[string]any{"subtotal": int64(2000), "requested_discount": int64(30), "margin_after_discount": int64(25)}, "inventory": map[string]any{"demand": "high", "stock_level": int64(8)}}},
		{Name: "loyalty discount", Input: map[string]any{"customer": map[string]any{"segment": "pro", "loyalty_years": int64(3)}, "cart": map[string]any{"subtotal": int64(400), "requested_discount": int64(12), "margin_after_discount": int64(28)}, "inventory": map[string]any{"demand": "low", "stock_level": int64(120)}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "dynamic_pricing", scenario)
		fmt.Printf("pricing route=%v pricing_risk=%.0f reason=%s\n", result.Attributes["route"], result.Score, result.ReasonCode)
	}
	runner.Batch(program, "dynamic_pricing", "dynamic_pricing_batch")
	runner.Gate(program, "dynamic_pricing_bundle")
}
