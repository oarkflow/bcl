package condition

import (
	"fmt"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/storage"
)

func workflowDefinition(program *bcl.DecisionProgram, workflowID string) (*WorkflowDefinition, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	var workflows []map[string]any
	if raw := program.Governance["_condition_workflows"]; raw != nil {
		collectBlocks(raw, "workflow", &workflows)
	}
	if len(workflows) == 0 && program.Normalized != nil {
		collectBlocks(program.Normalized.Body, "workflow", &workflows)
	}
	if len(workflows) == 0 {
		return nil, fmt.Errorf("definition has no workflows")
	}
	for _, block := range workflows {
		id := stringAny(block["id"])
		if workflowID != "" && id != workflowID {
			continue
		}
		body := bodyMap(block["body"])
		def := &WorkflowDefinition{ID: id, Start: first(stringAny(body["start"]), stringAny(body["start_at"]))}
		for _, stage := range childBlocks(block["body"], "stage") {
			stageBody := bodyMap(stage["body"])
			ws := WorkflowStage{ID: stringAny(stage["id"]), SLA: stringAny(stageBody["sla"]), OnTimeout: stringAny(stageBody["on_timeout"])}
			ws.Assign = assignmentFromStage(stageBody)
			for _, rule := range childBlocks(stage["body"], "rule") {
				ruleBody := bodyMap(rule["body"])
				ws.Transitions = append(ws.Transitions, WorkflowTransition{
					ID:        stringAny(rule["id"]),
					NextStage: stringAny(ruleBody["next_stage"]),
					Events:    stringListAny(ruleBody["event"]),
				})
			}
			def.Stages = append(def.Stages, ws)
		}
		return def, nil
	}
	return nil, fmt.Errorf("unknown workflow %q", workflowID)
}

func bodyMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	out := map[string]any{}
	for _, item := range blockList(v) {
		if name := stringAny(item["name"]); name != "" {
			out[name] = literalValue(item["value"])
			continue
		}
		switch stringAny(item["type"]) {
		case "assignment":
			name := stringAny(item["name"])
			if name != "" {
				out[name] = literalValue(item["value"])
			}
		default:
			typ := stringAny(item["type"])
			if typ != "" {
				out[typ] = append(blockList(out[typ]), item)
			}
		}
	}
	return out
}

func childBlocks(v any, typ string) []map[string]any {
	var out []map[string]any
	for _, item := range blockList(v) {
		if stringAny(item["type"]) == typ {
			out = append(out, item)
		}
	}
	return out
}

func literalValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		if data, exists := m["data"]; exists {
			return data
		}
	}
	return v
}

func applyWorkflowStage(run *WorkflowRun, workflow *WorkflowDefinition, input map[string]any) {
	stage := findStage(workflow, run.Stage)
	if stage == nil {
		run.Status = "completed"
		return
	}
	run.Assignment = stage.Assign
	if stage.SLA != "" {
		if run.Assignment == nil {
			run.Assignment = map[string]any{}
		}
		run.Assignment["sla"] = stage.SLA
	}
	if stage.OnTimeout != "" {
		if run.Assignment == nil {
			run.Assignment = map[string]any{}
		}
		run.Assignment["on_timeout"] = stage.OnTimeout
	}
	_ = input
}

func advanceWorkflowStage(run *WorkflowRun, workflow *WorkflowDefinition, input map[string]any) {
	stage := findStage(workflow, run.Stage)
	if stage == nil || len(stage.Transitions) == 0 {
		run.Status = "completed"
		return
	}
	for _, transition := range stage.Transitions {
		run.Events = append(run.Events, transition.Events...)
		if transition.NextStage != "" {
			run.Stage = transition.NextStage
			applyWorkflowStage(run, workflow, input)
			return
		}
	}
	run.Status = "completed"
}

func workflowRunRecord(run WorkflowRun) storage.WorkflowRunRecord {
	return storage.WorkflowRunRecord{
		ID: run.ID, Definition: run.Definition, Version: run.Version, Environment: run.Environment, WorkflowID: run.WorkflowID,
		Stage: run.Stage, Status: run.Status, Input: run.Input, Assignment: run.Assignment, Events: run.Events, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}
}

func workflowRunFromRecord(record storage.WorkflowRunRecord) WorkflowRun {
	return WorkflowRun{
		ID: record.ID, Definition: record.Definition, Version: record.Version, Environment: record.Environment, WorkflowID: record.WorkflowID,
		Stage: record.Stage, Status: record.Status, Input: record.Input, Assignment: record.Assignment, Events: record.Events, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func firstWorkflowStage(workflow *WorkflowDefinition) string {
	if workflow != nil && len(workflow.Stages) > 0 {
		return workflow.Stages[0].ID
	}
	return ""
}

func findStage(workflow *WorkflowDefinition, id string) *WorkflowStage {
	if workflow == nil {
		return nil
	}
	for i := range workflow.Stages {
		if workflow.Stages[i].ID == id {
			return &workflow.Stages[i]
		}
	}
	return nil
}

func collectBlocks(v any, want string, out *[]map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		if stringAny(x["type"]) == want {
			*out = append(*out, x)
		}
		for _, child := range x {
			collectBlocks(child, want, out)
		}
	case []any:
		for _, child := range x {
			collectBlocks(child, want, out)
		}
	case []map[string]any:
		for _, child := range x {
			collectBlocks(child, want, out)
		}
	}
}

func blockList(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		return []map[string]any{x}
	case []map[string]any:
		return x
	default:
		return nil
	}
}

func assignmentFromStage(body map[string]any) map[string]any {
	assign := map[string]any{}
	for _, block := range blockList(body["assign"]) {
		if typ := stringAny(block["type"]); typ != "" {
			assign[typ] = block["value"]
		}
		if id := stringAny(block["id"]); id != "" {
			assign["target"] = id
		}
		if b, ok := block["body"].(map[string]any); ok {
			for k, v := range b {
				assign[k] = v
			}
		}
	}
	for _, key := range []string{"queue", "role", "user"} {
		if value := body[key]; value != nil {
			assign[key] = value
		}
	}
	if len(assign) == 0 {
		return nil
	}
	return assign
}

func stringAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func stringListAny(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, item := range x {
			if s := stringAny(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func initWorkflowRunDefaults(run *WorkflowRun) {
	now := time.Now()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
}
