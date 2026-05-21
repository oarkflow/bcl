package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "sanctions match at onboarding", Input: map[string]any{"applicant": map[string]any{"sanctions_match": true, "identity_score": int64(98), "document_quality": int64(98), "country": "US"}}},
		{Name: "weak identity evidence", Input: map[string]any{"applicant": map[string]any{"sanctions_match": false, "identity_score": int64(72), "document_quality": int64(86), "country": "US"}}},
		{Name: "verified cross-border applicant", Input: map[string]any{"applicant": map[string]any{"sanctions_match": false, "identity_score": int64(94), "document_quality": int64(96), "country": "NP"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "kyc_onboarding", scenario)
		fmt.Printf("kyc route=%v assurance_score=%.0f severity=%v\n", result.Attributes["route"], result.Score, result.Metadata["severity"])
	}
	runner.Batch(program, "kyc_onboarding", "kyc_onboarding_batch")
	runner.Gate(program, "kyc_onboarding_bundle")
}
