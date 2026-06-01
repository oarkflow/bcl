package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Applicant struct {
	ID          string
	FullName    string
	DateOfBirth time.Time
	Country     string
}

type UploadedDocument struct {
	Type           string
	Number         string
	Expiry         time.Time
	ExtractedName  string
	MRZChecksumOK  bool
	OCRConfidence  float64
	TamperSignals  []string
	AddressCountry string
}

type SelfieSession struct {
	FaceMatch        float64
	Liveness         float64
	DeviceID         string
	Attempts         int
	PresentationRisk float64
}

type ScreeningHit struct {
	List        string
	Score       float64
	Disposition string
	Type        string
}

type OnboardingFile struct {
	ID        string
	Applicant Applicant
	Document  UploadedDocument
	Selfie    SelfieSession
	Hits      []ScreeningHit
}

type EvidencePackage struct {
	Profile   map[string]any
	Document  map[string]any
	Biometric map[string]any
	Screening map[string]any
	Summary   []string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "kyc-onboarding", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, file := range onboardingQueue() {
		evidence := buildEvidence(file, time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC))
		resp, err := svc.Evaluate(ctx, "kyc-onboarding", condition.EvaluateRequest{Decision: "kyc_onboarding", Input: evidence.Facts()})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", file.ID, file.Applicant.FullName)
		fmt.Printf("  evidence: %s\n", strings.Join(evidence.Summary, "; "))
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		executeKYCOutcome(file, evidence, decision.Effect)
	}
}

func onboardingQueue() []OnboardingFile {
	return []OnboardingFile{
		{
			ID:        "kyc-100",
			Applicant: Applicant{ID: "app-100", FullName: "Maya Chen", DateOfBirth: date("1994-03-12"), Country: "US"},
			Document:  UploadedDocument{Type: "passport", Number: "P1002003", Expiry: date("2031-09-01"), ExtractedName: "Maya Chen", MRZChecksumOK: true, OCRConfidence: 0.96, AddressCountry: "US"},
			Selfie:    SelfieSession{FaceMatch: 0.97, Liveness: 0.98, DeviceID: "ios-a1", Attempts: 1, PresentationRisk: 0.04},
		},
		{
			ID:        "kyc-101",
			Applicant: Applicant{ID: "app-101", FullName: "Jonas Patel", DateOfBirth: date("1981-11-08"), Country: "GB"},
			Document:  UploadedDocument{Type: "driver_license", Number: "D7788", Expiry: date("2027-02-14"), ExtractedName: "Jonas Pate1", MRZChecksumOK: true, OCRConfidence: 0.76, TamperSignals: []string{"font_mismatch", "barcode_blur"}, AddressCountry: "GB"},
			Selfie:    SelfieSession{FaceMatch: 0.91, Liveness: 0.96, DeviceID: "android-z9", Attempts: 2, PresentationRisk: 0.11},
		},
		{
			ID:        "kyc-102",
			Applicant: Applicant{ID: "app-102", FullName: "Oleg Morozov", DateOfBirth: date("1973-01-22"), Country: "CY"},
			Document:  UploadedDocument{Type: "passport", Number: "X00991", Expiry: date("2030-06-30"), ExtractedName: "Oleg Morozov", MRZChecksumOK: true, OCRConfidence: 0.95, AddressCountry: "CY"},
			Selfie:    SelfieSession{FaceMatch: 0.96, Liveness: 0.97, DeviceID: "ios-k2", Attempts: 1, PresentationRisk: 0.03},
			Hits:      []ScreeningHit{{List: "OFAC", Score: 0.96, Disposition: "potential_match", Type: "sanctions"}},
		},
	}
}

func buildEvidence(file OnboardingFile, now time.Time) EvidencePackage {
	age := yearsBetween(file.Applicant.DateOfBirth, now)
	nameDistance := normalizedNameDistance(file.Applicant.FullName, file.Document.ExtractedName)
	tampered := len(file.Document.TamperSignals) > 0 || !file.Document.MRZChecksumOK || file.Document.Expiry.Before(now)
	sanctionHit := unresolvedSanctionHit(file.Hits)
	pepHit := unresolvedPEPHit(file.Hits)
	countryRisk := countryRisk(file.Applicant.Country)
	manualReview := tampered || nameDistance > 0.18 || file.Document.OCRConfidence < 0.82 || file.Selfie.FaceMatch < 0.88 || file.Selfie.Liveness < 0.9
	eddRequired := pepHit || countryRisk == "high" || file.Selfie.Attempts >= 3
	autoApprove := file.Document.OCRConfidence >= 0.9 && file.Selfie.FaceMatch >= 0.92 && file.Selfie.Liveness >= 0.95 && !sanctionHit && age >= 18 && !manualReview && !eddRequired

	summary := []string{
		fmt.Sprintf("age=%d", age),
		fmt.Sprintf("ocr=%.2f", file.Document.OCRConfidence),
		fmt.Sprintf("face=%.2f", file.Selfie.FaceMatch),
		fmt.Sprintf("liveness=%.2f", file.Selfie.Liveness),
	}
	if nameDistance > 0 {
		summary = append(summary, fmt.Sprintf("name_distance=%.2f", nameDistance))
	}
	if len(file.Document.TamperSignals) > 0 {
		signals := append([]string(nil), file.Document.TamperSignals...)
		sort.Strings(signals)
		summary = append(summary, "tamper="+strings.Join(signals, ","))
	}
	if sanctionHit {
		summary = append(summary, "screening=potential_match")
	}

	return EvidencePackage{
		Profile: map[string]any{
			"id":                    file.Applicant.ID,
			"age":                   age,
			"country":               file.Applicant.Country,
			"auto_approve_eligible": autoApprove,
		},
		Document: map[string]any{
			"type":                   file.Document.Type,
			"number":                 file.Document.Number,
			"ocr_confidence":         file.Document.OCRConfidence,
			"tampered":               tampered || nameDistance > 0.18,
			"manual_review_required": manualReview,
		},
		Biometric: map[string]any{
			"face_match": file.Selfie.FaceMatch,
			"liveness":   file.Selfie.Liveness,
			"attempts":   file.Selfie.Attempts,
		},
		Screening: map[string]any{
			"sanction_hit": sanctionHit,
			"pep_hit":      pepHit,
			"country_risk": countryRisk,
			"edd_required": eddRequired,
		},
		Summary: summary,
	}
}

func (e EvidencePackage) Facts() map[string]any {
	return map[string]any{
		"profile":   e.Profile,
		"document":  e.Document,
		"biometric": e.Biometric,
		"screening": e.Screening,
	}
}

func executeKYCOutcome(file OnboardingFile, evidence EvidencePackage, effect string) {
	switch effect {
	case "allow":
		accountID := "acct-" + strings.TrimPrefix(file.Applicant.ID, "app-")
		fmt.Printf("  open account %s, attach verified document %s\n", accountID, file.Document.Number)
	case "require_review":
		fmt.Printf("  create KYC case with evidence bundle: %s\n", strings.Join(evidence.Summary, " | "))
	default:
		fmt.Printf("  reject onboarding, preserve screening hits: %s\n", screeningSummary(file.Hits))
	}
}

func unresolvedSanctionHit(hits []ScreeningHit) bool {
	for _, hit := range hits {
		if (hit.Type == "sanctions" || hit.List == "OFAC") && hit.Score >= 0.9 && hit.Disposition != "false_positive" {
			return true
		}
	}
	return false
}

func unresolvedPEPHit(hits []ScreeningHit) bool {
	for _, hit := range hits {
		if hit.Type == "pep" && hit.Score >= 0.85 && hit.Disposition != "false_positive" {
			return true
		}
	}
	return false
}

func countryRisk(country string) string {
	switch country {
	case "IR", "KP", "SY":
		return "blocked"
	case "CY", "AE", "TR":
		return "high"
	default:
		return "standard"
	}
}

func screeningSummary(hits []ScreeningHit) string {
	if len(hits) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(hits))
	for _, hit := range hits {
		parts = append(parts, fmt.Sprintf("%s:%.2f:%s", hit.List, hit.Score, hit.Disposition))
	}
	return strings.Join(parts, ",")
}

func normalizedNameDistance(a, b string) float64 {
	a = strings.ToLower(strings.ReplaceAll(a, " ", ""))
	b = strings.ToLower(strings.ReplaceAll(b, " ", ""))
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	return float64(levenshtein(a, b)) / float64(max(len(a), len(b)))
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func yearsBetween(birth, at time.Time) int {
	years := at.Year() - birth.Year()
	if at.YearDay() < birth.YearDay() {
		years--
	}
	return years
}

func date(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	return int(math.Max(float64(a), float64(b)))
}
