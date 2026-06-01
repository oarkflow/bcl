package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type WireTransfer struct {
	ID            string
	AmountUSD     int
	OriginCountry string
	DestCountry   string
	CreatedAt     time.Time
}

type CustomerAMLProfile struct {
	ID       string
	RiskTier string
	History  []WireTransfer
}

type Beneficiary struct {
	ID           string
	Country      string
	SanctionHits []string
}

type AMLCase struct {
	Name        string
	Customer    CustomerAMLProfile
	Transfer    WireTransfer
	Beneficiary Beneficiary
}

type AMLNetworkFeatures struct {
	VelocityCount    int
	StructuringScore int
	CountryRisk      int
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "aml-transaction-monitoring", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range amlQueue() {
		features := deriveNetworkFeatures(c)
		resp, err := svc.Evaluate(ctx, "aml-transaction-monitoring", condition.EvaluateRequest{Decision: "aml_monitoring", Input: features.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Transfer.ID, c.Name)
		fmt.Printf("  network: velocity=%d structuring=%d country_risk=%d sanctions=%v\n", features.VelocityCount, features.StructuringScore, features.CountryRisk, c.Beneficiary.SanctionHits)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processTransfer(c, features, decision.Effect)
	}
}

func amlQueue() []AMLCase {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	history := []WireTransfer{
		{ID: "old-1", AmountUSD: 2400, DestCountry: "US", CreatedAt: now.Add(-20 * time.Minute)},
		{ID: "old-2", AmountUSD: 2600, DestCountry: "US", CreatedAt: now.Add(-14 * time.Minute)},
		{ID: "old-3", AmountUSD: 2500, DestCountry: "US", CreatedAt: now.Add(-8 * time.Minute)},
	}
	return []AMLCase{
		{Name: "payroll vendor transfer", Customer: CustomerAMLProfile{ID: "cust-1", RiskTier: "low"}, Transfer: WireTransfer{ID: "wire-1", AmountUSD: 1200, OriginCountry: "US", DestCountry: "US", CreatedAt: now}, Beneficiary: Beneficiary{ID: "ben-1", Country: "US"}},
		{Name: "high risk corridor", Customer: CustomerAMLProfile{ID: "cust-2", RiskTier: "medium", History: history}, Transfer: WireTransfer{ID: "wire-2", AmountUSD: 9800, OriginCountry: "US", DestCountry: "NG", CreatedAt: now}, Beneficiary: Beneficiary{ID: "ben-2", Country: "NG"}},
		{Name: "sanction screening hit", Customer: CustomerAMLProfile{ID: "cust-3", RiskTier: "high"}, Transfer: WireTransfer{ID: "wire-3", AmountUSD: 4300, OriginCountry: "US", DestCountry: "CY", CreatedAt: now}, Beneficiary: Beneficiary{ID: "ben-3", Country: "CY", SanctionHits: []string{"OFAC-SDN"}}},
	}
}

func deriveNetworkFeatures(c AMLCase) AMLNetworkFeatures {
	velocity := 1
	for _, tx := range c.Customer.History {
		if c.Transfer.CreatedAt.Sub(tx.CreatedAt) <= 30*time.Minute {
			velocity++
		}
	}
	structuring := 0
	if velocity >= 4 {
		structuring += 45
	}
	if nearThreshold(c.Transfer.AmountUSD, 10000, 500) {
		structuring += 35
	}
	risk := countryRisk(c.Beneficiary.Country)
	return AMLNetworkFeatures{
		VelocityCount:    velocity,
		StructuringScore: int(math.Min(100, float64(structuring))),
		CountryRisk:      risk,
		Facts: map[string]any{
			"customer":    map[string]any{"id": c.Customer.ID, "risk_tier": c.Customer.RiskTier},
			"transaction": map[string]any{"id": c.Transfer.ID, "amount_usd": c.Transfer.AmountUSD, "cross_border": c.Transfer.OriginCountry != c.Transfer.DestCountry},
			"network":     map[string]any{"velocity_count": velocity, "structuring_score": structuring},
			"beneficiary": map[string]any{"id": c.Beneficiary.ID, "sanction_hit": len(c.Beneficiary.SanctionHits) > 0, "country_risk": risk},
		},
	}
}

func processTransfer(c AMLCase, f AMLNetworkFeatures, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  release wire and append AML clear memo\n")
	case "require_review":
		fmt.Printf("  hold wire, request source-of-funds; structuring=%d\n", f.StructuringScore)
	default:
		fmt.Printf("  block wire and draft SAR for beneficiary %s\n", c.Beneficiary.ID)
	}
}

func nearThreshold(value, threshold, band int) bool {
	return value >= threshold-band && value < threshold
}

func countryRisk(country string) int {
	switch country {
	case "NG":
		return 82
	case "CY":
		return 70
	default:
		return 20
	}
}
