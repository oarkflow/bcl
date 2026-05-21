package bcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenizeFileExposesEditorTokens(t *testing.T) {
	toks, diags := TokenizeFile("test.bcl", []byte(`const LIMIT = 10
policy "allow-admin" {
  effect allow
}
`))
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(toks) == 0 {
		t.Fatal("expected tokens")
	}
	var sawKeyword, sawString, sawNumber, sawProperty bool
	for _, tok := range toks {
		switch tok.Type {
		case "keyword":
			sawKeyword = true
		case "string":
			sawString = true
		case "number":
			sawNumber = true
		case "property":
			sawProperty = true
		}
		if tok.Span.Start.Line == 0 || tok.Span.Start.Column == 0 {
			t.Fatalf("token has invalid 1-based span: %+v", tok)
		}
	}
	if !sawKeyword || !sawString || !sawNumber || !sawProperty {
		t.Fatalf("missing expected token classes: keyword=%v string=%v number=%v property=%v", sawKeyword, sawString, sawNumber, sawProperty)
	}
}

func TestAnalyzeFileIndexesSymbolsReferencesAndCompletions(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

const LIMIT = 10

schema policy {
  required effect string enum ["allow", "deny"]
}

set "admin-roles" {
  admin
}

rule "enable_feature" {
  effect allow
}

policy "base" {
  effect allow
}

policy "allow-admin" {
  effect allow
  max LIMIT
  roles set("admin-roles")
  parent policy.base
  rule_ref rule.enable_feature
}
`)
	a, diags := AnalyzeFile("test.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if _, ok := a.Constants["LIMIT"]; !ok {
		t.Fatal("expected LIMIT constant")
	}
	if _, ok := a.Schemas["policy"]; !ok {
		t.Fatal("expected policy schema")
	}
	if _, ok := a.Sets["admin-roles"]; !ok {
		t.Fatal("expected admin-roles set")
	}
	var sawLimitRef, sawSetRef, sawPolicyRef, sawRuleRef bool
	for _, ref := range a.References {
		if ref.Name == "LIMIT" {
			sawLimitRef = true
		}
		if ref.Name == "admin-roles" {
			sawSetRef = true
		}
		if ref.Name == "policy.base" {
			sawPolicyRef = true
		}
		if ref.Name == "rule.enable_feature" {
			sawRuleRef = true
		}
	}
	if !sawLimitRef || !sawSetRef || !sawPolicyRef || !sawRuleRef {
		t.Fatalf("missing references: LIMIT=%v set=%v policy=%v rule=%v refs=%+v", sawLimitRef, sawSetRef, sawPolicyRef, sawRuleRef, a.References)
	}
	var sawEffectCompletion bool
	for _, c := range a.Completions {
		if c.Label == "effect" && c.Kind == "field" {
			sawEffectCompletion = true
			break
		}
	}
	if !sawEffectCompletion {
		t.Fatalf("expected schema field completion, got %+v", a.Completions)
	}
}

func TestAnalyzeFileAllowsExternalRuntimeReferences(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

headers {
  "X-Tenant-ID" subject.tenant
  "X-Request-ID" request.id
  generated_at time.now
  decision decision.effect
}
`)
	_, diags := AnalyzeFile("runtime-refs.bcl", src, nil)
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown reference") {
			t.Fatalf("runtime reference should not be reported as unknown: %#v", diags)
		}
	}
}

func TestRuntimeReferenceHoverUsesHintCatalog(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

audit {
  time time.now
  decision decision.effect
}
`)
	a, diags := AnalyzeFile("runtime-hover.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := SymbolAt(a, 6, 8)
	if !ok {
		t.Fatal("expected symbol at time.now")
	}
	if sym.Kind != SymbolRuntime || sym.Name != "time.now" {
		t.Fatalf("expected runtime symbol for time.now, got %+v", sym)
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{"runtime value", "Current evaluation timestamp", "host application supplies"} {
		if !strings.Contains(hover, want) {
			t.Fatalf("runtime hover missing %q:\n%s", want, hover)
		}
	}
}

func TestBuiltinFunctionHoverUsesHintCatalog(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

database_url sensitive(env.required("DATABASE_URL"))
`)
	a, diags := AnalyzeFile("function-hover.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := SymbolAt(a, 5, 14)
	if !ok {
		t.Fatal("expected symbol at sensitive(...)")
	}
	if sym.Kind != SymbolFunction || sym.Name != "sensitive" {
		t.Fatalf("expected function symbol for sensitive, got %+v", sym)
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{"function", "Marks a value for redaction", "Signature"} {
		if !strings.Contains(hover, want) {
			t.Fatalf("function hover missing %q:\n%s", want, hover)
		}
	}
}

func TestDefaultCompletionsIncludeDetailedRuntimeAndFunctionHints(t *testing.T) {
	comps := DefaultCompletions()
	want := map[string]string{
		"time.now":         "Current evaluation timestamp",
		"decision.effect":  "Final decision effect",
		"env.required":     "missing",
		"context.required": "required context value",
	}
	for label, doc := range want {
		var found Completion
		for _, c := range comps {
			if c.Label == label {
				found = c
				break
			}
		}
		if found.Label == "" || !strings.Contains(found.Documentation, doc) {
			t.Fatalf("completion %q missing documentation %q: %+v", label, doc, found)
		}
	}
}

func TestCompletionsAtIncludesDecisionValuesAndWorkspaceSymbols(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

decision_table "fraud_aml" {
  hit_policy first
  row "aml-review" {
    phase decide
  }
}
`)
	a, diags := AnalyzeFile("decision-completion.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	comps, ctx := CompletionsAt(a, src, 6, len("  hit_policy ")+1)
	if ctx.AssignmentName != "hit_policy" {
		t.Fatalf("unexpected completion context: %+v", ctx)
	}
	if !completionLabelsContain(comps, "first", "priority", "collect", "unique", "decision_table") {
		t.Fatalf("missing decision completions: %+v", comps)
	}
	comps, ctx = CompletionsAt(a, src, 8, len("    phase ")+1)
	if ctx.AssignmentName != "phase" || !completionLabelsContain(comps, "validate", "guard", "score", "decide", "notify") {
		t.Fatalf("missing phase completions ctx=%+v comps=%+v", ctx, comps)
	}
}

func completionLabelsContain(comps []Completion, labels ...string) bool {
	seen := map[string]bool{}
	for _, c := range comps {
		seen[c.Label] = true
	}
	for _, label := range labels {
		if !seen[label] {
			return false
		}
	}
	return true
}

func TestAnalyzeFileReportsUnknownReferenceOnlyForKnownBlockTypes(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

policy "base" {
  parent policy.missing
  runtime decision.effect
}
`)
	_, diags := AnalyzeFile("known-block-ref.bcl", src, nil)
	var unknownPolicy, unknownDecision int
	for _, d := range diags {
		if strings.Contains(d.Message, `unknown reference "policy.missing"`) {
			unknownPolicy++
		}
		if strings.Contains(d.Message, `unknown reference "decision.effect"`) {
			unknownDecision++
		}
	}
	if unknownPolicy != 1 || unknownDecision != 0 {
		t.Fatalf("unexpected unknown reference diagnostics: policy=%d decision=%d diags=%#v", unknownPolicy, unknownDecision, diags)
	}
}

func TestAnalyzeFileCountsImportedConstantUsedInExpression(t *testing.T) {
	dir := t.TempDir()
	commonPath := filepath.Join(dir, "common.bcl")
	mainPath := filepath.Join(dir, "main.bcl")
	if err := os.WriteFile(commonPath, []byte(`bcl {
  version "1.0"
}

const ADMIN_ROLES = ["admin", "superadmin"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	src := []byte(`import "./common.bcl"

policy "document-owner-or-admin" {
  when {
    subject.roles has_any ADMIN_ROLES
  }
}
`)
	if err := os.WriteFile(mainPath, src, 0o644); err != nil {
		t.Fatal(err)
	}

	_, diags := AnalyzeFile(mainPath, src, &Options{ResolveImports: true, BaseDir: dir})
	for _, d := range diags {
		if strings.Contains(d.Message, `unused constant "ADMIN_ROLES"`) {
			t.Fatalf("ADMIN_ROLES is used inside expression text: %#v", diags)
		}
	}
}

func TestLintSuppressesUnusedDeclarationsForPartialFiles(t *testing.T) {
	doc, err := ParseFile("common.bcl", []byte(`const ADMIN_ROLES = ["admin"]

set "admin-roles" {
  admin
}
`))
	if err != nil {
		t.Fatal(err)
	}

	diags := Lint(doc, &Options{Partial: true})
	for _, d := range diags {
		if strings.Contains(d.Message, "unused constant") || strings.Contains(d.Message, "unused set") {
			t.Fatalf("partial shared file should not report unused declarations: %#v", diags)
		}
	}
}

func TestSymbolAtResolvesReferenceToDeclaration(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

const LIMIT = 10
policy "p" {
  max LIMIT
}
`)
	a, diags := AnalyzeFile("test.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := SymbolAt(a, 7, 8)
	if !ok {
		t.Fatal("expected symbol at LIMIT reference")
	}
	if sym.Name != "LIMIT" || sym.Kind != SymbolConst {
		t.Fatalf("expected LIMIT const declaration, got %+v", sym)
	}
}

func TestSymbolAtResolvesSetAndGenericBlockReferences(t *testing.T) {
	src := genericReferenceHoverFixture()
	a, diags := AnalyzeFile("refs.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for target, want := range map[string]string{
		"admin-roles":         "set.admin-roles",
		"policy.base":         "policy.base",
		"rule.enable_feature": "rule.enable_feature",
	} {
		var refSpan Span
		for _, ref := range a.References {
			if ref.Name == target {
				refSpan = ref.Span
				break
			}
		}
		if refSpan.Start.Line == 0 {
			t.Fatalf("expected %s reference, got %+v", target, a.References)
		}
		sym, ok := SymbolAt(a, refSpan.Start.Line, refSpan.Start.Column)
		if !ok {
			t.Fatalf("expected symbol at %s reference", target)
		}
		if sym.Name != want {
			t.Fatalf("expected %s declaration for %s, got %+v", want, target, sym)
		}
	}
}

func TestRichHoverMarkdownIncludesEvaluatorSectionsAndCommands(t *testing.T) {
	src := []byte(`bcl {
  version "1.0"
}

pipeline "feature-rollout" {
  entrypoint "plan"

  step "plan" {
    kind task
  }
}
`)
	a, diags := AnalyzeFile("test.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["pipeline.feature-rollout"]
	if !ok {
		t.Fatal("expected pipeline symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{"Live structure", "Workflow steps: `plan`", "What it does", "How BCL evaluates it", "Request / input parameters", "Output / result", "command:bcl.compileCurrentFile"} {
		if !strings.Contains(hover, want) {
			t.Fatalf("rich hover missing %q:\n%s", want, hover)
		}
	}
}

func TestRichConnectionHoverIncludesSpecificParametersAndGraphEffect(t *testing.T) {
	src := workflowHoverFixture()
	a, diags := AnalyzeFile("workflow.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["connection.risk-to-approve"]
	if !ok {
		t.Fatal("expected connection symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{
		"Connects `step.risk-check` to `step.approve` when transition event is `unmatched`",
		"| `from` | `step.risk-check` | Source step in pipeline `feature-rollout`; `decision` step that evaluates conditions and chooses a matched or unmatched transition; when `any condition`. |",
		"| `to` | `step.approve` | Target step in pipeline `feature-rollout`; `action` step that performs or records an action requested by the workflow definition; then `action = enable_feature`. |",
		"| `on` | `unmatched`",
		"Source step `risk-check`: exists",
		"Target step `approve`: exists",
		"Source step:",
		"`risk-check`: evaluates conditions and chooses a matched or unmatched transition.",
		"Target step:",
		"`approve`: performs or records an action requested by the workflow definition.",
		"Then: `action = enable_feature`",
		"risk-check -> approve",
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("connection hover missing %q:\n%s", want, hover)
		}
	}
}

func TestRichConnectionHoverParameterMeaningsExplainSourceAndTargetSteps(t *testing.T) {
	src := workflowHoverFixture()
	a, diags := AnalyzeFile("workflow.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["connection.plan-to-risk"]
	if !ok {
		t.Fatal("expected connection symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{
		"| `from` | `step.plan` | Source step in pipeline `feature-rollout`; `task` step that runs or records a unit of workflow work before moving to the next transition; then `emit = feature.rollout.planned`, `log = Feature rollout planned`; fields `service = planning`. |",
		"| `to` | `step.risk-check` | Target step in pipeline `feature-rollout`; `decision` step that evaluates conditions and chooses a matched or unmatched transition; when `any condition`. |",
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("connection hover missing %q:\n%s", want, hover)
		}
	}
}

func TestWorkflowReferenceHoverAndSymbolResolutionUseRelatedStep(t *testing.T) {
	src := workflowHoverFixture()
	a, diags := AnalyzeFile("workflow.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	var refSpan Span
	for _, ref := range a.References {
		if ref.Name == "step.plan" {
			refSpan = ref.Span
			break
		}
	}
	if refSpan.Start.Line == 0 {
		t.Fatalf("expected step.plan reference, got %+v", a.References)
	}
	sym, ok := SymbolAt(a, refSpan.Start.Line, refSpan.Start.Column)
	if !ok {
		t.Fatal("expected symbol at step.plan reference")
	}
	if sym.Name != "step.plan" || sym.Detail != "step" {
		t.Fatalf("expected step.plan declaration, got %+v", sym)
	}

	conn, ok := a.Declarations["connection.plan-to-risk"]
	if !ok {
		t.Fatal("expected connection symbol")
	}
	var from LanguageSymbol
	for _, child := range conn.Children {
		if child.Name == "from" {
			from = child
			break
		}
	}
	if from.Name == "" {
		t.Fatal("expected from assignment")
	}
	hover := RichHoverMarkdown(a, from, src)
	for _, want := range []string{
		"Referenced items",
		"`step.plan`: Referenced step in pipeline `feature-rollout`; `task` step that runs or records a unit of workflow work before moving to the next transition",
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("assignment hover missing %q:\n%s", want, hover)
		}
	}
}

func TestRichPipelineHoverListsEntrypointStepsAndConnections(t *testing.T) {
	src := workflowHoverFixture()
	a, diags := AnalyzeFile("workflow.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["pipeline.feature-rollout"]
	if !ok {
		t.Fatal("expected pipeline symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{"Entrypoint: `plan`", "`plan`", "`risk-check`", "`approve`", "`risk-to-approve`: `step.risk-check -> step.approve` on `unmatched`"} {
		if !strings.Contains(hover, want) {
			t.Fatalf("pipeline hover missing %q:\n%s", want, hover)
		}
	}
}

func TestRichStepHoverListsIncomingOutgoingAndThenActions(t *testing.T) {
	src := workflowHoverFixture()
	a, diags := AnalyzeFile("workflow.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["step.approve"]
	if !ok {
		t.Fatal("expected step symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{"Step `approve`", "Incoming: `risk-to-approve`", "Then blocks/actions", "`action = enable_feature`"} {
		if !strings.Contains(hover, want) {
			t.Fatalf("step hover missing %q:\n%s", want, hover)
		}
	}
}

func TestRichGenericBlockHoverListsFieldsChildrenAndReferences(t *testing.T) {
	src := genericReferenceHoverFixture()
	a, diags := AnalyzeFile("generic.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["policy.allow-admin"]
	if !ok {
		t.Fatal("expected policy symbol")
	}
	hover := RichHoverMarkdown(a, sym, src)
	for _, want := range []string{
		"Realtime fields",
		"`effect` | `allow`",
		"`max` | `LIMIT`",
		"Child blocks",
		"`metadata` object",
		"References used here",
		"`LIMIT`: constant declaration; value `10`; value kind `int`.",
		"`admin-roles`: set block consumed by `set(...)`",
		"`policy.base`: `policy` block; fields `effect = allow`.",
		"`rule.enable_feature`: `rule` block; fields `effect = allow`.",
		"How BCL evaluates it",
	} {
		if !strings.Contains(hover, want) {
			t.Fatalf("generic hover missing %q:\n%s", want, hover)
		}
	}
}

func TestAssignmentHoverListsGenericReferencedItems(t *testing.T) {
	src := genericReferenceHoverFixture()
	a, diags := AnalyzeFile("generic.bcl", src, nil)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	sym, ok := a.Declarations["policy.allow-admin"]
	if !ok {
		t.Fatal("expected policy symbol")
	}
	assigns := blockChildAssignments(sym)
	for name, wants := range map[string][]string{
		"max": {
			"Referenced items",
			"`LIMIT`: constant declaration; value `10`; value kind `int`.",
		},
		"roles": {
			"Referenced items",
			"`admin-roles`: set block consumed by `set(...)`",
		},
		"parent": {
			"Referenced items",
			"`policy.base`: `policy` block; fields `effect = allow`.",
		},
		"rule_ref": {
			"Referenced items",
			"`rule.enable_feature`: `rule` block; fields `effect = allow`.",
		},
	} {
		hover := RichHoverMarkdown(a, assigns[name], src)
		for _, want := range wants {
			if !strings.Contains(hover, want) {
				t.Fatalf("%s assignment hover missing %q:\n%s", name, want, hover)
			}
		}
	}
}

func genericReferenceHoverFixture() []byte {
	return []byte(`bcl {
  version "1.0"
}

const LIMIT = 10

set "admin-roles" {
  admin
}

rule "enable_feature" {
  effect allow
}

policy "base" {
  effect allow
}

policy "allow-admin" {
  effect allow
  max LIMIT
  roles set("admin-roles")
  parent policy.base
  rule_ref rule.enable_feature
  metadata {
    owner "security"
  }
}
`)
}

func workflowHoverFixture() []byte {
	return []byte(`bcl {
  version "1.0"
}

pipeline "feature-rollout" {
  entrypoint "plan"

  step "plan" {
    kind task
    service "planning"

    then {
      emit "feature.rollout.planned"
      log "Feature rollout planned"
    }
  }

  step "risk-check" {
    kind decision

    when {
      any {
        feature.risk_score > 80
        feature.owner empty
      }
    }
  }

  step "approve" {
    kind action
    then {
      action "enable_feature"
    }
  }

  connection "plan-to-risk" {
    from step.plan
    to step.risk-check
    on success
  }

  connection "risk-to-approve" {
    from step.risk-check
    to step.approve
    on unmatched
  }
}
`)
}
