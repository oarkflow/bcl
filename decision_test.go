package bcl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestDecisionSchemaBlockLegacyTypePathClauses(t *testing.T) {
	doc, err := Parse([]byte(`module "legacy" {
  schema "transaction_review" {
    required customer.id
    required transaction.amount
    type customer.id string
    type transaction.amount number
  }
  decision_schema "transaction_review" { effects [allow, deny] default deny strategy first_match }
  decision "transaction_review" {
    rule "allow-valid" {
      when { transaction.amount > 0 }
      then { decision "allow" }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := prog.Schemas["transaction_review"].(map[string]any)["fields"].([]map[string]any)
	if len(fields) != 2 {
		t.Fatalf("expected merged decision schema fields, got %#v", fields)
	}
	for _, field := range fields {
		switch field["name"] {
		case "customer.id":
			if field["type"] != "string" || field["required"] != true {
				t.Fatalf("bad customer.id field: %#v", field)
			}
		case "transaction.amount":
			if field["type"] != "number" || field["required"] != true {
				t.Fatalf("bad transaction.amount field: %#v", field)
			}
		default:
			t.Fatalf("unexpected field: %#v", field)
		}
	}
	engine := NewDecisionEngine(prog, nil)
	_, err = engine.EvaluateWithOptions("transaction_review", map[string]any{
		"customer":    map[string]any{"id": "cust_123"},
		"transaction": map[string]any{"amount": 10},
	}, DecisionEvaluateOptions{ValidateInput: true, Strict: true})
	if err != nil {
		t.Fatalf("valid input should pass schema validation: %v", err)
	}
	result, err := engine.EvaluateWithOptions("transaction_review", map[string]any{
		"customer":    map[string]any{"id": 123},
		"transaction": map[string]any{},
	}, DecisionEvaluateOptions{ValidateInput: true, Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	text := FormatDiagnostics(result.Diagnostics)
	if !strings.Contains(text, "customer.id") || !strings.Contains(text, "transaction.amount") {
		t.Fatalf("expected path schema validation diagnostics, got %#v", result.Diagnostics)
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

func TestDecisionDatasetFileAdapters(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "batch.jsonl"), `{"id":"high","input":{"request":{"amount":20}}}
{"id":"low","input":{"request":{"amount":1}}}
`)
	mustWrite(t, filepath.Join(dir, "batch.csv"), "id,amount\nhigh,20\nlow,1\n")
	mustWrite(t, filepath.Join(dir, "batch.json"), `[{"id":"high","input":{"request":{"amount":20}}},{"id":"low","input":{"request":{"amount":1}}}]`)
	doc, err := Parse([]byte(`module "adapter-test" {
  decision_table "demo" {
    default deny
    hit_policy first
    row "allow-high" { when { request.amount > 10 } then { decision allow } }
  }
  decision_table "csv_demo" {
    default deny
    hit_policy first
    row "allow-high" { when { amount > 10 } then { decision allow } }
  }
  dataset "jsonl_batch" { source { adapter file path "./batch.jsonl" format jsonl facts_path "input" } }
  dataset "json_batch" { source { adapter file path "./batch.json" format json facts_path "input" } }
  dataset "csv_batch" { source { adapter file path "./batch.csv" format csv id_path "id" } }
  gate "demo_gate" { decision "demo" dataset "jsonl_batch" min_pass_rate 1.0 }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		decision string
		dataset  string
	}{
		{"demo", "jsonl_batch"},
		{"demo", "json_batch"},
		{"csv_demo", "csv_batch"},
	} {
		report, err := EvaluateDecisionDataset(prog, tc.decision, tc.dataset, &Options{BaseDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if report.EffectCounts["allow"] != 1 || report.EffectCounts["deny"] != 1 {
			t.Fatalf("%s report = %#v", tc.dataset, report.EffectCounts)
		}
	}
	gates, err := EvaluateDecisionGates(prog, "", &Options{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !gates.Passed {
		t.Fatalf("gate failed: %#v", gates)
	}
	compare, err := CompareDecisionDataset(prog, prog, "demo", "jsonl_batch", &Options{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(compare.ChangedCases) != 0 {
		t.Fatalf("compare changed: %#v", compare.ChangedCases)
	}
}

func TestDecisionDatasetHTTPAdapter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test") != "yes" {
			http.Error(w, "missing header", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"records":[{"id":"high","input":{"request":{"amount":20}}},{"id":"low","input":{"request":{"amount":1}}}]}`)
	}))
	defer server.Close()
	doc, err := Parse([]byte(fmt.Sprintf(`module "http-adapter-test" {
  decision_table "demo" {
    default deny
    hit_policy first
    row "allow-high" { when { request.amount > 10 } then { decision allow } }
  }
  dataset "http_batch" {
    source {
      adapter http
      url "%s"
      format json
      response_path "records"
      facts_path "input"
      headers { "X-Test" "yes" }
    }
  }
}`, server.URL)))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateDecisionDataset(prog, "demo", "http_batch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.EffectCounts["allow"] != 1 || report.EffectCounts["deny"] != 1 {
		t.Fatalf("http report = %#v", report.EffectCounts)
	}
}

func TestDecisionDatasetCustomAdapterAndRanking(t *testing.T) {
	doc, err := Parse([]byte(`module "custom-adapter-test" {
  decision_table "route" {
    default deny
    hit_policy first
    row "allow" { when { request.ready == true } then { decision allow } }
  }
  ranking "route" {
    dataset "providers"
    priority_path "provider.priority"
    rule "active" { when { provider.active == true } }
  }
  dataset "providers" { source { adapter db table "providers" } }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter := DecisionDatasetAdapterFunc(func(ctx context.Context, source DatasetSource, opts *Options) (DecisionRecordIterator, error) {
		return &sliceDecisionIterator{records: []DecisionCandidate{
			{ID: "slow", Facts: map[string]any{"provider": map[string]any{"active": true, "priority": int64(1)}}},
			{ID: "fast", Facts: map[string]any{"provider": map[string]any{"active": true, "priority": int64(10)}}},
		}}, nil
	})
	result, err := EvaluateDecision(prog, "route", map[string]any{"request": map[string]any{"ready": true}}, &Options{DecisionDatasetAdapters: map[string]DecisionDatasetAdapter{"db": adapter}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || result.Rank == nil || result.Rank.ID != "fast" {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionEvaluateExternalFunction(t *testing.T) {
	doc, err := Parse([]byte(`module "external-function-test" {
  decision_table "screen" {
    default deny
    hit_policy first
    row "allow-trusted" { when { external_trust_score(customer.id) >= 80 } then { decision allow } }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "screen", map[string]any{
		"customer": map[string]any{"id": "cust-123"},
	}, &Options{EvalFunctions: map[string]EvalFunction{
		"external_trust_score": func(args []any, opts *EvalOptions) (any, error) {
			if len(args) != 1 || args[0] != "cust-123" {
				return nil, fmt.Errorf("bad args: %#v", args)
			}
			return 91, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || !result.Allowed {
		t.Fatalf("decision = %#v", result)
	}
}

func TestDecisionRegisteredExternalFunctionInRankingCondition(t *testing.T) {
	RegisterDecisionFunction("registered_provider_available_for_test", func(args []any, opts *EvalOptions) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("registered_provider_available_for_test requires 1 argument")
		}
		return args[0] == "fast", nil
	})
	doc, err := Parse([]byte(`module "registered-external-function-test" {
  decision_table "route" {
    default deny
    hit_policy first
    row "allow" { when { request.ready == true } then { decision allow } }
  }
  ranking "route" {
    selection highest_score
    priority_path "provider.priority"
    rule "available" { when { registered_provider_available_for_test(provider.id) } }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "route", map[string]any{
		"request": map[string]any{"ready": true},
		"candidates": []any{
			map[string]any{"id": "slow", "facts": map[string]any{"provider": map[string]any{"id": "slow", "priority": int64(100)}}},
			map[string]any{"id": "fast", "facts": map[string]any{"provider": map[string]any{"id": "fast", "priority": int64(10)}}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rank == nil || result.Rank.ID != "fast" {
		t.Fatalf("rank = %#v", result.Rank)
	}
}

func TestDecisionPlatformReport(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl_decision_platform/use_cases/iam-access/decision.bcl", &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := EvaluateDecisionPlatform(prog, DecisionPlatformRequest{
		Decision:        "iam_access",
		Bundle:          "iam_access_bundle",
		IncludeGates:    true,
		Counterfactuals: true,
		IncludeFeatures: true,
		Input: map[string]any{
			"subject":  map[string]any{"blocked": false, "role": "analyst", "mfa": true},
			"resource": map[string]any{"sensitivity": "normal", "required_role": "analyst"},
			"request":  map[string]any{"action": "read"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.Decision == nil || report.Decision.Effect != "allow" {
		t.Fatalf("platform decision = %#v", report.Decision)
	}
	if report.Observation.DecisionID != "iam_access" || len(report.Observation.SelectedRules) == 0 {
		t.Fatalf("platform observation = %#v", report.Observation)
	}
	if report.Gates == nil || !report.Gates.Passed {
		t.Fatalf("platform gates = %#v", report.Gates)
	}
	if report.Features.DecisionCount == 0 || report.Features.ExternalDatasets == 0 || !stringIn("external_dataset_adapters", report.Features.Capabilities) {
		t.Fatalf("platform features = %#v", report.Features)
	}
	if source := report.DatasetSources["iam_access_batch"]; source.Adapter != "file" {
		t.Fatalf("dataset sources = %#v", report.DatasetSources)
	}
}

func TestDecisionDatasetRejectsMixedSourceAndRecords(t *testing.T) {
	doc, err := Parse([]byte(`module "bad-adapter-test" {
  decision_table "demo" { row "allow" { then { decision allow } } }
  dataset "bad" {
    source { adapter file path "./batch.jsonl" }
    record "inline" { request.amount 1 }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = CompileDecisionDocument(doc, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot mix source and inline records") {
		t.Fatalf("expected mixed dataset validation error, got %v", err)
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

func TestDecisionCompositionReasonCodesTagsHitPolicyCoverageAndCompare(t *testing.T) {
	baseDoc, err := Parse([]byte(`module "governance" {
  reason_code_catalog "main" {
    code "APPROVED" { description "approved request" }
    code "REVIEW" { description "manual review" }
  }

  decision_schema "kyc" {
    effects [allow, deny]
    default deny
    strategy first_match
  }

  decision_table "kyc" {
    default deny
    row "kyc-ok" {
      when { customer.verified == true }
      then { decision allow }
      reason_code "APPROVED"
      tags ["kyc"]
    }
  }

  decision_schema "main" {
    effects [allow, deny, require_review]
    default require_review
    strategy first_match
  }

  decision_table "main" {
    default require_review
    hit_policy unique
    row "allow-composed" {
      priority 10
      when { match(decision("kyc"), case({effect: "allow"}, true), false) }
      then { decision allow }
      reason_code "APPROVED"
      tags ["composition", "low-risk"]
    }
    row "review-large" {
      priority 10
      when { request.amount > 100 }
      then { decision require_review }
      reason_code "REVIEW"
      tags ["manual"]
    }
    row "unused" {
      priority 1
      when { request.amount > 1000 }
      then { decision deny }
      reason_code "REVIEW"
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	base, err := CompileDecisionDocument(baseDoc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(base, "main", map[string]any{
		"customer": map[string]any{"verified": true},
		"request":  map[string]any{"amount": int64(20)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || result.ReasonCode != "APPROVED" || !stringIn("composition", result.Tags) || len(result.ExplainGraph) == 0 {
		t.Fatalf("composed result = %#v", result)
	}
	multi, err := EvaluateDecision(base, "main", map[string]any{
		"customer": map[string]any{"verified": true},
		"request":  map[string]any{"amount": int64(200)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decisionDiagnosticsContain(multi.Diagnostics, "unique hit policy") {
		t.Fatalf("missing unique hit diagnostic: %#v", multi.Diagnostics)
	}
	cases := []DecisionBatchCase{
		{ID: "allow", Input: map[string]any{"customer": map[string]any{"verified": true}, "request": map[string]any{"amount": int64(20)}}, Expect: map[string]any{"effect": "allow", "reason_code": "APPROVED", "tags": []any{"composition", "low-risk"}, "selected_rules": []any{"allow-composed"}}},
		{ID: "review", Input: map[string]any{"customer": map[string]any{"verified": false}, "request": map[string]any{"amount": int64(200)}}, Expect: map[string]any{"effect": "require_review", "matched_rules": []any{"review-large"}}},
	}
	report, err := EvaluateDecisionBatch(base, "main", cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.RuleHitCounts["allow-composed"] == 0 || report.RuleHitCounts["review-large"] == 0 || !stringIn("unused", report.UnhitRules) || report.FailedCount != 0 {
		t.Fatalf("batch report = %#v", report)
	}

	candidateDoc, err := Parse([]byte(`module "governance-candidate" {
  decision_schema "kyc" {
    effects [allow, deny]
    default deny
    strategy first_match
  }
  decision_table "kyc" {
    default deny
    row "kyc-ok" { when { customer.verified == true } then { decision allow } }
  }
  decision_schema "main" {
    effects [allow, deny, require_review]
    default require_review
    strategy first_match
  }
  decision_table "main" {
    default require_review
    row "allow-composed" {
      when { match(decision("kyc"), case({effect: "allow"}, true), false) }
      then { decision require_review }
    }
    row "review-large" {
      when { request.amount > 100 }
      then { decision require_review }
    }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := CompileDecisionDocument(candidateDoc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	compare, err := CompareDecisionBatch(base, candidate, "main", cases, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(compare.ChangedCases) != 1 || compare.EffectTransitions["allow->require_review"] != 1 {
		t.Fatalf("compare report = %#v", compare)
	}
}

func TestDecisionCompositionRecursionAndReasonCodeCatalogWarning(t *testing.T) {
	doc, err := Parse([]byte(`module "recursive" {
  reason_code_catalog "loop" {
    code "KNOWN" {}
  }
  decision_table "loop" {
    default deny
    row "self" {
      when { match(decision("loop"), case({effect: "allow"}, true), false) }
      then { decision allow }
      reason_code "UNKNOWN"
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
	if !decisionDiagnosticsContain(prog.Diagnostics, "unknown reason code") {
		t.Fatalf("missing reason code warning: %#v", prog.Diagnostics)
	}
	result, err := EvaluateDecision(prog, "loop", map[string]any{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decisionDiagnosticsContain(result.Diagnostics, "recursive decision call") {
		t.Fatalf("missing recursion diagnostic: %#v", result.Diagnostics)
	}
}

func TestDecisionOperationsBundlesGatesParamsTemplatesCounterfactualObservation(t *testing.T) {
	doc, err := Parse([]byte(`module "ops" {
  reason_code_catalog "ops" {
    code "ALLOW" {}
    code "REVIEW" {}
  }

  rule_template "review_template" {
    row "template-review" {
      priority 5
      when { request.amount >= param.limit }
      then { decision require_review }
      reason_code "REVIEW"
      tags ["template"]
    }
  }

  decision_schema "ops" {
    effects [allow, require_review]
    default require_review
    strategy first_match
  }

  decision_table "ops" {
    default require_review
    hit_policy first
    param limit number { default 100 }
    approval {
      status approved
      approved_by "risk@example.com"
      approved_at "2026-05-20T00:00:00Z"
    }

    row "allow-low" {
      priority 20
      when { request.amount < param.limit }
      then { decision allow }
      reason_code "ALLOW"
      tags ["low"]
    }

    use rule_template "review_template" {
      id "review-high"
      priority 10
    }
  }

  dataset "ops_cases" {
    record "low" { request.amount 50 }
    record "high" { request.amount 150 }
  }

  gate "ops_gate" {
    bundle "ops_bundle"
    decision "ops"
    dataset "ops_cases"
    min_pass_rate 1.0
    max_diagnostics 10
    no_default_only true
    required_rules ["allow-low", "review_template.review-high"]
  }

  decision_bundle "ops_bundle" {
    decisions ["ops"]
    datasets ["ops_cases"]
    release "ops_release"
    approval { status approved approved_by "governance@example.com" approved_at "2026-05-20T00:00:00Z" }
  }

  decision_release "ops_release" {
    bundle "ops_bundle"
    version "2026.05"
    stage production
    approval { status approved approved_by "release@example.com" approved_at "2026-05-20T00:00:00Z" }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if bundle, err := CompileDecisionBundle(prog, "ops_bundle", nil); err != nil || bundle.Release != "ops_release" {
		t.Fatalf("bundle = %#v err=%v", bundle, err)
	}
	if release, err := CompileDecisionRelease(prog, "ops_release", nil); err != nil || release.Stage != "production" {
		t.Fatalf("release = %#v err=%v", release, err)
	}
	low, err := EvaluateDecision(prog, "ops", map[string]any{"request": map[string]any{"amount": int64(50)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if low.Effect != "allow" || low.ReasonCode != "ALLOW" || !stringIn("low", low.Tags) {
		t.Fatalf("low = %#v", low)
	}
	override, err := EvaluateDecision(prog, "ops", map[string]any{
		"params":  map[string]any{"limit": int64(200)},
		"request": map[string]any{"amount": int64(150)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if override.Effect != "allow" {
		t.Fatalf("param override = %#v", override)
	}
	gates, err := EvaluateDecisionGates(prog, "ops_bundle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !gates.Passed || len(gates.Results) != 1 {
		t.Fatalf("gates = %#v", gates)
	}
	cf, err := CounterfactualDecision(prog, "ops", map[string]any{"request": map[string]any{"amount": int64(150)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cf.Counterfactuals) == 0 || cf.Counterfactuals[0].Path != "request.amount" {
		t.Fatalf("counterfactuals = %#v", cf.Counterfactuals)
	}
	obs := DecisionResultObservation(low, map[string]any{"request": map[string]any{"amount": int64(50)}}, nil)
	if obs.DecisionID != "ops" || obs.Effect != "allow" || obs.InputHash == "" || len(obs.SelectedRules) == 0 {
		t.Fatalf("observation = %#v", obs)
	}
}

func TestDecisionOperationsRequiredParamAndReleaseApprovalWarnings(t *testing.T) {
	doc, err := Parse([]byte(`module "ops-warnings" {
  decision_table "needs_param" {
    default deny
    param threshold number { required true }
    row "allow" {
      when { request.amount >= param.threshold }
      then { decision allow }
    }
  }
  decision_bundle "draft_bundle" {
    decisions ["needs_param"]
    approval { status draft }
  }
  decision_release "draft_release" {
    bundle "draft_bundle"
    stage production
    approval { status draft }
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	prog, err := CompileDecisionDocument(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !decisionDiagnosticsContain(prog.Diagnostics, "not approved") && !decisionDiagnosticsContain(prog.Diagnostics, "unapproved") {
		t.Fatalf("missing approval warning: %#v", prog.Diagnostics)
	}
	result, err := EvaluateDecision(prog, "needs_param", map[string]any{"request": map[string]any{"amount": int64(10)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !decisionDiagnosticsContain(result.Diagnostics, "param") {
		t.Fatalf("missing param diagnostic: %#v", result.Diagnostics)
	}
}

func decisionDiagnosticsContain(diags []Diagnostic, text string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, text) {
			return true
		}
	}
	return false
}
