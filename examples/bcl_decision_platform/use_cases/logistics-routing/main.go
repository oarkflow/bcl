package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "hazmat shipment with uncertified carrier", Input: map[string]any{"shipment": map[string]any{"hazmat": true, "weight": int64(50), "service": "standard"}, "carrier": map[string]any{"hazmat_certified": false, "capacity_available": true, "express_enabled": true}}},
		{Name: "heavy freight capacity review", Input: map[string]any{"shipment": map[string]any{"hazmat": false, "weight": int64(1200), "service": "standard"}, "carrier": map[string]any{"hazmat_certified": true, "capacity_available": true, "express_enabled": true}}},
		{Name: "express parcel", Input: map[string]any{"shipment": map[string]any{"hazmat": false, "weight": int64(20), "service": "express"}, "carrier": map[string]any{"hazmat_certified": true, "capacity_available": true, "express_enabled": true}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "logistics_routing", scenario)
		fmt.Printf("dispatch route=%v carrier=%s rank_score=%.2f queue=%v\n", result.Attributes["route"], runner.RankID(result), runner.RankScore(result), result.Attributes["queue"])
	}
	runner.Batch(program, "logistics_routing", "logistics_routing_batch")
	runner.Gate(program, "logistics_routing_bundle")
}
