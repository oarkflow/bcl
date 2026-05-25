package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Account struct {
	ID          string
	RiskProfile string
	KYCValid    bool
}

type Holding struct {
	Symbol   string
	ValueUSD int
}

type TradeOrder struct {
	ID        string
	Symbol    string
	Side      string
	Quantity  int
	PriceUSD  int
	AssetRisk string
}

type TradeCase struct {
	Name           string
	Account        Account
	Holdings       []Holding
	Order          TradeOrder
	RestrictedList map[string]bool
}

type CompliancePacket struct {
	NotionalUSD               int
	SuitabilityScore          int
	ConcentrationAfterPercent int
	PositionLimitBreached     bool
	RestrictedSecurity        bool
	Facts                     map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "investment-trade-compliance", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range orders() {
		packet := preTradeCheck(c)
		resp, err := svc.Evaluate(ctx, "investment-trade-compliance", condition.EvaluateRequest{Decision: "trade_compliance", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Order.ID, c.Name)
		fmt.Printf("  compliance: notional=$%d suitability=%d concentration=%d%% restricted=%v\n", packet.NotionalUSD, packet.SuitabilityScore, packet.ConcentrationAfterPercent, packet.RestrictedSecurity)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		routeTrade(c, packet, decision.Effect)
	}
}

func orders() []TradeCase {
	restricted := map[string]bool{"XYZ": true}
	return []TradeCase{
		{Name: "balanced ETF buy", Account: Account{ID: "acct-1", RiskProfile: "growth", KYCValid: true}, Holdings: []Holding{{"CASH", 200000}, {"VTI", 300000}}, Order: TradeOrder{ID: "ord-1", Symbol: "VTI", Side: "buy", Quantity: 100, PriceUSD: 250, AssetRisk: "medium"}, RestrictedList: restricted},
		{Name: "unsuitable crypto allocation", Account: Account{ID: "acct-2", RiskProfile: "balanced", KYCValid: true}, Holdings: []Holding{{"CASH", 400000}, {"BND", 250000}}, Order: TradeOrder{ID: "ord-2", Symbol: "BTCF", Side: "buy", Quantity: 300, PriceUSD: 500, AssetRisk: "high"}, RestrictedList: restricted},
		{Name: "restricted issuer", Account: Account{ID: "acct-3", RiskProfile: "growth", KYCValid: true}, Holdings: []Holding{{"CASH", 500000}}, Order: TradeOrder{ID: "ord-3", Symbol: "XYZ", Side: "buy", Quantity: 1000, PriceUSD: 20, AssetRisk: "medium"}, RestrictedList: restricted},
	}
}

func preTradeCheck(c TradeCase) CompliancePacket {
	notional := c.Order.Quantity * c.Order.PriceUSD
	portfolioValue := notional
	for _, h := range c.Holdings {
		portfolioValue += h.ValueUSD
	}
	concentration := int(math.Round(float64(notional) / float64(portfolioValue) * 100))
	suitability := suitabilityScore(c.Account.RiskProfile, c.Order.AssetRisk)
	limitBreached := concentration > 35
	restricted := c.RestrictedList[c.Order.Symbol]
	return CompliancePacket{
		NotionalUSD:               notional,
		SuitabilityScore:          suitability,
		ConcentrationAfterPercent: concentration,
		PositionLimitBreached:     limitBreached,
		RestrictedSecurity:        restricted,
		Facts: map[string]any{
			"account":   map[string]any{"id": c.Account.ID, "kyc_valid": c.Account.KYCValid},
			"trade":     map[string]any{"id": c.Order.ID, "restricted_security": restricted, "suitability_score": suitability, "notional_usd": notional},
			"portfolio": map[string]any{"position_limit_breached": limitBreached, "concentration_after_percent": concentration},
		},
	}
}

func routeTrade(c TradeCase, p CompliancePacket, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  send order to broker with pre-trade approval id\n")
	case "require_review":
		fmt.Printf("  hold order for supervisor attestation, concentration=%d%%\n", p.ConcentrationAfterPercent)
	default:
		fmt.Printf("  block order %s and record compliance event\n", c.Order.ID)
	}
}

func suitabilityScore(profile, assetRisk string) int {
	matrix := map[string]map[string]int{
		"balanced": {"low": 95, "medium": 80, "high": 55},
		"growth":   {"low": 90, "medium": 92, "high": 75},
	}
	if scores, ok := matrix[profile]; ok {
		return scores[assetRisk]
	}
	return 50
}
