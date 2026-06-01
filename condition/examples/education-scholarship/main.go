package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"sort"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type StudentRecord struct {
	ID             string
	Enrolled       bool
	GPA            float64
	CreditsEarned  int
	CommunityHours int
}

type HouseholdFinance struct {
	IncomeUSD                  int
	HouseholdSize              int
	ExpectedFamilyContribution int
}

type ScholarshipApplication struct {
	ID                   string
	Complete             bool
	SpecialProgram       bool
	EssayScore           int
	RecommendationScores []int
}

type ScholarshipCase struct {
	Name        string
	Student     StudentRecord
	Household   HouseholdFinance
	Application ScholarshipApplication
}

type ScholarshipPacket struct {
	NeedIndex      int
	MeritScore     int
	CommitteeNotes []string
	Facts          map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "education-scholarship", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range applications() {
		packet := scoreApplication(c)
		resp, err := svc.Evaluate(ctx, "education-scholarship", condition.EvaluateRequest{Decision: "scholarship_award", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Application.ID, c.Name)
		fmt.Printf("  score: merit=%d need=%d notes=%v\n", packet.MeritScore, packet.NeedIndex, packet.CommitteeNotes)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processScholarship(c, packet, decision.Effect)
	}
}

func applications() []ScholarshipCase {
	return []ScholarshipCase{
		{Name: "merit need award", Student: StudentRecord{ID: "stu-1", Enrolled: true, GPA: 3.8, CreditsEarned: 80, CommunityHours: 120}, Household: HouseholdFinance{IncomeUSD: 52000, HouseholdSize: 4, ExpectedFamilyContribution: 2500}, Application: ScholarshipApplication{ID: "app-1", Complete: true, EssayScore: 92, RecommendationScores: []int{90, 88}}},
		{Name: "special program review", Student: StudentRecord{ID: "stu-2", Enrolled: true, GPA: 3.5, CreditsEarned: 64, CommunityHours: 40}, Household: HouseholdFinance{IncomeUSD: 64000, HouseholdSize: 3, ExpectedFamilyContribution: 5000}, Application: ScholarshipApplication{ID: "app-2", Complete: true, SpecialProgram: true, EssayScore: 85, RecommendationScores: []int{82, 86}}},
		{Name: "incomplete application", Student: StudentRecord{ID: "stu-3", Enrolled: true, GPA: 3.9, CreditsEarned: 75, CommunityHours: 90}, Household: HouseholdFinance{IncomeUSD: 38000, HouseholdSize: 5}, Application: ScholarshipApplication{ID: "app-3", Complete: false, EssayScore: 95}},
	}
}

func scoreApplication(c ScholarshipCase) ScholarshipPacket {
	need := int(math.Max(0, 100-float64(c.Household.ExpectedFamilyContribution/100)-float64(c.Household.IncomeUSD/2000)))
	merit := int(math.Round(c.Student.GPA*20)) + c.Application.EssayScore/5 + average(c.Application.RecommendationScores)/5 + c.Student.CommunityHours/20
	var notes []string
	if c.Application.SpecialProgram {
		notes = append(notes, "special-program")
	}
	if !c.Application.Complete {
		notes = append(notes, "missing-materials")
	}
	sort.Strings(notes)
	return ScholarshipPacket{
		NeedIndex:      need,
		MeritScore:     merit,
		CommitteeNotes: notes,
		Facts: map[string]any{
			"student":     map[string]any{"id": c.Student.ID, "enrolled": c.Student.Enrolled, "gpa": c.Student.GPA},
			"household":   map[string]any{"income_usd": c.Household.IncomeUSD},
			"application": map[string]any{"id": c.Application.ID, "complete": c.Application.Complete, "special_program": c.Application.SpecialProgram},
		},
	}
}

func processScholarship(c ScholarshipCase, p ScholarshipPacket, effect string) {
	switch effect {
	case "allow":
		award := 2000 + p.NeedIndex*40
		fmt.Printf("  reserve award $%d for %s\n", award, c.Student.ID)
	case "require_review":
		fmt.Printf("  send to committee with notes=%v\n", p.CommitteeNotes)
	default:
		fmt.Printf("  mark incomplete and request missing documents\n")
	}
}

func average(values []int) int {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return sum / len(values)
}
