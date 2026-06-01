package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type CitizenRecord struct {
	ID              string
	Resident        bool
	Age             int
	AddressVerified bool
	PriorFraudFlags []string
}

type HouseholdEvidence struct {
	IncomeUSD int
	Members   int
	Documents map[string]bool
}

type BenefitProgram struct {
	ID           string
	MaxIncomeUSD int
	RequiredDocs []string
}

type BenefitApplication struct {
	ID        string
	Applicant CitizenRecord
	Household HouseholdEvidence
	Program   BenefitProgram
}

type EligibilityPacket struct {
	MissingDocuments []string
	FraudHold        bool
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "government-benefits", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, app := range applications() {
		packet := verifyApplication(app)
		resp, err := svc.Evaluate(ctx, "government-benefits", condition.EvaluateRequest{Decision: "benefit_eligibility", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", app.ID, app.Program.ID)
		fmt.Printf("  verification: missing=%v fraud_hold=%v income=%d/%d\n", packet.MissingDocuments, packet.FraudHold, app.Household.IncomeUSD, app.Program.MaxIncomeUSD)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processApplication(app, packet, decision.Effect)
	}
}

func applications() []BenefitApplication {
	return []BenefitApplication{
		{ID: "app-1", Applicant: CitizenRecord{ID: "cit-1", Resident: true, Age: 42, AddressVerified: true}, Household: HouseholdEvidence{IncomeUSD: 28000, Members: 3, Documents: map[string]bool{"id": true, "income": true, "address": true}}, Program: BenefitProgram{ID: "food-assistance", MaxIncomeUSD: 36000, RequiredDocs: []string{"id", "income", "address"}}},
		{ID: "app-2", Applicant: CitizenRecord{ID: "cit-2", Resident: true, Age: 31}, Household: HouseholdEvidence{IncomeUSD: 22000, Members: 2, Documents: map[string]bool{"id": true}}, Program: BenefitProgram{ID: "housing", MaxIncomeUSD: 40000, RequiredDocs: []string{"id", "income", "address"}}},
		{ID: "app-3", Applicant: CitizenRecord{ID: "cit-3", Age: 45}, Household: HouseholdEvidence{IncomeUSD: 18000, Members: 1, Documents: map[string]bool{"id": true, "income": true}}, Program: BenefitProgram{ID: "cash-assistance", MaxIncomeUSD: 30000, RequiredDocs: []string{"id", "income"}}},
	}
}

func verifyApplication(app BenefitApplication) EligibilityPacket {
	var missing []string
	for _, doc := range app.Program.RequiredDocs {
		if !app.Household.Documents[doc] {
			missing = append(missing, doc)
		}
	}
	sort.Strings(missing)
	fraudHold := len(app.Applicant.PriorFraudFlags) > 0
	return EligibilityPacket{
		MissingDocuments: missing,
		FraudHold:        fraudHold,
		Facts: map[string]any{
			"applicant":   map[string]any{"id": app.Applicant.ID, "resident": app.Applicant.Resident, "age": app.Applicant.Age, "address_verified": app.Applicant.AddressVerified},
			"household":   map[string]any{"income_usd": app.Household.IncomeUSD, "members": app.Household.Members},
			"program":     map[string]any{"id": app.Program.ID, "max_income_usd": app.Program.MaxIncomeUSD},
			"application": map[string]any{"id": app.ID, "missing_documents": len(missing) > 0, "fraud_hold": fraudHold},
		},
	}
}

func processApplication(app BenefitApplication, p EligibilityPacket, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  open case and schedule first payment for citizen %s\n", app.Applicant.ID)
	case "require_review":
		fmt.Printf("  assign caseworker, request documents=%v\n", p.MissingDocuments)
	default:
		fmt.Printf("  send statutory denial notice to %s\n", app.Applicant.ID)
	}
}
