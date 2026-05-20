package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/oarkflow/bcl"
)

func main() {
	runWithEngine("loan prime applicant", "packages/loan_eligibility.bcl", "loan-application", map[string]any{
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

	runWithEngine("support critical enterprise ticket", "packages/support_priority.bcl", "ticket-routing", map[string]any{
		"ticket": map[string]any{
			"severity":  "critical",
			"age_hours": int64(2),
			"topic":     "database",
		},
		"customer": map[string]any{
			"plan": "enterprise",
		},
	})

	runWithEngine("AI malware generation", "packages/ai_guardrails.bcl", "response-safety", map[string]any{
		"ai": map[string]any{
			"intent":     "malware_generation",
			"risk_score": int64(95),
			"confidence": 0.92,
		},
	})

	runWithEngine("procurement small funded request", "packages/procurement_workflow.bcl", "procurement-request", map[string]any{
		"request": map[string]any{"amount": int64(3000)},
		"budget":  map[string]any{"available": int64(10000)},
	})

	runWithEngine("telecom route with provider ranking", "packages/telecom_optimization.bcl", "sms-route", map[string]any{
		"recipient": map[string]any{
			"country":    "NP",
			"phone_hash": "stable-customer-key",
		},
		"message": map[string]any{
			"compliance_checked": true,
		},
	})

	runStrictValidationExample()
	runInlinePatternDecision()
	runScenario("packages/loan_eligibility.bcl", "scenarios/loan_prime.yaml")
	runInlineScenario()
}

func runWithEngine(name, path, decision string, input map[string]any) {
	program, err := compileDecisionPackage(path)
	if err != nil {
		log.Fatal(err)
	}
	engine := bcl.NewDecisionEngine(program, nil)
	result, err := engine.Evaluate(decision, input)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== %s ==\n", name)
	printJSON(result)
}

func runStrictValidationExample() {
	program := compileInlineDecision(`
module "validation-demo" {
  schema validation-demo {
    required request object {
      required amount number min 10
      required active bool
    }
  }

  decision_schema "validation-demo" {
    effects [allow, deny]
    default deny
    strategy deny_overrides
  }

  policy "validation-demo" {
    allow "valid-active-request" when {
      request.amount >= 10
      request.active == true
    } reason "request is valid and active"
  }
}`)
	engine := bcl.NewDecisionEngine(program, nil)
	result, err := engine.EvaluateWithOptions("validation-demo", map[string]any{
		"request": map[string]any{
			"amount": int64(5),
			"active": "yes",
		},
	}, bcl.DecisionEvaluateOptions{Explain: true, ValidateInput: true, Strict: true})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== strict input validation ==\n")
	printJSON(result)
}

func runInlinePatternDecision() {
	program := compileInlineDecision(`
module "inline-risk" {
  schema inline-risk {
    required request object {
      required kind string
      required amount number min 1
      required tags list
    }
  }

  decision_schema "inline-risk" {
    effects [allow, deny, require_review]
    default require_review
    strategy deny_overrides
  }

  predicate "priority_loan" {
    match(request, case({kind: "loan", tags: ["priority", ...rest]} if request.amount <= 50000, true), false)
  }

  policy "inline-risk" {
    deny "deny-huge-request" when {
      request.amount > 100000
    } reason "large request exceeds automatic limit"

    allow "allow-priority-loan" when {
      predicate.priority_loan
    } reason "priority loan matches pattern"
  }

  rule_set "inline-risk" {
    rule "score-amount-band" {
      priority 20
      when { match(request, case({amount: amount:number}, amount >= 25000), false) }
      then {
        score += 10
        event risk_signal { type "amount_band" }
      }
      reason "amount is in review scoring band"
    }
  }
}`)
	engine := bcl.NewDecisionEngine(program, nil)
	result, err := engine.Evaluate("inline-risk", map[string]any{
		"request": map[string]any{
			"kind":   "loan",
			"amount": int64(30000),
			"tags":   []any{"priority", "existing-customer"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== inline BCL pattern matching decision ==\n")
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
	program, err := compileDecisionPackage(programPath)
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

func runInlineScenario() {
	program, err := compileDecisionPackage("packages/ai_guardrails.bcl")
	if err != nil {
		log.Fatal(err)
	}
	engine := bcl.NewDecisionEngine(program, nil)
	result, err := engine.EvaluateScenario(&bcl.DecisionScenario{
		Name:     "high risk AI response creates review case",
		Decision: "response-safety",
		Input: map[string]any{
			"ai": map[string]any{
				"intent":     "summarization",
				"risk_score": int64(72),
				"confidence": 0.7,
			},
		},
		Expect: map[string]any{
			"effect":    "require_review",
			"allowed":   false,
			"policy_id": "review-low-confidence",
			"score":     int64(25),
			"action":    "create_case",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== inline scenario with extended expectations ==\n")
	printJSON(result)
}

func compileDecisionPackage(path string) (*bcl.DecisionProgram, error) {
	return bcl.CompileDecisionFile("examples/bcl/"+path, &bcl.Options{
		AllowTime: true,
	})
}

func compileInlineDecision(src string) *bcl.DecisionProgram {
	doc, err := bcl.Parse([]byte(strings.TrimSpace(src)))
	if err != nil {
		log.Fatal(err)
	}
	program, err := bcl.CompileDecisionDocument(doc, &bcl.Options{AllowTime: true})
	if err != nil {
		log.Fatal(err)
	}
	return program
}
