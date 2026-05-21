package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/oarkflow/bcl"
)

type demoCase struct {
	Name     string
	Slug     string
	Decision string
	Input    map[string]any
}

func main() {
	cases := []demoCase{
		{
			Name:     "Fraud AML high-value review",
			Slug:     "fraud-aml",
			Decision: "fraud_aml",
			Input: map[string]any{
				"customer":    map[string]any{"blacklisted": false, "country": "NP", "tier": "vip"},
				"transaction": map[string]any{"amount": int64(125000), "channel": "wire"},
			},
		},
		{
			Name:     "Loan prime approval",
			Slug:     "loan-eligibility",
			Decision: "loan_eligibility",
			Input: map[string]any{
				"applicant": map[string]any{"age": int64(34), "credit_score": int64(742), "monthly_income": int64(7000), "blacklisted": false},
				"loan":      map[string]any{"emi": int64(1800), "amount": int64(50000)},
			},
		},
		{
			Name:     "AI safety blocked intent",
			Slug:     "ai-safety",
			Decision: "ai_safety",
			Input: map[string]any{
				"ai": map[string]any{"intent": "malware_generation", "risk_score": int64(95), "confidence": 0.94},
			},
		},
		{
			Name:     "Communications provider routing",
			Slug:     "communications-provider-routing",
			Decision: "communications_provider_routing",
			Input: map[string]any{
				"user":    map[string]any{"id": "u-2", "country": "NP", "tier": "premium"},
				"message": map[string]any{"channel": "sms", "type": "otp", "compliance_checked": true, "quality_floor": 0.98},
			},
		},
	}

	for _, c := range cases {
		path := filepath.Join("examples", "bcl_decision_platform", "use_cases", c.Slug, "decision.bcl")
		program, err := bcl.CompileDecisionFile(path, &bcl.Options{AllowTime: true, Verbose: verbose()})
		if err != nil {
			log.Fatal(err)
		}
		engine := bcl.NewDecisionEngine(program, &bcl.Options{Verbose: verbose()})
		result, err := engine.Evaluate(c.Decision, c.Input)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\n== %s ==\n", c.Name)
		if verbose() {
			printJSON(result)
		} else {
			printJSON(result.Answer())
		}

		batch, err := bcl.EvaluateDecisionDataset(program, c.Decision, c.Decision+"_batch", &bcl.Options{Verbose: verbose()})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("batch effects: ")
		printJSON(batch.EffectCounts)

		gates, err := bcl.EvaluateDecisionGates(program, c.Decision+"_bundle", nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("gate passed: %v\n", gates.Passed)
	}
}

func printJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}

func verbose() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BCL_VERBOSE"))) {
	case "1", "true", "yes", "on", "verbose":
		return true
	default:
		return false
	}
}
