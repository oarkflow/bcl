package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/oarkflow/bcl"
)

func main() {
	run("loan prime applicant", "packages/loan_eligibility.bcl", "loan-application", map[string]any{
		"applicant": map[string]any{
			"age":            int64(31),
			"credit_score":   int64(735),
			"monthly_income": int64(6000),
			"blacklisted":    false,
		},
		"loan": map[string]any{
			"emi": int64(1500),
		},
	})

	run("support critical enterprise ticket", "packages/support_priority.bcl", "ticket-routing", map[string]any{
		"ticket": map[string]any{
			"severity":  "critical",
			"age_hours": int64(2),
			"topic":     "database",
		},
		"customer": map[string]any{
			"plan": "enterprise",
		},
	})

	run("AI malware generation", "packages/ai_guardrails.bcl", "response-safety", map[string]any{
		"ai": map[string]any{
			"intent":     "malware_generation",
			"risk_score": int64(95),
			"confidence": 0.92,
		},
	})

	runScenario("packages/loan_eligibility.bcl", "scenarios/loan_prime.yaml")
}

func run(name, path, decision string, input map[string]any) {
	program, err := bcl.CompileDecisionFile("examples/bcl/"+path, &bcl.Options{
		AllowTime: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	result, err := bcl.EvaluateDecision(program, decision, input, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== %s ==\n", name)
	printJSON(result)
}

func printJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}

func runScenario(programPath, scenarioPath string) {
	program, err := bcl.CompileDecisionFile("examples/bcl/"+programPath, &bcl.Options{
		AllowTime: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	scenario, err := bcl.ReadDecisionScenarioFile("examples/bcl/" + scenarioPath)
	if err != nil {
		log.Fatal(err)
	}
	result, err := bcl.EvaluateDecisionScenario(program, scenario, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== scenario: %s ==\n", scenario.Name)
	printJSON(result)
}
