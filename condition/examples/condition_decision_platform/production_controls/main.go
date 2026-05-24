package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{
		Environment:               "example",
		StrictValidation:          true,
		StrictEvaluation:          true,
		RequireTests:              true,
		RequireActivationApproval: true,
	})

	path := filepath.Join("examples", "condition_decision_platform", "use_cases", "feature-gating", "decision.bcl")
	publish, err := svc.PublishVersion(ctx, condition.PublishRequest{
		Name:     "feature-gating",
		Version:  "1",
		Path:     path,
		RunTests: true,
	})
	must("publish version", publish, err)

	_, activateErr := svc.Activate(ctx, "feature-gating", "1", "example")
	printJSON("activation before approval", map[string]any{"blocked": activateErr != nil, "error": errorString(activateErr)})

	approval, err := svc.Approve(ctx, "feature-gating", "1", condition.ApprovalRequest{
		Environment: "example",
		ApprovedBy:  "product-ops",
		Reason:      "strict tests passed",
	})
	must("approve", approval, err)
	activation, err := svc.Activate(ctx, "feature-gating", "1", "example")
	must("activate", activation, err)

	input := map[string]any{
		"subject": map[string]any{"tenant_disabled": false, "plan": "enterprise"},
		"feature": map[string]any{"enabled": true, "beta": false},
	}
	evalResp, err := svc.Evaluate(ctx, "feature-gating", condition.EvaluateRequest{
		Decision:        "feature_gating",
		Input:           input,
		IncludeFeatures: true,
		Environment:     "example",
	})
	eval := must("evaluate", evalResp, err).(*condition.EvaluateResponse)
	printJSON("strict evaluation", eval.Report.Decision.Answer())

	_, strictErr := svc.Evaluate(ctx, "feature-gating", condition.EvaluateRequest{
		Decision:    "feature_gating",
		Input:       map[string]any{"subject": map[string]any{"plan": "enterprise"}},
		Environment: "example",
	})
	printJSON("strict invalid input", map[string]any{"blocked": strictErr != nil, "error": errorString(strictErr)})

	shadowResp, err := svc.Evaluate(ctx, "feature-gating", condition.EvaluateRequest{
		Decision:              "feature_gating",
		Input:                 input,
		Environment:           "example",
		ShadowCandidateSource: shadowCandidate(),
	})
	shadow := must("shadow evaluate", shadowResp, err).(*condition.EvaluateResponse)
	printJSON("shadow evaluation", map[string]any{
		"base_effect":        shadow.Report.Decision.Effect,
		"changed_cases":      shadow.Shadow.ChangedCases,
		"effect_transitions": shadow.Shadow.EffectTransitions,
	})

	disable, err := svc.Disable(ctx, "feature-gating", condition.DisableRequest{Environment: "example", Reason: "incident"})
	must("disable", disable, err)
	_, disabledErr := svc.Evaluate(ctx, "feature-gating", condition.EvaluateRequest{Decision: "feature_gating", Input: input, Environment: "example"})
	printJSON("disabled evaluation", map[string]any{"blocked": disabledErr != nil, "error": errorString(disabledErr)})

	enable, err := svc.Enable(ctx, "feature-gating", condition.DisableRequest{Environment: "example", Reason: "resolved"})
	must("enable", enable, err)
	printJSON("production readiness", svc.ProductionReadiness(ctx))
	mustNoValue("audit verify", svc.VerifyAudits(ctx))
}

func shadowCandidate() string {
	return `module "feature-gating-candidate" {
  decision_table "feature_gating" {
    default deny
    hit_policy first
    row "deny-enterprise" {
      when { subject.plan == "enterprise" }
      then { decision deny reason "candidate blocks enterprise" }
      reason "candidate blocks enterprise"
      reason_code "CANDIDATE_DENY"
    }
  }
}`
}

func must(label string, value any, err error) any {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", label, err))
	}
	return value
}

func mustNoValue(label string, err error) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", label, err))
	}
}

func printJSON(label string, value any) {
	payload, _ := json.MarshalIndent(value, "", "  ")
	fmt.Printf("\n== %s ==\n%s\n", label, payload)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
