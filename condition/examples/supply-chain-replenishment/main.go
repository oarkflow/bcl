package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type SKUState struct {
	SKU         string
	OnHand      int
	OnOrder     int
	DailyDemand []int
	SafetyStock int
}

type VendorScorecard struct {
	ID           string
	Active       bool
	QualityScore int
	LeadTimeDays int
	MOQ          int
}

type ReplenishmentCase struct {
	Name   string
	SKU    SKUState
	Vendor VendorScorecard
}

type ReplenishmentPlan struct {
	StockoutDays int
	DemandSpike  bool
	OrderQty     int
	Facts        map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "supply-chain-replenishment", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range planningQueue() {
		plan := buildPlan(c)
		resp, err := svc.Evaluate(ctx, "supply-chain-replenishment", condition.EvaluateRequest{Decision: "replenishment", Input: plan.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.SKU.SKU, c.Name)
		fmt.Printf("  plan: stockout=%dd qty=%d demand_spike=%v vendor_quality=%d\n", plan.StockoutDays, plan.OrderQty, plan.DemandSpike, c.Vendor.QualityScore)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		executePlan(c, plan, decision.Effect)
	}
}

func planningQueue() []ReplenishmentCase {
	return []ReplenishmentCase{
		{Name: "stable reorder", SKU: SKUState{SKU: "sku-1", OnHand: 900, OnOrder: 100, DailyDemand: []int{45, 48, 50, 46, 47}, SafetyStock: 200}, Vendor: VendorScorecard{ID: "ven-1", Active: true, QualityScore: 92, LeadTimeDays: 9, MOQ: 500}},
		{Name: "demand spike risk", SKU: SKUState{SKU: "sku-2", OnHand: 260, DailyDemand: []int{40, 55, 120, 135, 150}, SafetyStock: 150}, Vendor: VendorScorecard{ID: "ven-2", Active: true, QualityScore: 84, LeadTimeDays: 18, MOQ: 400}},
		{Name: "bad supplier", SKU: SKUState{SKU: "sku-3", OnHand: 600, DailyDemand: []int{20, 22, 21, 20, 23}, SafetyStock: 100}, Vendor: VendorScorecard{ID: "ven-3", Active: true, QualityScore: 48, LeadTimeDays: 12, MOQ: 300}},
	}
}

func buildPlan(c ReplenishmentCase) ReplenishmentPlan {
	avg := average(c.SKU.DailyDemand)
	stockout := int(math.Floor(float64(c.SKU.OnHand+c.SKU.OnOrder-c.SKU.SafetyStock) / math.Max(1, avg)))
	spike := demandSpike(c.SKU.DailyDemand)
	qty := int(math.Max(float64(c.Vendor.MOQ), avg*float64(c.Vendor.LeadTimeDays+14)))
	return ReplenishmentPlan{
		StockoutDays: stockout,
		DemandSpike:  spike,
		OrderQty:     qty,
		Facts: map[string]any{
			"sku":      map[string]any{"id": c.SKU.SKU, "stockout_days": stockout},
			"vendor":   map[string]any{"id": c.Vendor.ID, "active": c.Vendor.Active, "quality_score": c.Vendor.QualityScore, "lead_time_days": c.Vendor.LeadTimeDays},
			"forecast": map[string]any{"demand_spike": spike},
		},
	}
}

func executePlan(c ReplenishmentCase, p ReplenishmentPlan, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  create purchase order for %d units to vendor %s\n", p.OrderQty, c.Vendor.ID)
	case "require_review":
		fmt.Printf("  planner to split order or expedite; stockout in %d days\n", p.StockoutDays)
	default:
		fmt.Printf("  block vendor %s and search alternate suppliers\n", c.Vendor.ID)
	}
}

func average(values []int) float64 {
	sum := 0
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func demandSpike(values []int) bool {
	if len(values) < 3 {
		return false
	}
	return values[len(values)-1] > values[0]*2
}
