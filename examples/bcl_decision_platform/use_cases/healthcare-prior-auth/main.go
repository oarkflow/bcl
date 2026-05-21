package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "inactive member MRI request", Input: map[string]any{"member": map[string]any{"coverage_active": false, "plan": "silver"}, "request": map[string]any{"procedure": "MRI", "urgency": "routine", "medical_necessity_score": int64(92), "prior_denial": false}}},
		{Name: "urgent surgical authorization", Input: map[string]any{"member": map[string]any{"coverage_active": true, "plan": "gold"}, "request": map[string]any{"procedure": "surgery", "urgency": "urgent", "medical_necessity_score": int64(82), "prior_denial": false}}},
		{Name: "routine physical therapy", Input: map[string]any{"member": map[string]any{"coverage_active": true, "plan": "gold"}, "request": map[string]any{"procedure": "physical_therapy", "urgency": "routine", "medical_necessity_score": int64(90), "prior_denial": false}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "healthcare_prior_auth", scenario)
		fmt.Printf("clinical route=%v reviewer=%s queue=%v\n", result.Attributes["route"], runner.RankID(result), result.Attributes["queue"])
	}
	runner.Batch(program, "healthcare_prior_auth", "healthcare_prior_auth_batch")
	runner.Gate(program, "healthcare_prior_auth_bundle")
}
