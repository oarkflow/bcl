package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Claim struct {
	ID           string
	Type         string
	Photos       int
	PoliceReport bool
	LineItems    []ClaimLine
}

type ClaimLine struct {
	Description string
	AmountUSD   int
}

type Policy struct {
	ID             string
	DeductibleUSD  int
	Coverages      []string
	PreviousClaims []string
}

type Provider struct {
	ID              string
	Suspicious      bool
	OverbillingRate float64
}

type ClaimCase struct {
	Name     string
	Claim    Claim
	Policy   Policy
	Provider Provider
}

type ClaimFeatures struct {
	EstimateUSD      int
	FraudScore       int
	MissingDocuments bool
	CoverageGap      bool
	Duplicate        bool
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "insurance-claims", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range claimInbox() {
		features := estimateClaim(c)
		resp, err := svc.Evaluate(ctx, "insurance-claims", condition.EvaluateRequest{Decision: "claim_triage", Input: features.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Claim.ID, c.Name)
		fmt.Printf("  estimate=$%d fraud=%d missing_docs=%v coverage_gap=%v duplicate=%v\n", features.EstimateUSD, features.FraudScore, features.MissingDocuments, features.CoverageGap, features.Duplicate)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processClaim(c, features, decision.Effect)
	}
}

func claimInbox() []ClaimCase {
	return []ClaimCase{
		{Name: "windshield replacement", Claim: Claim{ID: "clm-1", Type: "glass", Photos: 3, LineItems: []ClaimLine{{"glass", 650}}}, Policy: Policy{ID: "pol-1", DeductibleUSD: 100, Coverages: []string{"glass"}}, Provider: Provider{ID: "shop-1"}},
		{Name: "water damage missing report", Claim: Claim{ID: "clm-2", Type: "property", Photos: 1, LineItems: []ClaimLine{{"flooring", 4200}, {"drywall", 2400}}}, Policy: Policy{ID: "pol-2", DeductibleUSD: 500, Coverages: []string{"property"}}, Provider: Provider{ID: "contractor-2"}},
		{Name: "duplicate suspicious provider", Claim: Claim{ID: "clm-3", Type: "medical", Photos: 0, LineItems: []ClaimLine{{"therapy", 900}, {"therapy", 950}}}, Policy: Policy{ID: "pol-3", DeductibleUSD: 250, Coverages: []string{"medical"}, PreviousClaims: []string{"clm-3"}}, Provider: Provider{ID: "clinic-9", Suspicious: true, OverbillingRate: 0.42}},
	}
}

func estimateClaim(c ClaimCase) ClaimFeatures {
	estimate := 0
	for _, item := range c.Claim.LineItems {
		estimate += item.AmountUSD
	}
	missing := c.Claim.Photos < 2 || (c.Claim.Type == "property" && !c.Claim.PoliceReport)
	coverageGap := !contains(c.Policy.Coverages, c.Claim.Type)
	duplicate := contains(c.Policy.PreviousClaims, c.Claim.ID)
	fraud := 5
	if duplicate {
		fraud += 50
	}
	if c.Provider.Suspicious {
		fraud += 45
	}
	fraud += int(math.Round(c.Provider.OverbillingRate * 40))
	if missing {
		fraud += 10
	}
	return ClaimFeatures{
		EstimateUSD:      estimate,
		FraudScore:       min(fraud, 100),
		MissingDocuments: missing,
		CoverageGap:      coverageGap,
		Duplicate:        duplicate,
		Facts: map[string]any{
			"claim":    map[string]any{"id": c.Claim.ID, "estimate_usd": estimate, "fraud_score": min(fraud, 100), "coverage_gap": coverageGap, "missing_documents": missing, "duplicate": duplicate},
			"provider": map[string]any{"id": c.Provider.ID, "suspicious": c.Provider.Suspicious},
		},
	}
}

func processClaim(c ClaimCase, f ClaimFeatures, effect string) {
	switch effect {
	case "allow":
		payout := max(0, f.EstimateUSD-c.Policy.DeductibleUSD)
		fmt.Printf("  settle claim for $%d after deductible\n", payout)
	case "require_review":
		fmt.Printf("  assign adjuster and request missing evidence\n")
	default:
		fmt.Printf("  route to SIU; preserve provider=%s evidence\n", c.Provider.ID)
	}
}

func contains(values []string, want string) bool {
	sort.Strings(values)
	i := sort.SearchStrings(values, want)
	return i < len(values) && values[i] == want
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
