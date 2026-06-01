package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Shipment struct {
	ID          string
	Service     string
	WeightKG    int
	Hazmat      bool
	Origin      string
	Destination string
}

type CarrierLane struct {
	ID                string
	Mode              string
	HazmatCertified   bool
	CapacityAvailable bool
	ExpressEnabled    bool
	CutoffHour        int
	OnTimePercent     float64
	CostCents         int
}

type DispatchCase struct {
	Name     string
	Shipment Shipment
	Carriers []CarrierLane
}

type CarrierChoice struct {
	Carrier CarrierLane
	Score   float64
	Reasons []string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "logistics-routing", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range dispatchBoard() {
		choice := selectCarrier(c)
		resp, err := svc.Evaluate(ctx, "logistics-routing", condition.EvaluateRequest{Decision: "logistics_routing", Input: routingDecisionFacts(c.Shipment, choice.Carrier)})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Shipment.ID, c.Name)
		fmt.Printf("  selected=%s score=%.1f reasons=%s\n", choice.Carrier.ID, choice.Score, strings.Join(choice.Reasons, ", "))
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		commitDispatch(c.Shipment, choice, decision.Effect)
	}
}

func dispatchBoard() []DispatchCase {
	return []DispatchCase{
		{Name: "express parcel", Shipment: Shipment{ID: "ship-1", Service: "express", WeightKG: 20, Origin: "SFO", Destination: "LAX"}, Carriers: carriers()},
		{Name: "hazmat chemical sample", Shipment: Shipment{ID: "ship-2", Service: "standard", WeightKG: 45, Hazmat: true, Origin: "HOU", Destination: "DAL"}, Carriers: carriers()},
		{Name: "heavy freight", Shipment: Shipment{ID: "ship-3", Service: "standard", WeightKG: 1400, Origin: "ORD", Destination: "ATL"}, Carriers: carriers()},
	}
}

func carriers() []CarrierLane {
	return []CarrierLane{
		{ID: "air-1", Mode: "air", CapacityAvailable: true, ExpressEnabled: true, CutoffHour: 18, OnTimePercent: 0.98, CostCents: 4200},
		{ID: "ground-hazmat", Mode: "ground", HazmatCertified: true, CapacityAvailable: true, CutoffHour: 16, OnTimePercent: 0.93, CostCents: 1800},
		{ID: "ltl-9", Mode: "ltl", HazmatCertified: true, CapacityAvailable: true, CutoffHour: 15, OnTimePercent: 0.90, CostCents: 1200},
	}
}

func selectCarrier(c DispatchCase) CarrierChoice {
	var choices []CarrierChoice
	for _, carrier := range c.Carriers {
		score := carrier.OnTimePercent * 100
		var reasons []string
		if c.Shipment.Service == "express" && carrier.ExpressEnabled {
			score += 25
			reasons = append(reasons, "express-capable")
		}
		if c.Shipment.Hazmat && carrier.HazmatCertified {
			score += 30
			reasons = append(reasons, "hazmat-certified")
		}
		if c.Shipment.WeightKG > 1000 && carrier.Mode == "ltl" {
			score += 20
			reasons = append(reasons, "freight-fit")
		}
		score -= float64(carrier.CostCents) / 1000
		if !carrier.CapacityAvailable {
			score -= 100
			reasons = append(reasons, "no-capacity")
		}
		if len(reasons) == 0 {
			reasons = append(reasons, "baseline-quality")
		}
		choices = append(choices, CarrierChoice{Carrier: carrier, Score: score, Reasons: reasons})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Score > choices[j].Score })
	return choices[0]
}

func routingDecisionFacts(shipment Shipment, carrier CarrierLane) map[string]any {
	return map[string]any{
		"shipment": map[string]any{
			"id":          shipment.ID,
			"service":     shipment.Service,
			"weight_kg":   shipment.WeightKG,
			"hazmat":      shipment.Hazmat,
			"origin":      shipment.Origin,
			"destination": shipment.Destination,
		},
		"carrier": map[string]any{
			"id":                 carrier.ID,
			"hazmat_certified":   carrier.HazmatCertified,
			"capacity_available": carrier.CapacityAvailable,
			"express_enabled":    carrier.ExpressEnabled,
		},
	}
}

func commitDispatch(shipment Shipment, choice CarrierChoice, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  tender load to %s and reserve dock appointment\n", choice.Carrier.ID)
	case "require_review":
		fmt.Printf("  create planner task for %s with carrier shortlist\n", shipment.ID)
	default:
		fmt.Printf("  remove %s from shortlist and restart carrier search\n", choice.Carrier.ID)
	}
}
