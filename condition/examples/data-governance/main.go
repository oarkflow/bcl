package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Column struct {
	Name string
	Tags []string
}

type Dataset struct {
	ID             string
	Region         string
	LegalHold      bool
	Columns        []Column
	RetentionYears int
}

type DataUser struct {
	ID        string
	Clearance string
	Region    string
	Purpose   string
}

type QueryRequest struct {
	ID      string
	SQL     string
	Columns []string
	Export  bool
}

type DataAccessCase struct {
	Name    string
	Dataset Dataset
	User    DataUser
	Request QueryRequest
}

type GovernanceDecisionInput struct {
	ContainsPII     bool
	MaskingRequired bool
	RegionAllowed   bool
	MaskColumns     []string
	Facts           map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "data-governance", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range queryLog() {
		governance := inspectQuery(c)
		resp, err := svc.Evaluate(ctx, "data-governance", condition.EvaluateRequest{Decision: "data_access", Input: governance.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Request.ID, c.Name)
		fmt.Printf("  query: columns=%v pii=%v mask=%v region_ok=%v\n", c.Request.Columns, governance.ContainsPII, governance.MaskColumns, governance.RegionAllowed)
		fmt.Printf("  decision: effect=%s transform=%v reason=%s\n", decision.Effect, decision.Attributes["transform"], decision.ReasonCode)
		enforceDataPolicy(c, governance, decision.Effect)
	}
}

func queryLog() []DataAccessCase {
	customers := Dataset{ID: "customers", Region: "US", Columns: []Column{{"email", []string{"pii"}}, {"phone", []string{"pii"}}, {"status", []string{"business"}}}, RetentionYears: 7}
	claims := Dataset{ID: "claims", Region: "US", LegalHold: true, Columns: []Column{{"claim_id", nil}, {"ssn", []string{"pii", "restricted"}}}}
	return []DataAccessCase{
		{Name: "restricted caseworker", Dataset: customers, User: DataUser{ID: "u-1", Clearance: "restricted", Region: "US", Purpose: "casework"}, Request: QueryRequest{ID: "dr-1", SQL: "select email,phone,status from customers", Columns: []string{"email", "phone", "status"}}},
		{Name: "analytics export", Dataset: customers, User: DataUser{ID: "u-2", Clearance: "standard", Region: "US", Purpose: "analytics"}, Request: QueryRequest{ID: "dr-2", SQL: "select email,status from customers", Columns: []string{"email", "status"}, Export: true}},
		{Name: "legal hold dataset", Dataset: claims, User: DataUser{ID: "u-3", Clearance: "restricted", Region: "US", Purpose: "casework"}, Request: QueryRequest{ID: "dr-3", SQL: "select ssn from claims", Columns: []string{"ssn"}}},
	}
}

func inspectQuery(c DataAccessCase) GovernanceDecisionInput {
	mask := selectedPIIColumns(c.Dataset, c.Request.Columns)
	containsPII := len(mask) > 0
	maskingRequired := containsPII && (c.User.Clearance != "restricted" || c.User.Purpose == "analytics" || c.Request.Export)
	regionAllowed := c.User.Region == c.Dataset.Region
	sort.Strings(mask)
	return GovernanceDecisionInput{
		ContainsPII:     containsPII,
		MaskingRequired: maskingRequired,
		RegionAllowed:   regionAllowed,
		MaskColumns:     mask,
		Facts: map[string]any{
			"dataset": map[string]any{"id": c.Dataset.ID, "contains_pii": containsPII, "legal_hold": c.Dataset.LegalHold},
			"user":    map[string]any{"id": c.User.ID, "clearance": c.User.Clearance},
			"query":   map[string]any{"id": c.Request.ID, "purpose": c.User.Purpose, "region_allowed": regionAllowed, "masking_required": maskingRequired},
		},
	}
}

func enforceDataPolicy(c DataAccessCase, g GovernanceDecisionInput, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  issue short-lived warehouse token with row filter purpose=%s\n", c.User.Purpose)
	case "require_review":
		fmt.Printf("  rewrite SQL with masking for columns: %s\n", strings.Join(g.MaskColumns, ","))
	default:
		fmt.Printf("  block query and preserve SQL for audit: %q\n", c.Request.SQL)
	}
}

func selectedPIIColumns(dataset Dataset, requested []string) []string {
	requestedSet := map[string]bool{}
	for _, col := range requested {
		requestedSet[col] = true
	}
	var pii []string
	for _, col := range dataset.Columns {
		if requestedSet[col.Name] && hasTag(col.Tags, "pii") {
			pii = append(pii, col.Name)
		}
	}
	return pii
}

func hasTag(tags []string, tag string) bool {
	for _, item := range tags {
		if item == tag {
			return true
		}
	}
	return false
}
