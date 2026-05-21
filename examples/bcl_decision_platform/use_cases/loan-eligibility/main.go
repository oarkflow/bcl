package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "blocked applicant with strong score", Input: map[string]any{"applicant": map[string]any{"age": int64(42), "credit_score": int64(760), "monthly_income": int64(9000), "blacklisted": true}, "loan": map[string]any{"emi": int64(2000), "amount": int64(70000)}}},
		{Name: "borderline affordable borrower", Input: map[string]any{"applicant": map[string]any{"age": int64(29), "credit_score": int64(660), "monthly_income": int64(5000), "blacklisted": false}, "loan": map[string]any{"emi": int64(1400), "amount": int64(35000)}}},
		{Name: "prime affordable borrower", Input: map[string]any{"applicant": map[string]any{"age": int64(34), "credit_score": int64(742), "monthly_income": int64(7000), "blacklisted": false}, "loan": map[string]any{"emi": int64(1800), "amount": int64(50000)}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "loan_eligibility", scenario)
		fmt.Printf("underwriting route=%v band=%v score=%.0f\n", result.Attributes["route"], result.Attributes["band"], result.Score)
	}
	runner.Batch(program, "loan_eligibility", "loan_eligibility_batch")
	runner.Gate(program, "loan_eligibility_bundle")
}
