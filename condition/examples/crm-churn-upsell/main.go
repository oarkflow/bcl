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

type AccountMetrics struct {
	ID                string
	ARRUSD            int
	NPS               int
	SupportTickets    int
	SeatsPurchased    int
	SeatsActive       int
	CoreFeaturesUsed  int
	CoreFeaturesTotal int
	WeeklyActiveTrend int
}

type CRMCase struct {
	Name    string
	Account AccountMetrics
}

type CustomerHealth struct {
	ChurnScore      int
	ProductHealth   int
	SeatUtilization int
	FeatureAdoption int
	Facts           map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "crm-churn-upsell", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range accountBook() {
		health := computeHealth(c.Account)
		resp, err := svc.Evaluate(ctx, "crm-churn-upsell", condition.EvaluateRequest{Decision: "customer_next_action", Input: health.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Account.ID, c.Name)
		fmt.Printf("  health: churn=%d product=%d seats=%d%% adoption=%d%%\n", health.ChurnScore, health.ProductHealth, health.SeatUtilization, health.FeatureAdoption)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		routeAccount(c.Account, health, decision.Effect)
	}
}

func accountBook() []CRMCase {
	return []CRMCase{
		{Name: "expansion candidate", Account: AccountMetrics{ID: "acct-1", ARRUSD: 50000, NPS: 9, SupportTickets: 1, SeatsPurchased: 100, SeatsActive: 92, CoreFeaturesUsed: 8, CoreFeaturesTotal: 10, WeeklyActiveTrend: 12}},
		{Name: "enterprise needs QBR", Account: AccountMetrics{ID: "acct-2", ARRUSD: 180000, NPS: 7, SupportTickets: 3, SeatsPurchased: 200, SeatsActive: 130, CoreFeaturesUsed: 5, CoreFeaturesTotal: 10, WeeklyActiveTrend: -4}},
		{Name: "churn save account", Account: AccountMetrics{ID: "acct-3", ARRUSD: 30000, NPS: 3, SupportTickets: 8, SeatsPurchased: 80, SeatsActive: 20, CoreFeaturesUsed: 2, CoreFeaturesTotal: 10, WeeklyActiveTrend: -30}},
	}
}

func computeHealth(a AccountMetrics) CustomerHealth {
	seat := percent(a.SeatsActive, a.SeatsPurchased)
	adoption := percent(a.CoreFeaturesUsed, a.CoreFeaturesTotal)
	product := clamp((seat+adoption+a.NPS*10)/3+a.WeeklyActiveTrend/2, 0, 100)
	churn := clamp(100-product+a.SupportTickets*4+(10-a.NPS)*5, 0, 100)
	return CustomerHealth{
		ChurnScore:      churn,
		ProductHealth:   product,
		SeatUtilization: seat,
		FeatureAdoption: adoption,
		Facts: map[string]any{
			"customer": map[string]any{"id": a.ID, "arr_usd": a.ARRUSD, "churn_score": churn},
			"health":   map[string]any{"product_health": product, "support_tickets": a.SupportTickets, "nps": a.NPS},
			"usage":    map[string]any{"seat_utilization": seat, "feature_adoption": adoption},
		},
	}
}

func routeAccount(a AccountMetrics, h CustomerHealth, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  enroll in expansion campaign and create AE task\n")
	case "require_review":
		fmt.Printf("  schedule CSM review for ARR $%d\n", a.ARRUSD)
	default:
		fmt.Printf("  trigger retention playbook, churn score=%d\n", h.ChurnScore)
	}
}

func percent(n, d int) int {
	if d == 0 {
		return 0
	}
	return int(math.Round(float64(n) / float64(d) * 100))
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
