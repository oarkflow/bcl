package bcl

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDottedAssignmentWithEqualsParses(t *testing.T) {
	doc, err := Parse([]byte(`test "dotted" {
  input {
    customer.blacklisted = true
    customer.country "NP"
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := Compile(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	testBody := n.Tests[0]["body"].(map[string]any)
	input := testBody["input"].(map[string]any)
	customer := input["customer"].(map[string]any)
	if customer["blacklisted"] != true || customer["country"] != "NP" {
		t.Fatalf("bad dotted input: %#v", input)
	}
}

func TestDecisionExamplesCompile(t *testing.T) {
	paths := []string{"examples/bcl_decision_platform/fraud.bcl"}
	matches, err := filepath.Glob("examples/bcl/packages/*.bcl")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, matches...)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			prog, err := CompileDecisionFile(path, &Options{AllowTime: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(prog.Decisions) == 0 && len(prog.Constants) == 0 {
				t.Fatalf("missing decisions in %s", path)
			}
		})
	}
}

func TestDecisionEvaluateFraudBlacklisted(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl_decision_platform/fraud.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "fraud-review", map[string]any{
		"customer":    map[string]any{"blacklisted": true, "country": "NP"},
		"transaction": map[string]any{"amount": int64(1000)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "deny" || result.Allowed {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionEvaluateFraudReviewMarket(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl_decision_platform/fraud.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "fraud-review", map[string]any{
		"customer":    map[string]any{"blacklisted": false, "country": "NP"},
		"transaction": map[string]any{"amount": int64(125000)},
		"candidates": []any{
			map[string]any{"id": "aml", "facts": map[string]any{"active": true, "priority": int64(9), "load": 0.2}},
			map[string]any{"id": "standard", "facts": map[string]any{"active": true, "priority": int64(5), "load": 0.5}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "require_review" {
		t.Fatalf("decision = %#v", result)
	}
	if result.Rank == nil || result.Rank.ID != "aml" {
		t.Fatalf("rank = %#v", result.Rank)
	}
}

func TestDecisionRuleSetRecordsScoreAndActions(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl_decision_platform/fraud.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "fraud-review", map[string]any{
		"customer":    map[string]any{"blacklisted": false, "country": "US", "tier": "vip"},
		"transaction": map[string]any{"amount": int64(125000)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "require_review" || result.Score != 40 || len(result.Actions) != 1 || result.Actions[0].Name != "notify" {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionEvaluateSupportPriorityRank(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl/packages/support_priority.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "ticket-routing", map[string]any{
		"ticket":   map[string]any{"severity": "critical", "age_hours": int64(2), "topic": "database"},
		"customer": map[string]any{"plan": "enterprise"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "escalate" || result.Rank == nil || result.Rank.ID != "database" {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionEvaluateLoanPrimeApplicant(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl/packages/loan_eligibility.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "loan-application", map[string]any{
		"applicant": map[string]any{"age": int64(31), "credit_score": int64(735), "monthly_income": int64(6000), "blacklisted": false},
		"loan":      map[string]any{"emi": int64(1500)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || !result.Allowed {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionScenarioYAML(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl/packages/loan_eligibility.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	scenario, err := ReadDecisionScenarioFile("examples/bcl/scenarios/loan_prime.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecisionScenario(prog, scenario, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || result.Decision == nil || result.Decision.Effect != "allow" {
		t.Fatalf("scenario = %#v", result)
	}
}

func TestDecisionContractRejectsUnknownEffect(t *testing.T) {
	doc, err := Parse([]byte(`module "custom" {
  decision_schema "demo" {
    effects [allow, deny]
    default deny
  }

  policy "demo" {
    hold "manual-hold" when { request.amount > 10 } reason "custom effect"
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileDecisionDocument(doc, &Options{})
	if err == nil {
		t.Fatal("expected contract validation error")
	}
}

func TestDecisionCustomEffectDoesNotFailValidation(t *testing.T) {
	doc, err := Parse([]byte(`module "custom" {
  policy "demo" {
    default deny
    hold "manual-hold" when { request.amount > 10 } reason "custom effect"
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "demo", map[string]any{"request": map[string]any{"amount": int64(12)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "hold" {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionEngineWrapperAndScenarioExpectations(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl_decision_platform/fraud.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewDecisionEngine(prog, nil)
	result, err := engine.EvaluateScenario(&DecisionScenario{
		Name:     "high value vip requires review",
		Decision: "fraud-review",
		Input: map[string]any{
			"customer":    map[string]any{"blacklisted": false, "country": "US", "tier": "vip"},
			"transaction": map[string]any{"amount": int64(125000)},
		},
		Expect: map[string]any{
			"effect":    "require_review",
			"allowed":   false,
			"policy_id": "1001",
			"score":     int64(40),
			"action":    "notify",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("scenario failed: %#v", result.Diagnostics)
	}
}

func TestDecisionStrategies(t *testing.T) {
	rules := []DecisionRule{
		{ID: "deny", Effect: "deny", Order: 1},
		{ID: "allow", Effect: "allow", Order: 2},
		{ID: "review", Effect: "require_review", Order: 3},
	}
	cases := map[string][]string{
		"deny_overrides":   {"deny"},
		"allow_overrides":  {"allow"},
		"first_match":      {"deny"},
		"highest_priority": {"deny"},
		"all_must_pass":    {"deny"},
		"collect_all":      {"deny", "allow", "review"},
	}
	for strategy, want := range cases {
		gotRules := chooseDecisionPolicies(strategy, rules)
		if len(gotRules) != len(want) {
			t.Fatalf("%s selected %#v, want %#v", strategy, gotRules, want)
		}
		for i := range want {
			if gotRules[i].ID != want[i] {
				t.Fatalf("%s selected %#v, want %#v", strategy, gotRules, want)
			}
		}
	}
}

func TestDecisionValidationDiagnostics(t *testing.T) {
	doc, err := Parse([]byte(`module "validation" {
  decision_schema "demo" {
    effects [allow, deny]
    default deny
    strategy mystery
  }

  action "known" {}

  policy "demo" {
    allow "same" when { request.amount > 10 }
    deny "same" when { request.blocked == true }
  }

  rule_set "demo" {
    rule "notify-missing" {
      when { request.amount > 100 }
      then { action missing { team "risk" } }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileDecisionDocument(doc, &Options{})
	if err == nil {
		t.Fatal("expected validation diagnostics")
	}
	text := err.Error()
	for _, want := range []string{"invalid strategy", "duplicate rule", "unknown action"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q diagnostic in:\n%s", want, text)
		}
	}
}

func TestDecisionRuntimeInputValidation(t *testing.T) {
	doc, err := Parse([]byte(`module "runtime-validation" {
  schema demo {
    required request object {
      required amount number min 10
      required active bool
    }
  }

  policy "demo" {
    default deny
    allow "ok" when { request.amount >= 10 }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewDecisionEngine(prog, nil).EvaluateWithOptions("demo", map[string]any{
		"request": map[string]any{"amount": int64(5), "active": "yes"},
	}, DecisionEvaluateOptions{Explain: true, ValidateInput: true, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	text := FormatDiagnostics(result.Diagnostics)
	if !strings.Contains(text, "below minimum") || !strings.Contains(text, "should be bool") {
		t.Fatalf("missing runtime validation diagnostics: %#v", result.Diagnostics)
	}
	if result.Evaluated != 0 {
		t.Fatalf("strict validation should skip evaluation: %#v", result)
	}
}

func TestDecisionPatternMatchingInPolicyRuleSetAndRanking(t *testing.T) {
	doc, err := Parse([]byte(`module "pattern-decision" {
  decision_schema "route" {
    effects [allow, deny, require_review]
    default deny
    strategy allow_overrides
  }

  predicate "prime_loan" {
    match(request, case({kind: "loan", tags: ["prime", ...rest]} if request.amount > 100, true), false)
  }

  policy "route" {
    allow "prime-loan" when { predicate.prime_loan }
  }

  rule_set "route" {
    rule "typed-bind-score" {
      when { match(request, case({amount: amount:number}, true), false) }
      then { score += 7 }
    }
  }

  dataset "queues" {
    record "loan" {
      provider.type "queue"
      provider.skills ["loan", "prime"]
      provider.priority 9
    }
  }

  ranking "route" {
    dataset "queues"
    priority_path "provider.priority"
    rule "queue-shape" {
      when { match(provider, case({type: "queue", skills: ALL(skill:string)}, true), false) }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "route", map[string]any{
		"request": map[string]any{"kind": "loan", "amount": int64(150), "tags": []any{"prime", "np"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || result.Score != 7 || result.Rank == nil || result.Rank.ID != "loan" {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionExplainTraceDeterministic(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl/packages/support_priority.bcl", &Options{})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{
		"ticket":   map[string]any{"severity": "critical", "age_hours": int64(2), "topic": "database"},
		"customer": map[string]any{"plan": "enterprise"},
	}
	first, err := ExplainDecision(prog, "ticket-routing", input, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExplainDecision(prog, "ticket-routing", input, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first.Explain)
	b, _ := json.Marshal(second.Explain)
	if string(a) != string(b) {
		t.Fatalf("non-deterministic explain:\n%s\n%s", a, b)
	}
	if len(first.Explain) == 0 || first.Explain[0].ConditionResult == nil {
		t.Fatalf("missing condition explain: %#v", first.Explain)
	}
}
