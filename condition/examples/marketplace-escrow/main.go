package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type MarketplaceOrder struct {
	ID          string
	AmountUSD   int
	CreatedAt   time.Time
	DisputeOpen bool
}

type SellerRisk struct {
	ID              string
	DeliveredOrders int
	DisputeRate     float64
	FraudFlag       bool
}

type BuyerState struct {
	ID             string
	ChargebackOpen bool
	Messages       []string
}

type FulfillmentEvidence struct {
	DeliveredAt   *time.Time
	TrackingScans []time.Time
	PromisedBy    time.Time
	ProofUploaded bool
}

type EscrowCase struct {
	Name        string
	Order       MarketplaceOrder
	Seller      SellerRisk
	Buyer       BuyerState
	Fulfillment FulfillmentEvidence
}

type EscrowPacket struct {
	Delivered  bool
	DaysLate   int
	TrustScore int
	Evidence   []string
	Facts      map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "marketplace-escrow", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range escrowQueue() {
		packet := buildEscrowPacket(c)
		resp, err := svc.Evaluate(ctx, "marketplace-escrow", condition.EvaluateRequest{Decision: "escrow_release", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Order.ID, c.Name)
		fmt.Printf("  escrow: delivered=%v late=%dd trust=%d evidence=%v\n", packet.Delivered, packet.DaysLate, packet.TrustScore, packet.Evidence)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processEscrow(c, packet, decision.Effect)
	}
}

func escrowQueue() []EscrowCase {
	now := time.Date(2026, 5, 24, 9, 0, 0, 0, time.UTC)
	delivered := now.Add(-24 * time.Hour)
	late := now.Add(-12 * time.Hour)
	return []EscrowCase{
		{Name: "trusted delivered order", Order: MarketplaceOrder{ID: "ord-1", AmountUSD: 300, CreatedAt: now.Add(-5 * 24 * time.Hour)}, Seller: SellerRisk{ID: "sel-1", DeliveredOrders: 1200, DisputeRate: 0.01}, Buyer: BuyerState{ID: "buy-1"}, Fulfillment: FulfillmentEvidence{DeliveredAt: &delivered, PromisedBy: now, ProofUploaded: true}},
		{Name: "late expensive shipment", Order: MarketplaceOrder{ID: "ord-2", AmountUSD: 3200, CreatedAt: now.Add(-10 * 24 * time.Hour)}, Seller: SellerRisk{ID: "sel-2", DeliveredOrders: 80, DisputeRate: 0.06}, Buyer: BuyerState{ID: "buy-2"}, Fulfillment: FulfillmentEvidence{DeliveredAt: &late, PromisedBy: now.Add(-5 * 24 * time.Hour), ProofUploaded: true}},
		{Name: "active buyer dispute", Order: MarketplaceOrder{ID: "ord-3", AmountUSD: 450, CreatedAt: now.Add(-4 * 24 * time.Hour), DisputeOpen: true}, Seller: SellerRisk{ID: "sel-3", DeliveredOrders: 900, DisputeRate: 0.02}, Buyer: BuyerState{ID: "buy-3", Messages: []string{"item not as described"}}, Fulfillment: FulfillmentEvidence{DeliveredAt: &delivered, PromisedBy: now, ProofUploaded: true}},
	}
}

func buildEscrowPacket(c EscrowCase) EscrowPacket {
	delivered := c.Fulfillment.DeliveredAt != nil && c.Fulfillment.ProofUploaded
	daysLate := 0
	if c.Fulfillment.DeliveredAt != nil && c.Fulfillment.DeliveredAt.After(c.Fulfillment.PromisedBy) {
		daysLate = int(c.Fulfillment.DeliveredAt.Sub(c.Fulfillment.PromisedBy).Hours() / 24)
	}
	trust := 95 - int(c.Seller.DisputeRate*500)
	if c.Seller.DeliveredOrders < 100 {
		trust -= 20
	}
	var evidence []string
	if c.Fulfillment.ProofUploaded {
		evidence = append(evidence, "delivery-proof")
	}
	if len(c.Buyer.Messages) > 0 {
		evidence = append(evidence, "buyer-message")
	}
	sort.Strings(evidence)
	return EscrowPacket{
		Delivered:  delivered,
		DaysLate:   daysLate,
		TrustScore: trust,
		Evidence:   evidence,
		Facts: map[string]any{
			"order":       map[string]any{"id": c.Order.ID, "amount_usd": c.Order.AmountUSD, "dispute_open": c.Order.DisputeOpen},
			"seller":      map[string]any{"id": c.Seller.ID, "trust_score": trust, "fraud_flag": c.Seller.FraudFlag},
			"buyer":       map[string]any{"id": c.Buyer.ID, "chargeback_open": c.Buyer.ChargebackOpen},
			"fulfillment": map[string]any{"delivered": delivered, "days_late": daysLate},
		},
	}
}

func processEscrow(c EscrowCase, p EscrowPacket, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  release payout net of marketplace fees to seller %s\n", c.Seller.ID)
	case "require_review":
		fmt.Printf("  hold payout, send evidence packet %v to marketplace ops\n", p.Evidence)
	default:
		fmt.Printf("  freeze funds and link dispute messages=%v\n", c.Buyer.Messages)
	}
}
