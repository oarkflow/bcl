package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Shopper struct {
	ID          string
	LoyaltyTier string
	Segment     string
}

type CatalogItem struct {
	SKU            string
	ListPriceCents int
	UnitCostCents  int
}

type DemandSnapshot struct {
	ViewsLastHour        int
	OrdersLastHour       int
	CompetitorPriceCents int
	InventoryUnits       int
}

type PricingCase struct {
	Name     string
	Shopper  Shopper
	Item     CatalogItem
	Snapshot DemandSnapshot
	At       time.Time
}

type PriceRecommendation struct {
	MarginPercent int
	DemandIndex   int
	DaysOfSupply  int
	PriceCents    int
	Rationale     []string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "dynamic-pricing", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range pricingFeed() {
		rec := recommendPrice(c)
		resp, err := svc.Evaluate(ctx, "dynamic-pricing", condition.EvaluateRequest{Decision: "dynamic_pricing", Input: pricingDecisionFacts(c, rec)})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Item.SKU, c.Name)
		fmt.Printf("  recommendation: price=%s margin=%d%% demand=%d supply=%dd rationale=%v\n", money(rec.PriceCents), rec.MarginPercent, rec.DemandIndex, rec.DaysOfSupply, rec.Rationale)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["price_action"], decision.ReasonCode)
		applyPrice(c, rec, decision.Effect)
	}
}

func pricingFeed() []PricingCase {
	now := time.Date(2026, 5, 24, 13, 0, 0, 0, time.UTC)
	return []PricingCase{
		{Name: "platinum shopper discount", Shopper: Shopper{ID: "cus-1", LoyaltyTier: "platinum", Segment: "retention"}, Item: CatalogItem{SKU: "sku-100", ListPriceCents: 12900, UnitCostCents: 9300}, Snapshot: DemandSnapshot{ViewsLastHour: 80, OrdersLastHour: 6, CompetitorPriceCents: 12400, InventoryUnits: 400}, At: now},
		{Name: "surge with low inventory", Shopper: Shopper{ID: "cus-2", LoyaltyTier: "gold", Segment: "standard"}, Item: CatalogItem{SKU: "sku-200", ListPriceCents: 8900, UnitCostCents: 6500}, Snapshot: DemandSnapshot{ViewsLastHour: 900, OrdersLastHour: 90, CompetitorPriceCents: 9800, InventoryUnits: 15}, At: now},
		{Name: "margin floor breach", Shopper: Shopper{ID: "cus-3", LoyaltyTier: "platinum", Segment: "retention"}, Item: CatalogItem{SKU: "sku-300", ListPriceCents: 5400, UnitCostCents: 5000}, Snapshot: DemandSnapshot{ViewsLastHour: 40, OrdersLastHour: 2, CompetitorPriceCents: 5200, InventoryUnits: 800}, At: now},
	}
}

func recommendPrice(c PricingCase) PriceRecommendation {
	price := c.Item.ListPriceCents
	var rationale []string
	if c.Shopper.LoyaltyTier == "platinum" {
		price = int(float64(price) * 0.90)
		rationale = append(rationale, "loyalty-10-percent")
	}
	if c.Snapshot.CompetitorPriceCents > c.Item.ListPriceCents {
		price += int(float64(c.Item.ListPriceCents) * 0.04)
		rationale = append(rationale, "competitor-above-list")
	}
	demand := demandIndex(c.Snapshot)
	daysSupply := daysOfSupply(c.Snapshot)
	margin := marginPercent(price, c.Item.UnitCostCents)
	return PriceRecommendation{MarginPercent: margin, DemandIndex: demand, DaysOfSupply: daysSupply, PriceCents: price, Rationale: rationale}
}

func pricingDecisionFacts(c PricingCase, rec PriceRecommendation) map[string]any {
	return map[string]any{
		"customer": map[string]any{"id": c.Shopper.ID, "loyalty_tier": c.Shopper.LoyaltyTier, "segment": c.Shopper.Segment},
		"offer": map[string]any{
			"sku":              c.Item.SKU,
			"list_price_cents": c.Item.ListPriceCents,
			"margin_percent":   rec.MarginPercent,
		},
		"inventory": map[string]any{"days_of_supply": rec.DaysOfSupply, "units": c.Snapshot.InventoryUnits},
		"market":    map[string]any{"demand_index": rec.DemandIndex},
	}
}

func applyPrice(c PricingCase, rec PriceRecommendation, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  publish personalized price %s for shopper %s\n", money(rec.PriceCents), c.Shopper.ID)
	case "require_review":
		fmt.Printf("  pause price card and ask revenue manager to review demand spike\n")
	default:
		fmt.Printf("  suppress discount, margin would be %d%%\n", rec.MarginPercent)
	}
}

func demandIndex(s DemandSnapshot) int {
	return int(math.Min(100, float64(s.ViewsLastHour)/10+float64(s.OrdersLastHour)))
}

func daysOfSupply(s DemandSnapshot) int {
	if s.OrdersLastHour == 0 {
		return 99
	}
	return int(math.Ceil(float64(s.InventoryUnits) / float64(s.OrdersLastHour*24)))
}

func marginPercent(price, cost int) int {
	return int(math.Round((float64(price-cost) / float64(price)) * 100))
}

func money(cents int) string {
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}
