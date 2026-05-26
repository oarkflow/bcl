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

type Seller struct {
	ID                    string
	TenureDays            int
	ChargebackRatePercent int
	DeviceID              string
	PayoutID              string
	KnownBad              bool
}

type GraphCase struct {
	Name    string
	Seller  Seller
	Network []Seller
}

type GraphFeatures struct {
	SharedDeviceAccounts int
	SharedPayoutAccounts int
	KnownBadNeighbors    int
	ChargebackNeighbors  int
	Facts                map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "fraud-ring-graph", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range graphCases() {
		features := buildGraphFeatures(c)
		resp, err := svc.Evaluate(ctx, "fraud-ring-graph", condition.EvaluateRequest{Decision: "fraud_ring_graph", Input: features.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Seller.ID, c.Name)
		fmt.Printf("  graph: shared_device=%d shared_payout=%d known_bad=%d chargeback_neighbors=%d\n", features.SharedDeviceAccounts, features.SharedPayoutAccounts, features.KnownBadNeighbors, features.ChargebackNeighbors)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		processSellerGraph(c, features, decision.Effect)
	}
}

func graphCases() []GraphCase {
	network := []Seller{
		{ID: "s-10", DeviceID: "dev-a", PayoutID: "bank-1", KnownBad: true, ChargebackRatePercent: 18},
		{ID: "s-11", DeviceID: "dev-a", PayoutID: "bank-1", ChargebackRatePercent: 9},
		{ID: "s-12", DeviceID: "dev-b", PayoutID: "bank-2", ChargebackRatePercent: 7},
		{ID: "s-13", DeviceID: "dev-a", PayoutID: "bank-3", ChargebackRatePercent: 1},
		{ID: "s-14", DeviceID: "dev-a", PayoutID: "bank-4", ChargebackRatePercent: 3},
	}
	return []GraphCase{
		{Name: "isolated veteran seller", Seller: Seller{ID: "s-1", TenureDays: 420, ChargebackRatePercent: 1, DeviceID: "dev-z", PayoutID: "bank-z"}, Network: network},
		{Name: "device-linked seller ring", Seller: Seller{ID: "s-2", TenureDays: 14, ChargebackRatePercent: 5, DeviceID: "dev-a", PayoutID: "bank-x"}, Network: network},
		{Name: "shared payout cluster", Seller: Seller{ID: "s-3", TenureDays: 80, ChargebackRatePercent: 1, DeviceID: "dev-c", PayoutID: "bank-1"}, Network: network},
	}
}

func buildGraphFeatures(c GraphCase) GraphFeatures {
	var sharedDevice, sharedPayout, knownBad, chargeback int
	for _, neighbor := range c.Network {
		linked := false
		if neighbor.DeviceID == c.Seller.DeviceID {
			sharedDevice++
			linked = true
		}
		if neighbor.PayoutID == c.Seller.PayoutID {
			sharedPayout++
			linked = true
		}
		if linked && neighbor.KnownBad {
			knownBad++
		}
		if linked && neighbor.ChargebackRatePercent >= 5 {
			chargeback++
		}
	}
	return GraphFeatures{
		SharedDeviceAccounts: sharedDevice,
		SharedPayoutAccounts: sharedPayout,
		KnownBadNeighbors:    knownBad,
		ChargebackNeighbors:  chargeback,
		Facts: map[string]any{
			"seller": map[string]any{"id": c.Seller.ID, "tenure_days": c.Seller.TenureDays, "chargeback_rate_percent": c.Seller.ChargebackRatePercent},
			"graph":  map[string]any{"shared_device_accounts": sharedDevice, "shared_payout_accounts": sharedPayout, "known_bad_neighbors": knownBad, "chargeback_neighbors": chargeback},
		},
	}
}

func processSellerGraph(c GraphCase, f GraphFeatures, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  release payout for seller %s\n", c.Seller.ID)
	case "require_review":
		fmt.Printf("  open seller-risk investigation, shared_payout=%d chargeback_neighbors=%d\n", f.SharedPayoutAccounts, f.ChargebackNeighbors)
	default:
		fmt.Printf("  suspend seller %s and freeze linked payout instrument %s\n", c.Seller.ID, c.Seller.PayoutID)
	}
}
