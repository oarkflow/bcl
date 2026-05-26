package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type CustomerProfile struct {
	ID       string
	Age      int
	Segment  string
	Affinity map[string]int
}

type ProductCandidate struct {
	SKU             string
	Category        string
	MinimumAge      int
	RequiresLicense bool
	InventoryUnits  int
	MarginPercent   int
}

type RecommendationCase struct {
	Name       string
	Customer   CustomerProfile
	Candidates []ProductCandidate
}

type Recommendation struct {
	Product ProductCandidate
	Score   int
	Facts   map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "product-recommendation", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range recommendationCases() {
		rec := rankProducts(c)
		resp, err := svc.Evaluate(ctx, "product-recommendation", condition.EvaluateRequest{Decision: "product_recommendation", Input: rec.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Customer.ID, c.Name)
		fmt.Printf("  top_offer: sku=%s category=%s score=%d margin=%d%% inventory=%d\n", rec.Product.SKU, rec.Product.Category, rec.Score, rec.Product.MarginPercent, rec.Product.InventoryUnits)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		renderRecommendation(c, rec, decision.Effect)
	}
}

func recommendationCases() []RecommendationCase {
	candidates := []ProductCandidate{
		{SKU: "sku-analytics", Category: "software", MinimumAge: 18, InventoryUnits: 999, MarginPercent: 34},
		{SKU: "sku-card", Category: "regulated_finance", MinimumAge: 18, RequiresLicense: true, InventoryUnits: 100, MarginPercent: 16},
		{SKU: "sku-console", Category: "electronics", MinimumAge: 13, InventoryUnits: 3, MarginPercent: 9},
	}
	return []RecommendationCase{
		{Name: "b2b expansion offer", Customer: CustomerProfile{ID: "cus-1", Age: 42, Segment: "b2b", Affinity: map[string]int{"software": 80}}, Candidates: candidates},
		{Name: "regulated product suppressed", Customer: CustomerProfile{ID: "cus-2", Age: 31, Segment: "finance", Affinity: map[string]int{"regulated_finance": 95}}, Candidates: candidates},
		{Name: "young gamer with inventory risk", Customer: CustomerProfile{ID: "cus-3", Age: 16, Segment: "consumer", Affinity: map[string]int{"electronics": 90}}, Candidates: candidates},
	}
}

func rankProducts(c RecommendationCase) Recommendation {
	type scored struct {
		product ProductCandidate
		score   int
	}
	var scores []scored
	for _, p := range c.Candidates {
		score := c.Customer.Affinity[p.Category] + min(20, p.InventoryUnits/5) + p.MarginPercent/2
		scores = append(scores, scored{product: p, score: score})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	top := scores[0]
	eligible := c.Customer.Age >= top.product.MinimumAge && !top.product.RequiresLicense
	reviewRequired := top.product.MarginPercent < 12 || top.product.Category == "regulated_finance"
	showOffer := eligible && !reviewRequired && top.score >= 70 && top.product.InventoryUnits > 10
	return Recommendation{
		Product: top.product,
		Score:   top.score,
		Facts: map[string]any{
			"customer":       map[string]any{"id": c.Customer.ID, "age": c.Customer.Age, "segment": c.Customer.Segment},
			"product":        map[string]any{"sku": top.product.SKU, "category": top.product.Category, "minimum_age": top.product.MinimumAge, "requires_license": top.product.RequiresLicense, "inventory_units": top.product.InventoryUnits, "eligible": eligible},
			"recommendation": map[string]any{"score": top.score, "margin_percent": top.product.MarginPercent, "review_required": reviewRequired, "show_offer": showOffer},
		},
	}
}

func renderRecommendation(c RecommendationCase, rec Recommendation, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  render offer %s to customer %s\n", rec.Product.SKU, c.Customer.ID)
	case "require_review":
		fmt.Printf("  send placement to growth-ops; margin=%d%% score=%d\n", rec.Product.MarginPercent, rec.Score)
	default:
		fmt.Printf("  suppress offer %s and select next eligible candidate\n", rec.Product.SKU)
	}
}
