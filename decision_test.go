package bcl

import (
	"path/filepath"
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
