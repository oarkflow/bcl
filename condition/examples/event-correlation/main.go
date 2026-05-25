package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type CommerceEvent struct {
	At   time.Time
	Type string
}

type Order struct {
	ID              string
	At              time.Time
	TotalCents      int
	GiftCardCents   int
	ShippingCountry string
}

type CorrelationCase struct {
	Name   string
	Events []CommerceEvent
	Order  Order
}

type SequenceFeatures struct {
	NewDeviceLogin   bool
	PasswordReset    bool
	AddressChange    bool
	TakeoverPurchase bool
	PartialTakeover  bool
	Benign           bool
	GiftCardPercent  int
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "event-correlation", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range correlationCases() {
		features := correlate(c)
		resp, err := svc.Evaluate(ctx, "event-correlation", condition.EvaluateRequest{Decision: "event_correlation", Input: features.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Order.ID, c.Name)
		fmt.Printf("  sequence: new_device=%v reset=%v address_change=%v gift_card=%d%%\n", features.NewDeviceLogin, features.PasswordReset, features.AddressChange, features.GiftCardPercent)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
	}
}

func correlationCases() []CorrelationCase {
	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	return []CorrelationCase{
		{Name: "regular reorder", Events: []CommerceEvent{{At: now.Add(-2 * time.Hour), Type: "trusted_login"}}, Order: Order{ID: "ord-1", At: now, TotalCents: 9400}},
		{Name: "full takeover pattern", Events: []CommerceEvent{{At: now.Add(-50 * time.Minute), Type: "new_device_login"}, {At: now.Add(-45 * time.Minute), Type: "password_reset"}, {At: now.Add(-30 * time.Minute), Type: "address_change"}}, Order: Order{ID: "ord-2", At: now, TotalCents: 50000, GiftCardCents: 50000}},
		{Name: "partial sequence", Events: []CommerceEvent{{At: now.Add(-20 * time.Minute), Type: "new_device_login"}, {At: now.Add(-18 * time.Minute), Type: "password_reset"}}, Order: Order{ID: "ord-3", At: now, TotalCents: 26000, GiftCardCents: 10000}},
	}
}

func correlate(c CorrelationCase) SequenceFeatures {
	var newDevice, reset, address bool
	for _, event := range c.Events {
		if c.Order.At.Sub(event.At) > time.Hour {
			continue
		}
		switch event.Type {
		case "new_device_login":
			newDevice = true
		case "password_reset":
			reset = true
		case "address_change":
			address = true
		}
	}
	giftCardPercent := 0
	if c.Order.TotalCents > 0 {
		giftCardPercent = c.Order.GiftCardCents * 100 / c.Order.TotalCents
	}
	takeoverPurchase := newDevice && reset && address && giftCardPercent >= 80
	partialTakeover := newDevice && reset && !takeoverPurchase
	benign := !newDevice && giftCardPercent < 50
	return SequenceFeatures{
		NewDeviceLogin:   newDevice,
		PasswordReset:    reset,
		AddressChange:    address,
		TakeoverPurchase: takeoverPurchase,
		PartialTakeover:  partialTakeover,
		Benign:           benign,
		GiftCardPercent:  giftCardPercent,
		Facts: map[string]any{
			"sequence": map[string]any{"new_device_login": newDevice, "password_reset": reset, "address_change": address, "takeover_purchase": takeoverPurchase, "partial_takeover": partialTakeover, "benign": benign},
			"order":    map[string]any{"gift_card_percent": giftCardPercent, "shipping_country": c.Order.ShippingCountry},
		},
	}
}
