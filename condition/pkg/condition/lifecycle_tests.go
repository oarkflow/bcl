package condition

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/storage"
)

type lifecycleScenario struct {
	Name      string
	Lifecycle string
	Phase     string
	Method    string
	Path      string
	Request   map[string]any
	Input     map[string]any
	Response  map[string]any
	Event     string
	EntityKey string
	Expect    map[string]any
}

func lifecycleScenarios(program *bcl.DecisionProgram) ([]lifecycleScenario, []bcl.Diagnostic) {
	var blocks []map[string]any
	if program != nil && program.Governance["_condition_lifecycle_tests"] != nil {
		collectBlocks(program.Governance["_condition_lifecycle_tests"], "lifecycle_test", &blocks)
	} else if program != nil && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "lifecycle_test", &blocks)
	}
	var out []lifecycleScenario
	var diags []bcl.Diagnostic
	for _, block := range blocks {
		body := bodyMap(block["body"])
		scenario := lifecycleScenario{
			Name:      strings.TrimSpace(stringAny(block["id"])),
			Lifecycle: first(stringAny(body["lifecycle"]), stringAny(body["lifecycle_id"])),
			Phase:     stringAny(body["phase"]),
			Method:    stringAny(body["method"]),
			Path:      stringAny(body["path"]),
			Event:     stringAny(body["event"]),
			EntityKey: stringAny(body["entity_key"]),
		}
		for _, child := range childBlocks(block["body"], "input") {
			scenario.Input = lifecycleObjectMap(child["body"])
		}
		for _, child := range childBlocks(block["body"], "request") {
			scenario.Request = lifecycleObjectMap(child["body"])
		}
		for _, child := range childBlocks(block["body"], "response") {
			scenario.Response = lifecycleObjectMap(child["body"])
		}
		for _, child := range childBlocks(block["body"], "expect") {
			scenario.Expect = lifecycleObjectMap(child["body"])
		}
		if scenario.Name == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "lifecycle_test is missing id"})
			continue
		}
		if scenario.Lifecycle == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle_test %q is missing lifecycle", scenario.Name)})
			continue
		}
		if scenario.Phase == "" {
			diags = append(diags, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("lifecycle_test %q is missing phase", scenario.Name)})
			continue
		}
		out = append(out, scenario)
	}
	return out, diags
}

func (s *Service) runLifecycleScenarios(program *bcl.DecisionProgram, definition, version, environment string) []LifecycleScenarioResult {
	scenarios, diags := lifecycleScenarios(program)
	var results []LifecycleScenarioResult
	for _, diag := range diags {
		results = append(results, LifecycleScenarioResult{Passed: false, Diagnostics: []bcl.Diagnostic{diag}})
	}
	if len(scenarios) == 0 {
		return results
	}
	store := storage.NewMemoryStore()
	record := storage.DefinitionRecord{
		TenantID:    s.cfg.DefaultTenant,
		Name:        first(definition, "lifecycle-test"),
		Version:     first(version, "test"),
		Environment: first(environment, s.cfg.Environment),
		Program:     program,
		PublishedAt: s.now(),
	}
	if err := store.SaveDefinitionVersion(context.Background(), record); err != nil {
		results = append(results, LifecycleScenarioResult{Passed: false, Diagnostics: []bcl.Diagnostic{{Severity: "error", Message: err.Error()}}})
		return results
	}
	if err := store.ActivateDefinition(context.Background(), record.Name, record.Version, record.Environment); err != nil {
		results = append(results, LifecycleScenarioResult{Passed: false, Diagnostics: []bcl.Diagnostic{{Severity: "error", Message: err.Error()}}})
		return results
	}
	runner := NewService(store, s.cfg)
	runner.cfg.Clock = func() time.Time { return s.now() }
	for _, scenario := range scenarios {
		result := LifecycleScenarioResult{Name: scenario.Name, Lifecycle: scenario.Lifecycle, Phase: scenario.Phase, Passed: true, Expected: scenario.Expect}
		resp, err := runner.EvaluateLifecycle(context.Background(), record.Name, scenario.Lifecycle, LifecycleEvaluateRequest{
			Phase: scenario.Phase, Method: scenario.Method, Path: scenario.Path, Request: scenario.Request, Input: scenario.Input, Response: scenario.Response, Event: scenario.Event, EntityKey: scenario.EntityKey, DryRun: true,
		})
		if err != nil {
			result.Passed = false
			result.Diagnostics = append(result.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
			results = append(results, result)
			continue
		}
		result.Actual = map[string]any{
			"final_action": resp.Evaluation.FinalAction,
			"final_effect": resp.Evaluation.FinalEffect,
			"final_reason": resp.Evaluation.FinalReason,
			"route":        resp.Evaluation.Route.ID,
		}
		for key, want := range scenario.Expect {
			if got := fmt.Sprint(result.Actual[key]); got != fmt.Sprint(want) {
				result.Passed = false
				result.Diagnostics = append(result.Diagnostics, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("expected %s=%v got %v", key, want, got)})
			}
		}
		results = append(results, result)
	}
	return results
}

func lifecycleObjectMap(v any) map[string]any {
	out := map[string]any{}
	for _, item := range blockList(v) {
		name := stringAny(item["name"])
		if name != "" {
			setPathValue(out, name, literalValue(item["value"]))
			continue
		}
		typ := stringAny(item["type"])
		if typ == "" || typ == "assignment" {
			continue
		}
		child := lifecycleObjectMap(item["body"])
		if id := strings.TrimSpace(stringAny(item["id"])); id != "" {
			child["id"] = id
		}
		setPathValue(out, typ, child)
	}
	if len(out) > 0 {
		return out
	}
	return metadataMap(v)
}
