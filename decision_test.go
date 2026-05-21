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

func TestDecisionTableCompilesRowsIntoDecisionRules(t *testing.T) {
	doc, err := Parse([]byte(`module "table-decision" {
  decision_schema "loan" {
    effects [allow, deny, require_review]
    default deny
    strategy first_match
  }

  decision_table "loan" {
    strategy first_match

    row "prime" {
      priority 20
      when { applicant.credit_score >= 700 }
      then { decision allow }
      reason "prime applicant"
    }

    row "review" {
      priority 10
      when { applicant.credit_score >= 620 }
      then { decision require_review }
      reason "borderline credit"
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
	result, err := EvaluateDecision(prog, "loan", map[string]any{
		"applicant": map[string]any{"credit_score": int64(650)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "require_review" || result.PolicyID != "review" || result.Reason != "borderline credit" {
		t.Fatalf("decision table result = %#v", result)
	}
	if len(result.Explain) == 0 || result.Explain[len(result.Explain)-1].Source != "decision_table" {
		t.Fatalf("missing decision_table explain source: %#v", result.Explain)
	}
}

func TestDecisionConservativeWarningsDoNotFailCompilation(t *testing.T) {
	doc, err := Parse([]byte(`module "diagnostic-decision" {
  decision_table "demo" {
    strategy first_match

    row "catch-all" {
      then { decision allow }
    }

    row "unreachable" {
      when { request.amount > 100 }
      then { decision deny }
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
	text := FormatDiagnostics(prog.Diagnostics)
	for _, want := range []string{"no explicit default", "unreachable after catch-all"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q warning in:\n%s", want, text)
		}
	}
}

func TestDecisionTableDuplicateRowsAndConflictDiagnostics(t *testing.T) {
	doc, err := Parse([]byte(`module "diagnostic-decision" {
  decision_table "demo" {
    default deny

    row "same" {
      when { request.amount > 100 }
      then { decision allow }
    }

    row "same" {
      when { request.amount > 100 }
      then { decision deny }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileDecisionDocument(doc, &Options{})
	if err == nil {
		t.Fatal("expected duplicate row validation error")
	}
	text := err.Error()
	for _, want := range []string{"duplicate rule", "conflicting effects"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q diagnostic in:\n%s", want, text)
		}
	}
}

func TestDecisionOutcomeAttributesPhaseAndWhyNot(t *testing.T) {
	doc, err := Parse([]byte(`module "outcome-decision" {
  decision_schema "demo" {
    effects [allow, deny, require_review]
    default deny
    strategy first_match
  }

  decision_table "demo" {
    strategy first_match

    row "requires-flag" {
      phase guard
      priority 30
      when { request.required == true }
      then { decision deny }
    }

    row "amount-is-100" {
      phase guard
      priority 20
      when { request.amount == 100 }
      then { decision deny }
    }

    row "score-first" {
      phase score
      priority 1
      when { request.amount > 1000 }
      then { score += 5 }
      reason "adds score"
    }

    row "review" {
      phase decide
      priority 10
      when { request.amount >= 5000 }
      then {
        outcome {
          decision require_review
          reason "large request"
          attributes { band "large" }
          metadata { queue "risk" }
        }
      }
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
	result, err := EvaluateDecision(prog, "demo", map[string]any{"request": map[string]any{"amount": int64(6000)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "require_review" || result.Outcome == nil || result.Outcome.Attributes["band"] != "large" || result.Metadata["queue"] != "risk" {
		t.Fatalf("outcome result = %#v", result)
	}
	if result.Score != 5 {
		t.Fatalf("phase score should run before decide: %#v", result)
	}
	sawScorePhase := false
	sawWhyNot := false
	for _, step := range result.Explain {
		if step.RuleID == "score-first" && step.Phase == "score" {
			sawScorePhase = true
		}
		if step.RuleID == "review" && step.Status == "matched" && step.Phase == "decide" {
			// covered below by result selection; keep phase assertion explicit
		}
	}
	denied, err := EvaluateDecision(prog, "demo", map[string]any{"request": map[string]any{"amount": int64(100)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range denied.Explain {
		if step.Status == "skipped" && strings.Contains(step.Message, "actual=100") && strings.Contains(step.Message, "expected=1000") {
			sawWhyNot = true
		}
	}
	if !sawScorePhase || !sawWhyNot {
		t.Fatalf("missing phase/why-not trace: score=%v why=%v explain=%#v denied=%#v", sawScorePhase, sawWhyNot, result.Explain, denied.Explain)
	}
	missing, err := EvaluateDecision(prog, "demo", map[string]any{"request": map[string]any{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := EvaluateDecision(prog, "demo", map[string]any{"request": map[string]any{"amount": "small"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decisionExplainContains(missing.Explain, "actual=<nil>") || !decisionExplainContains(typed.Explain, "actual=small") {
		t.Fatalf("missing why-not details for missing/type cases: missing=%#v typed=%#v", missing.Explain, typed.Explain)
	}
}

func decisionExplainContains(trace []DecisionTrace, text string) bool {
	for _, step := range trace {
		if strings.Contains(step.Message, text) {
			return true
		}
	}
	return false
}

func TestDecisionScenarioOutcomeAttributeExpectations(t *testing.T) {
	doc, err := Parse([]byte(`module "outcome-scenario" {
  decision_table "demo" {
    default deny
    row "review" {
      when { request.amount > 100 }
      then {
        outcome {
          decision require_review
          attributes { band "high" }
        }
      }
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
	result, err := EvaluateDecisionScenario(prog, &DecisionScenario{
		Name:     "high amount",
		Decision: "demo",
		Input:    map[string]any{"request": map[string]any{"amount": int64(150)}},
		Expect:   map[string]any{"effect": "require_review", "attributes.band": "high", "outcome.attributes.band": "high"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Fatalf("scenario failed: %#v", result.Diagnostics)
	}
}

func TestDecisionTestMatrixRunsCases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.bcl")
	mustWrite(t, path, `module "matrix" {
  decision_table "demo" {
    default deny
    row "review" {
      when { request.amount > 100 }
      then {
        outcome {
          decision require_review
          attributes { band "high" }
        }
      }
    }
  }

  test_matrix "amount bands" {
    decision "demo"

    case "high" {
      input { request.amount 150 }
      expect {
        effect "require_review"
        attributes.band "high"
      }
    }

    case "low" {
      input { request.amount 25 }
      expect { effect "deny" }
    }
  }
}`)
	suite, err := TestFile(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !suite.Passed || len(suite.Tests) != 2 {
		t.Fatalf("matrix suite = %#v", suite)
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

func TestDecisionRuleMetadataEffectiveWindowObligationsAdviceAndBatch(t *testing.T) {
	doc, err := Parse([]byte(`module "metadata-decision" {
  decision_schema "demo" {
    effects [allow, deny, require_review]
    default deny
    strategy first_match
  }

  decision_table "demo" {
    row "inactive" {
      priority 30
      status inactive
      when { request.amount > 10 }
      then { decision deny }
    }

    row "future" {
      priority 20
      effective_from "2030-01-01T00:00:00Z"
      when { request.amount > 10 }
      then { decision deny }
    }

    row "allow-active" {
      priority 10
      version "v1"
      owner "risk"
      rationale "active rule"
      effective_from "2020-01-01"
      effective_until "2028-01-01"
      when { request.amount > 10 }
      then {
        outcome { decision allow }
        obligation mfa { level "step-up" }
        advice appeal { url "/appeal" }
      }
    }
  }

  dataset "batch" {
    record "high" { request.amount 20 time.now "2026-05-20T00:00:00Z" }
    record "low" { request.amount 1 time.now "2026-05-20T00:00:00Z" }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "demo", map[string]any{
		"request": map[string]any{"amount": int64(20)},
		"time":    map[string]any{"now": "2026-05-20T00:00:00Z"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || result.PolicyID != "allow-active" || len(result.Obligations) != 1 || len(result.Advice) != 1 {
		t.Fatalf("decision = %#v", result)
	}
	if result.Outcome == nil || len(result.Outcome.Obligations) != 1 || len(result.Outcome.Advice) != 1 {
		t.Fatalf("outcome obligations/advice = %#v", result.Outcome)
	}
	if !decisionExplainContains(result.Explain, "not yet effective") {
		t.Fatalf("missing effective-window explain: %#v", result.Explain)
	}
	report, err := EvaluateDecisionDataset(prog, "demo", "batch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.EffectCounts["allow"] != 1 || report.EffectCounts["deny"] != 1 || report.DefaultOnlyCount != 1 || report.RuleHitCounts["allow-active"] == 0 {
		t.Fatalf("batch report = %#v", report)
	}
}

func TestDecisionRuleAnalysisAndSchemaPatternWarnings(t *testing.T) {
	doc, err := Parse([]byte(`module "analysis-decision" {
  schema demo {
    required request object {
      required kind string enum ["loan", "card"]
      required amount number
      optional channel string
    }
  }

  decision_schema "demo" {
    effects [allow, deny]
    default deny
    strategy first_match
  }

  decision_table "demo" {
    row "loan" {
      priority 10
      when { request.kind == "loan" }
      then { decision allow }
    }
    row "large-allow" {
      priority 5
      when { request.amount >= 100 }
      then { decision allow }
    }
    row "large-deny" {
      priority 5
      when { request.amount > 50 }
      then { decision deny }
    }
    row "bad-pattern" {
      priority 1
      when { match(request, case({kind: "mortgage", unknown: true, amount: amount:string, channel: MISSING}, true), false) }
      then { decision deny }
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
	text := FormatDiagnostics(prog.Diagnostics)
	for _, want := range []string{"ambiguous same-priority", "overlapping numeric rules", "does not cover all enum values", "unknown schema field", "outside schema enum", "expects string but schema is number"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q diagnostic in:\n%s", want, text)
		}
	}
}
