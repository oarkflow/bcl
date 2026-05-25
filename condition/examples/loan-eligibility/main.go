package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"math"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type CreditBureauFile struct {
	ApplicantID         string
	Tradelines          []Tradeline
	BankruptcyMonthsAgo int
}

type Tradeline struct {
	Type       string
	BalanceUSD int
	LimitUSD   int
	Delinquent bool
	AgeMonths  int
}

type IncomeVerification struct {
	MonthlyIncomeUSD int
	MonthlyDebtUSD   int
	Verified         bool
}

type LoanRequest struct {
	ID        string
	AmountUSD int
	Purpose   string
}

type CollateralFile struct {
	AssetID      string
	AppraisedUSD int
	LienVerified bool
}

type LoanCase struct {
	Name       string
	Bureau     CreditBureauFile
	Income     IncomeVerification
	Loan       LoanRequest
	Collateral CollateralFile
}

type UnderwritingPacket struct {
	CreditScore         int
	DebtToIncomePercent int
	BankruptcyRecent    bool
	CollateralVerified  bool
	LoanToValuePercent  int
	Facts               map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "loan-eligibility", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range applications() {
		packet := underwrite(c)
		resp, err := svc.Evaluate(ctx, "loan-eligibility", condition.EvaluateRequest{Decision: "loan_eligibility", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Loan.ID, c.Name)
		fmt.Printf("  underwriting: fico=%d dti=%d%% ltv=%d%% collateral=%v\n", packet.CreditScore, packet.DebtToIncomePercent, packet.LoanToValuePercent, packet.CollateralVerified)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processLoan(c, packet, decision.Effect)
	}
}

func applications() []LoanCase {
	return []LoanCase{
		{Name: "prime borrower", Bureau: CreditBureauFile{ApplicantID: "app-1", Tradelines: []Tradeline{{Type: "card", BalanceUSD: 2000, LimitUSD: 20000, AgeMonths: 80}, {Type: "auto", BalanceUSD: 6000, LimitUSD: 12000, AgeMonths: 36}}}, Income: IncomeVerification{MonthlyIncomeUSD: 9000, MonthlyDebtUSD: 2500, Verified: true}, Loan: LoanRequest{ID: "loan-1", AmountUSD: 42000, Purpose: "auto"}, Collateral: CollateralFile{AssetID: "vin-1", AppraisedUSD: 58000, LienVerified: true}},
		{Name: "borderline credit", Bureau: CreditBureauFile{ApplicantID: "app-2", Tradelines: []Tradeline{{Type: "card", BalanceUSD: 9000, LimitUSD: 12000, AgeMonths: 18}}}, Income: IncomeVerification{MonthlyIncomeUSD: 6200, MonthlyDebtUSD: 2100, Verified: true}, Loan: LoanRequest{ID: "loan-2", AmountUSD: 30000, Purpose: "personal"}, Collateral: CollateralFile{AssetID: "cashflow", AppraisedUSD: 35000, LienVerified: true}},
		{Name: "recent bankruptcy", Bureau: CreditBureauFile{ApplicantID: "app-3", BankruptcyMonthsAgo: 8, Tradelines: []Tradeline{{Type: "card", BalanceUSD: 1000, LimitUSD: 8000, AgeMonths: 60}}}, Income: IncomeVerification{MonthlyIncomeUSD: 7000, MonthlyDebtUSD: 2100, Verified: true}, Loan: LoanRequest{ID: "loan-3", AmountUSD: 25000, Purpose: "personal"}, Collateral: CollateralFile{AssetID: "cashflow", AppraisedUSD: 35000, LienVerified: true}},
	}
}

func underwrite(c LoanCase) UnderwritingPacket {
	score := estimateCreditScore(c.Bureau)
	dti := int(math.Round(float64(c.Income.MonthlyDebtUSD) / float64(c.Income.MonthlyIncomeUSD) * 100))
	ltv := int(math.Round(float64(c.Loan.AmountUSD) / float64(c.Collateral.AppraisedUSD) * 100))
	collateralVerified := c.Collateral.LienVerified && ltv <= 90
	bankruptcyRecent := c.Bureau.BankruptcyMonthsAgo > 0 && c.Bureau.BankruptcyMonthsAgo <= 24
	return UnderwritingPacket{
		CreditScore:         score,
		DebtToIncomePercent: dti,
		BankruptcyRecent:    bankruptcyRecent,
		CollateralVerified:  collateralVerified,
		LoanToValuePercent:  ltv,
		Facts: map[string]any{
			"applicant": map[string]any{
				"id":                     c.Bureau.ApplicantID,
				"credit_score":           score,
				"debt_to_income_percent": dti,
				"bankruptcy_recent":      bankruptcyRecent,
			},
			"loan":       map[string]any{"id": c.Loan.ID, "amount_usd": c.Loan.AmountUSD},
			"collateral": map[string]any{"verified": collateralVerified},
		},
	}
}

func processLoan(c LoanCase, p UnderwritingPacket, effect string) {
	switch effect {
	case "allow":
		rate := 6.5 + float64(760-p.CreditScore)/100
		fmt.Printf("  generate approval package with rate %.2f%%\n", rate)
	case "require_review":
		fmt.Printf("  assign to underwriter: dti=%d%% ltv=%d%%\n", p.DebtToIncomePercent, p.LoanToValuePercent)
	default:
		fmt.Printf("  decline and send adverse action reasons: credit policy\n")
	}
}

func estimateCreditScore(file CreditBureauFile) int {
	score := 760
	utilizationPenalty := 0
	for _, t := range file.Tradelines {
		if t.LimitUSD > 0 {
			utilizationPenalty += int(float64(t.BalanceUSD) / float64(t.LimitUSD) * 80)
		}
		if t.Delinquent {
			score -= 90
		}
		if t.AgeMonths < 24 {
			score -= 25
		}
	}
	score -= utilizationPenalty / max(1, len(file.Tradelines))
	if file.BankruptcyMonthsAgo > 0 {
		score -= 60
	}
	return score
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
