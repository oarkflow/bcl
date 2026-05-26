package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Asset struct {
	ID          string
	Environment string
	Tier        string
}

type IncidentSignal struct {
	Name              string
	ErrorRatePercent  int
	SaturationPercent int
	CorrelatedAlerts  int
}

type Runbook struct {
	Name        string
	Destructive bool
	BlastRadius string
}

type RemediationCase struct {
	Name    string
	Asset   Asset
	Signal  IncidentSignal
	Runbook Runbook
}

type RemediationPlan struct {
	ConfidencePercent int
	Facts             map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "autonomous-remediation", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range remediationCases() {
		plan := buildPlan(c)
		resp, err := svc.Evaluate(ctx, "autonomous-remediation", condition.EvaluateRequest{Decision: "autonomous_remediation", Input: plan.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Asset.ID, c.Name)
		fmt.Printf("  runbook=%s confidence=%d%% destructive=%v blast=%s\n", c.Runbook.Name, plan.ConfidencePercent, c.Runbook.Destructive, c.Runbook.BlastRadius)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		executeRemediation(c, plan, decision.Effect)
	}
}

func remediationCases() []RemediationCase {
	return []RemediationCase{
		{Name: "restart saturated staging worker", Asset: Asset{ID: "svc-worker", Environment: "staging", Tier: "standard"}, Signal: IncidentSignal{Name: "queue_saturation", SaturationPercent: 96, CorrelatedAlerts: 4}, Runbook: Runbook{Name: "restart-worker", BlastRadius: "single-service"}},
		{Name: "uncertain production latency", Asset: Asset{ID: "svc-api", Environment: "production", Tier: "standard"}, Signal: IncidentSignal{Name: "latency", ErrorRatePercent: 3, SaturationPercent: 70, CorrelatedAlerts: 1}, Runbook: Runbook{Name: "scale-up", BlastRadius: "single-service"}},
		{Name: "regulated database rebuild", Asset: Asset{ID: "db-ledger", Environment: "production", Tier: "regulated"}, Signal: IncidentSignal{Name: "replica_corruption", ErrorRatePercent: 40, CorrelatedAlerts: 6}, Runbook: Runbook{Name: "rebuild-primary", Destructive: true, BlastRadius: "data-plane"}},
	}
}

func buildPlan(c RemediationCase) RemediationPlan {
	confidence := min(100, c.Signal.ErrorRatePercent+c.Signal.SaturationPercent+c.Signal.CorrelatedAlerts*8)
	return RemediationPlan{
		ConfidencePercent: confidence,
		Facts: map[string]any{
			"asset":       map[string]any{"id": c.Asset.ID, "environment": c.Asset.Environment, "tier": c.Asset.Tier},
			"signal":      map[string]any{"name": c.Signal.Name, "confidence_percent": confidence},
			"remediation": map[string]any{"name": c.Runbook.Name, "destructive": c.Runbook.Destructive, "blast_radius": c.Runbook.BlastRadius},
		},
	}
}

func executeRemediation(c RemediationCase, p RemediationPlan, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  execute runbook %s against %s and emit SRE audit event\n", c.Runbook.Name, c.Asset.ID)
	case "require_review":
		fmt.Printf("  page on-call for approval; confidence=%d%% environment=%s\n", p.ConfidencePercent, c.Asset.Environment)
	default:
		fmt.Printf("  block runbook %s and create incident for asset %s\n", c.Runbook.Name, c.Asset.ID)
	}
}
