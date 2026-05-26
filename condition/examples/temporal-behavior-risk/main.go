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

type CustomerHistory struct {
	ID              string
	AverageSpend30d int
	Events          []BehaviorEvent
	Transactions    []Transaction
}

type BehaviorEvent struct {
	At   time.Time
	Type string
}

type Transaction struct {
	ID        string
	At        time.Time
	AmountUSD int
	NewPayee  bool
}

type BehaviorCase struct {
	Name        string
	Customer    CustomerHistory
	Transaction Transaction
}

type TemporalFeatures struct {
	FailedLogins10m  int
	PasswordReset30m bool
	NewPayee30m      bool
	Velocity1h       int
	AmountMultiple   int
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "temporal-behavior-risk", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range behaviorCases() {
		features := buildTemporalFeatures(c)
		resp, err := svc.Evaluate(ctx, "temporal-behavior-risk", condition.EvaluateRequest{Decision: "temporal_behavior_risk", Input: features.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Transaction.ID, c.Name)
		fmt.Printf("  window: failed10m=%d reset30m=%v new_payee=%v velocity1h=%d amount_x=%d\n", features.FailedLogins10m, features.PasswordReset30m, features.NewPayee30m, features.Velocity1h, features.AmountMultiple)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		enforceBehaviorRisk(c, features, decision.Effect)
	}
}

func behaviorCases() []BehaviorCase {
	now := time.Date(2026, 5, 24, 16, 0, 0, 0, time.UTC)
	return []BehaviorCase{
		{Name: "routine grocery transfer", Customer: CustomerHistory{ID: "cus-1", AverageSpend30d: 90}, Transaction: Transaction{ID: "txn-1", At: now, AmountUSD: 84}},
		{Name: "reset then new payee", Customer: CustomerHistory{ID: "cus-2", AverageSpend30d: 120, Events: repeatedEvents(now, "failed_login", 6, 90), Transactions: []Transaction{{ID: "old", At: now.Add(-20 * time.Minute), AmountUSD: 10, NewPayee: true}}}, Transaction: Transaction{ID: "txn-2", At: now, AmountUSD: 980, NewPayee: true}},
		{Name: "large but explainable invoice", Customer: CustomerHistory{ID: "cus-3", AverageSpend30d: 250, Transactions: []Transaction{{ID: "t-a", At: now.Add(-30 * time.Minute), AmountUSD: 60}, {ID: "t-b", At: now.Add(-20 * time.Minute), AmountUSD: 75}}}, Transaction: Transaction{ID: "txn-3", At: now, AmountUSD: 1200}},
	}
}

func buildTemporalFeatures(c BehaviorCase) TemporalFeatures {
	at := c.Transaction.At
	failed := 0
	reset := false
	for _, event := range c.Customer.Events {
		if event.Type == "failed_login" && at.Sub(event.At) <= 10*time.Minute {
			failed++
		}
		if event.Type == "password_reset" && at.Sub(event.At) <= 30*time.Minute {
			reset = true
		}
	}
	newPayee := c.Transaction.NewPayee
	velocity := 1
	for _, tx := range c.Customer.Transactions {
		if at.Sub(tx.At) <= time.Hour {
			velocity++
		}
		if tx.NewPayee && at.Sub(tx.At) <= 30*time.Minute {
			newPayee = true
		}
	}
	multiple := 99
	if c.Customer.AverageSpend30d > 0 {
		multiple = c.Transaction.AmountUSD / c.Customer.AverageSpend30d
	}
	return TemporalFeatures{
		FailedLogins10m:  failed,
		PasswordReset30m: reset,
		NewPayee30m:      newPayee,
		Velocity1h:       velocity,
		AmountMultiple:   multiple,
		Facts: map[string]any{
			"behavior":    map[string]any{"failed_logins_10m": failed, "password_reset_30m": reset, "new_payee_30m": newPayee},
			"transaction": map[string]any{"amount_usd": c.Transaction.AmountUSD, "amount_vs_30d_avg": multiple, "velocity_1h": velocity},
		},
	}
}

func repeatedEvents(now time.Time, eventType string, count int, secondsApart int) []BehaviorEvent {
	events := []BehaviorEvent{{At: now.Add(-8 * time.Minute), Type: "password_reset"}}
	for i := 0; i < count; i++ {
		events = append(events, BehaviorEvent{At: now.Add(-time.Duration(i*secondsApart) * time.Second), Type: eventType})
	}
	return events
}

func enforceBehaviorRisk(c BehaviorCase, f TemporalFeatures, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  approve transaction and refresh customer baseline for %s\n", c.Customer.ID)
	case "require_review":
		fmt.Printf("  hold transaction, contact customer, velocity=%d amount_x=%d\n", f.Velocity1h, f.AmountMultiple)
	default:
		fmt.Printf("  freeze account %s and cancel new-payee transaction %s\n", c.Customer.ID, c.Transaction.ID)
	}
}
