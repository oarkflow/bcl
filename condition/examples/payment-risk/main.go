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

type AuthorizationRequest struct {
	ID             string
	AmountCents    int
	MerchantID     string
	MerchantMCC    string
	PresentmentISO string
	CreatedAt      time.Time
}

type CardAccount struct {
	PANLast4 string
	BIN      string
	Country  string
	Stolen   bool
}

type CustomerProfile struct {
	ID              string
	HomeCountry     string
	TrustedPayeeIDs map[string]bool
	RecentAuths     []AuthorizationRequest
}

type AuthorizationCase struct {
	Name     string
	Payment  AuthorizationRequest
	Card     CardAccount
	Customer CustomerProfile
}

type AuthorizationEnrichment struct {
	RiskScore       int
	CrossBorder     bool
	VelocityCount   int
	TrustedMerchant bool
	Reasons         []string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "payment-risk", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range authorizationFeed() {
		enriched := enrichAuthorization(c)
		resp, err := svc.Evaluate(ctx, "payment-risk", condition.EvaluateRequest{Decision: "payment_risk", Input: authorizationDecisionFacts(c, enriched), IncludeFeatures: true})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s amount=%s\n", c.Payment.ID, c.Name, money(c.Payment.AmountCents))
		fmt.Printf("  enrich: risk=%d velocity=%d cross_border=%v reasons=%v\n", enriched.RiskScore, enriched.VelocityCount, enriched.CrossBorder, enriched.Reasons)
		fmt.Printf("  decision: effect=%s route=%v policy_score=%.0f reason=%s\n", decision.Effect, decision.Attributes["route"], decision.Score, decision.ReasonCode)
		settleOrHold(c, enriched, decision.Effect)
	}
}

func authorizationFeed() []AuthorizationCase {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return []AuthorizationCase{
		{
			Name:     "reported stolen card at fuel pump",
			Payment:  AuthorizationRequest{ID: "auth-001", AmountCents: 4000, MerchantID: "fuel-9", MerchantMCC: "5542", PresentmentISO: "US", CreatedAt: now},
			Card:     CardAccount{PANLast4: "1111", BIN: "411111", Country: "US", Stolen: true},
			Customer: CustomerProfile{ID: "cus-7", HomeCountry: "US", TrustedPayeeIDs: map[string]bool{}},
		},
		{
			Name:    "new merchant after burst",
			Payment: AuthorizationRequest{ID: "auth-002", AmountCents: 80000, MerchantID: "travel-44", MerchantMCC: "4722", PresentmentISO: "FR", CreatedAt: now},
			Card:    CardAccount{PANLast4: "5555", BIN: "555555", Country: "US"},
			Customer: CustomerProfile{ID: "cus-8", HomeCountry: "US", TrustedPayeeIDs: map[string]bool{}, RecentAuths: []AuthorizationRequest{
				{ID: "old-1", AmountCents: 1200, CreatedAt: now.Add(-20 * time.Minute)},
				{ID: "old-2", AmountCents: 1800, CreatedAt: now.Add(-12 * time.Minute)},
				{ID: "old-3", AmountCents: 2200, CreatedAt: now.Add(-6 * time.Minute)},
			}},
		},
		{
			Name:     "trusted subscription renewal",
			Payment:  AuthorizationRequest{ID: "auth-003", AmountCents: 3500, MerchantID: "saas-1", MerchantMCC: "5734", PresentmentISO: "US", CreatedAt: now},
			Card:     CardAccount{PANLast4: "4242", BIN: "424242", Country: "US"},
			Customer: CustomerProfile{ID: "cus-9", HomeCountry: "US", TrustedPayeeIDs: map[string]bool{"saas-1": true}},
		},
	}
}

func enrichAuthorization(c AuthorizationCase) AuthorizationEnrichment {
	risk := 5
	var reasons []string
	crossBorder := c.Card.Country != c.Payment.PresentmentISO
	if crossBorder {
		risk += 30
		reasons = append(reasons, "cross-border presentment")
	}
	if c.Payment.AmountCents > 50000 {
		risk += 20
		reasons = append(reasons, "high ticket")
	}
	velocity := recentVelocity(c.Customer.RecentAuths, c.Payment.CreatedAt)
	if velocity >= 3 {
		risk += 22
		reasons = append(reasons, "short-window velocity")
	}
	if c.Card.Stolen {
		risk = 99
		reasons = append(reasons, "card hotlisted")
	}
	trusted := c.Customer.TrustedPayeeIDs[c.Payment.MerchantID]
	if trusted {
		risk -= 15
		reasons = append(reasons, "trusted payee")
	}
	sort.Strings(reasons)
	return AuthorizationEnrichment{RiskScore: clamp(risk, 0, 99), CrossBorder: crossBorder, VelocityCount: velocity, TrustedMerchant: trusted, Reasons: reasons}
}

func authorizationDecisionFacts(c AuthorizationCase, e AuthorizationEnrichment) map[string]any {
	return map[string]any{
		"payment": map[string]any{
			"id":           c.Payment.ID,
			"amount_cents": c.Payment.AmountCents,
			"risk_score":   e.RiskScore,
			"cross_border": e.CrossBorder,
			"mcc":          c.Payment.MerchantMCC,
		},
		"card": map[string]any{
			"bin":     c.Card.BIN,
			"stolen":  c.Card.Stolen,
			"country": c.Card.Country,
		},
		"customer": map[string]any{
			"id":      c.Customer.ID,
			"trusted": e.TrustedMerchant,
		},
	}
}

func settleOrHold(c AuthorizationCase, e AuthorizationEnrichment, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  authorize hold, reserve interchange record, capture window=7d\n")
	case "require_review":
		fmt.Printf("  request 3DS challenge, attach risk reasons: %v\n", e.Reasons)
	default:
		fmt.Printf("  decline at network edge, freeze card ending %s\n", c.Card.PANLast4)
	}
}

func recentVelocity(auths []AuthorizationRequest, now time.Time) int {
	count := 0
	for _, auth := range auths {
		if now.Sub(auth.CreatedAt) <= 30*time.Minute {
			count++
		}
	}
	return count
}

func money(cents int) string {
	return fmt.Sprintf("$%.2f", float64(cents)/100)
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
