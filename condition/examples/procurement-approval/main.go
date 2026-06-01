package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type PurchaseRequisition struct {
	ID         string
	AmountUSD  int
	Category   string
	CostCenter string
	DataAccess []string
}

type VendorDossier struct {
	ID             string
	Approved       bool
	Sanctioned     bool
	SOC2Current    bool
	SecurityRating int
	DPAExecuted    bool
}

type EmployeeBudget struct {
	ID                 string
	Manager            string
	BudgetRemainingUSD int
}

type ProcurementCase struct {
	Name        string
	Requisition PurchaseRequisition
	Vendor      VendorDossier
	Requester   EmployeeBudget
}

type ApprovalPacket struct {
	Approvers []string
	RiskNotes []string
	Facts     map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "procurement-approval", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range intake() {
		packet := buildApprovalPacket(c)
		resp, err := svc.Evaluate(ctx, "procurement-approval", condition.EvaluateRequest{Decision: "procurement_approval", Input: packet.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Requisition.ID, c.Name)
		fmt.Printf("  packet: approvers=%v risk=%v remaining_budget=$%d\n", packet.Approvers, packet.RiskNotes, c.Requester.BudgetRemainingUSD)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processPurchase(c, packet, decision.Effect)
	}
}

func intake() []ProcurementCase {
	return []ProcurementCase{
		{Name: "office supplies", Requisition: PurchaseRequisition{ID: "po-1", AmountUSD: 3200, Category: "office", CostCenter: "G&A"}, Vendor: VendorDossier{ID: "ven-1", Approved: true, SecurityRating: 86, SOC2Current: true, DPAExecuted: true}, Requester: EmployeeBudget{ID: "emp-1", Manager: "mgr-1", BudgetRemainingUSD: 12000}},
		{Name: "security platform", Requisition: PurchaseRequisition{ID: "po-2", AmountUSD: 72000, Category: "security_software", CostCenter: "ENG", DataAccess: []string{"customer_logs", "tokens"}}, Vendor: VendorDossier{ID: "ven-2", Approved: true, SecurityRating: 92, SOC2Current: true, DPAExecuted: true}, Requester: EmployeeBudget{ID: "emp-2", Manager: "mgr-2", BudgetRemainingUSD: 90000}},
		{Name: "blocked vendor", Requisition: PurchaseRequisition{ID: "po-3", AmountUSD: 6000, Category: "consulting", CostCenter: "OPS"}, Vendor: VendorDossier{ID: "ven-3", Sanctioned: true, SecurityRating: 30}, Requester: EmployeeBudget{ID: "emp-3", Manager: "mgr-3", BudgetRemainingUSD: 15000}},
	}
}

func buildApprovalPacket(c ProcurementCase) ApprovalPacket {
	var approvers []string
	var risk []string
	if c.Requisition.AmountUSD > 50000 {
		approvers = append(approvers, "finance-director")
	}
	if c.Requisition.Category == "security_software" || len(c.Requisition.DataAccess) > 0 {
		approvers = append(approvers, "security-review")
	}
	if c.Requisition.AmountUSD > c.Requester.BudgetRemainingUSD {
		risk = append(risk, "budget-exceeded")
	}
	if c.Vendor.Sanctioned {
		risk = append(risk, "sanctions-hit")
	}
	if c.Vendor.SecurityRating < 50 {
		risk = append(risk, "low-security-rating")
	}
	if !c.Vendor.SOC2Current && len(c.Requisition.DataAccess) > 0 {
		risk = append(risk, "missing-soc2")
	}
	if !c.Vendor.DPAExecuted && containsSensitiveData(c.Requisition.DataAccess) {
		risk = append(risk, "missing-dpa")
	}
	sort.Strings(approvers)
	sort.Strings(risk)
	return ApprovalPacket{
		Approvers: approvers,
		RiskNotes: risk,
		Facts: map[string]any{
			"purchase": map[string]any{"id": c.Requisition.ID, "amount_usd": c.Requisition.AmountUSD, "category": c.Requisition.Category},
			"vendor": map[string]any{
				"id":              c.Vendor.ID,
				"approved":        c.Vendor.Approved,
				"sanctioned":      c.Vendor.Sanctioned,
				"security_rating": c.Vendor.SecurityRating,
			},
			"requester": map[string]any{"id": c.Requester.ID, "budget_remaining_usd": c.Requester.BudgetRemainingUSD},
		},
	}
}

func processPurchase(c ProcurementCase, packet ApprovalPacket, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  issue PO and reserve $%d from %s\n", c.Requisition.AmountUSD, c.Requisition.CostCenter)
	case "require_review":
		fmt.Printf("  launch approval workflow: %s\n", strings.Join(packet.Approvers, " -> "))
	default:
		fmt.Printf("  reject vendor %s, notes=%v\n", c.Vendor.ID, packet.RiskNotes)
	}
}

func containsSensitiveData(values []string) bool {
	for _, value := range values {
		if strings.Contains(value, "customer") || strings.Contains(value, "token") {
			return true
		}
	}
	return false
}
