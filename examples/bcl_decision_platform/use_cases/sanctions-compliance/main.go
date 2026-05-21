package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "sanctioned counterparty", Input: map[string]any{"counterparty": map[string]any{"sanctions_hit": true, "country": "RU", "watchlist_score": int64(96)}, "shipment": map[string]any{"export_controlled": false, "destination_country": "US", "product_code": "BOOK"}}},
		{Name: "controlled export shipment", Input: map[string]any{"counterparty": map[string]any{"sanctions_hit": false, "country": "US", "watchlist_score": int64(12)}, "shipment": map[string]any{"export_controlled": true, "destination_country": "AE", "product_code": "DUAL_USE"}}},
		{Name: "clear shipment to Canada", Input: map[string]any{"counterparty": map[string]any{"sanctions_hit": false, "country": "US", "watchlist_score": int64(3)}, "shipment": map[string]any{"export_controlled": false, "destination_country": "CA", "product_code": "BOOK"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "sanctions_compliance", scenario)
		fmt.Printf("compliance route=%v match_score=%.0f reason=%s\n", result.Attributes["route"], result.Score, result.ReasonCode)
	}
	runner.Batch(program, "sanctions_compliance", "sanctions_compliance_batch")
	runner.Gate(program, "sanctions_compliance_bundle")
}
