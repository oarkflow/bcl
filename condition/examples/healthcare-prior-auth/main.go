package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Member struct {
	ID             string
	PlanActive     bool
	Pregnant       bool
	Allergies      []string
	PriorTherapies []string
}

type ClinicalRequest struct {
	ID          string
	Diagnosis   string
	Treatment   string
	RequestedAt time.Time
	Documents   []string
}

type MedicalPolicy struct {
	Treatment                      string
	CostUSD                        int
	RequiredDocuments              []string
	SupportedDiagnoses             []string
	ContraindicatedInPregnancy     bool
	ContraindicatedAllergyKeywords []string
}

type PriorAuthCase struct {
	Name    string
	Member  Member
	Request ClinicalRequest
	Policy  MedicalPolicy
}

type ClinicalPacket struct {
	MissingDocuments []string
	DiagnosisCovered bool
	AllergyMatch     bool
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "healthcare-prior-auth", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range utilizationReviewQueue() {
		packet := assembleClinicalPacket(c)
		resp, err := svc.Evaluate(ctx, "healthcare-prior-auth", condition.EvaluateRequest{Decision: "prior_auth", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Request.ID, c.Name)
		fmt.Printf("  policy: diagnosis_covered=%v missing=%v allergy_match=%v cost=$%d\n", packet.DiagnosisCovered, packet.MissingDocuments, packet.AllergyMatch, c.Policy.CostUSD)
		fmt.Printf("  decision: effect=%s queue=%v reason=%s\n", decision.Effect, decision.Attributes["queue"], decision.ReasonCode)
		applyAuthorization(c, packet, decision.Effect)
	}
}

func utilizationReviewQueue() []PriorAuthCase {
	now := time.Date(2026, 5, 24, 9, 0, 0, 0, time.UTC)
	return []PriorAuthCase{
		{Name: "guideline matched imaging", Member: Member{ID: "pat-1", PlanActive: true}, Request: ClinicalRequest{ID: "pa-1", Diagnosis: "M54.16", Treatment: "MRI-LUMBAR", RequestedAt: now, Documents: []string{"clinical_notes", "xray_report"}}, Policy: mriPolicy()},
		{Name: "missing migraine notes", Member: Member{ID: "pat-2", PlanActive: true, PriorTherapies: []string{"topiramate"}}, Request: ClinicalRequest{ID: "pa-2", Diagnosis: "G43.909", Treatment: "BOTOX", RequestedAt: now, Documents: []string{"prescription"}}, Policy: botoxPolicy()},
		{Name: "contraindicated medication", Member: Member{ID: "pat-3", PlanActive: true, Pregnant: true}, Request: ClinicalRequest{ID: "pa-3", Diagnosis: "L70.0", Treatment: "ISOTRETINOIN", RequestedAt: now, Documents: []string{"clinical_notes"}}, Policy: isotretinoinPolicy()},
	}
}

func assembleClinicalPacket(c PriorAuthCase) ClinicalPacket {
	missing := missingDocuments(c.Policy.RequiredDocuments, c.Request.Documents)
	covered := contains(c.Policy.SupportedDiagnoses, c.Request.Diagnosis)
	allergyMatch := allergyMatch(c.Member.Allergies, c.Policy.ContraindicatedAllergyKeywords)
	complete := len(missing) == 0
	return ClinicalPacket{
		MissingDocuments: missing,
		DiagnosisCovered: covered,
		AllergyMatch:     allergyMatch,
		Facts: map[string]any{
			"patient": map[string]any{
				"id":            c.Member.ID,
				"plan_active":   c.Member.PlanActive,
				"pregnant":      c.Member.Pregnant,
				"allergy_match": allergyMatch,
			},
			"diagnosis": map[string]any{
				"code":               c.Request.Diagnosis,
				"confirmed":          covered,
				"needs_confirmation": !covered,
			},
			"evidence": map[string]any{
				"complete":        complete,
				"missing_records": !complete,
			},
			"treatment": map[string]any{
				"code":                         c.Request.Treatment,
				"cost_usd":                     c.Policy.CostUSD,
				"guideline_supported":          covered,
				"contraindicated_in_pregnancy": c.Policy.ContraindicatedInPregnancy,
			},
		},
	}
}

func applyAuthorization(c PriorAuthCase, packet ClinicalPacket, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  issue auth number AUTH-%s, valid for 60 days\n", strings.TrimPrefix(c.Request.ID, "pa-"))
	case "require_review":
		fmt.Printf("  request missing evidence from provider: %s\n", strings.Join(packet.MissingDocuments, ","))
	default:
		fmt.Printf("  deny %s, notify prescriber with safety rationale\n", c.Request.Treatment)
	}
}

func mriPolicy() MedicalPolicy {
	return MedicalPolicy{Treatment: "MRI-LUMBAR", CostUSD: 1200, RequiredDocuments: []string{"clinical_notes", "xray_report"}, SupportedDiagnoses: []string{"M54.16"}}
}

func botoxPolicy() MedicalPolicy {
	return MedicalPolicy{Treatment: "BOTOX", CostUSD: 2600, RequiredDocuments: []string{"clinical_notes", "headache_diary", "failed_therapies"}, SupportedDiagnoses: []string{"G43.909"}}
}

func isotretinoinPolicy() MedicalPolicy {
	return MedicalPolicy{Treatment: "ISOTRETINOIN", CostUSD: 700, RequiredDocuments: []string{"clinical_notes"}, SupportedDiagnoses: []string{"L70.0"}, ContraindicatedInPregnancy: true}
}

func missingDocuments(required, present []string) []string {
	var missing []string
	for _, doc := range required {
		if !contains(present, doc) {
			missing = append(missing, doc)
		}
	}
	sort.Strings(missing)
	return missing
}

func allergyMatch(allergies, keywords []string) bool {
	for _, allergy := range allergies {
		for _, keyword := range keywords {
			if strings.Contains(strings.ToLower(allergy), strings.ToLower(keyword)) {
				return true
			}
		}
	}
	return false
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
