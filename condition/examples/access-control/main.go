package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Principal struct {
	ID              string
	Role            string
	TenantID        string
	BreakGlass      bool
	Groups          []string
	LastMFA         time.Time
	PrivilegedUntil time.Time
}

type Resource struct {
	ID          string
	TenantID    string
	Sensitivity string
	OwnerGroup  string
}

type AccessRequest struct {
	ID       string
	Action   string
	Resource Resource
	At       time.Time
}

type DevicePosture struct {
	ID             string
	Trusted        bool
	CountryAllowed bool
	RiskSignals    []string
}

type AccessCase struct {
	Name      string
	Principal Principal
	Request   AccessRequest
	Device    DevicePosture
}

type AccessContext struct {
	SessionRisk int
	Entitlement string
	OfficeHours bool
	Facts       map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "access-control", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range accessRequests() {
		access := buildAccessContext(c)
		resp, err := svc.Evaluate(ctx, "access-control", condition.EvaluateRequest{Decision: "access_control", Input: access.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Request.ID, c.Name)
		fmt.Printf("  entitlement=%s office_hours=%v risk=%d signals=%v\n", access.Entitlement, access.OfficeHours, access.SessionRisk, c.Device.RiskSignals)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		enforceAccess(c, access, decision.Effect)
	}
}

func accessRequests() []AccessCase {
	now := time.Date(2026, 5, 24, 14, 0, 0, 0, time.UTC)
	return []AccessCase{
		{
			Name:      "same-tenant admin opens billing report",
			Principal: Principal{ID: "u-1", Role: "admin", TenantID: "t-100", Groups: []string{"billing-admins"}, LastMFA: now.Add(-20 * time.Minute)},
			Request:   AccessRequest{ID: "req-1", Action: "read_report", At: now, Resource: Resource{ID: "res-1", TenantID: "t-100", Sensitivity: "standard", OwnerGroup: "billing-admins"}},
			Device:    DevicePosture{ID: "mac-1", Trusted: true, CountryAllowed: true},
		},
		{
			Name:      "admin exports from an unmanaged laptop",
			Principal: Principal{ID: "u-2", Role: "admin", TenantID: "t-100", Groups: []string{"billing-admins"}, LastMFA: now.Add(-9 * time.Hour)},
			Request:   AccessRequest{ID: "req-2", Action: "export_data", At: now, Resource: Resource{ID: "res-2", TenantID: "t-100", Sensitivity: "standard", OwnerGroup: "billing-admins"}},
			Device:    DevicePosture{ID: "unknown", Trusted: false, CountryAllowed: true, RiskSignals: []string{"unmanaged_device", "stale_mfa"}},
		},
		{
			Name:      "on-call engineer restores service",
			Principal: Principal{ID: "u-3", Role: "engineer", TenantID: "t-200", BreakGlass: true, Groups: []string{"oncall"}, LastMFA: now.Add(-5 * time.Minute), PrivilegedUntil: now.Add(25 * time.Minute)},
			Request:   AccessRequest{ID: "req-3", Action: "restore_service", At: now, Resource: Resource{ID: "res-3", TenantID: "t-200", Sensitivity: "restricted", OwnerGroup: "oncall"}},
			Device:    DevicePosture{ID: "linux-7", Trusted: true, CountryAllowed: true},
		},
	}
}

func buildAccessContext(c AccessCase) AccessContext {
	risk := 5 + len(c.Device.RiskSignals)*20
	if !c.Device.Trusted {
		risk += 35
	}
	if !c.Device.CountryAllowed {
		risk += 50
	}
	if c.Request.At.Sub(c.Principal.LastMFA) > 4*time.Hour {
		risk += 15
	}
	entitlement := "none"
	if c.Principal.Role == "admin" && c.Principal.TenantID == c.Request.Resource.TenantID && contains(c.Principal.Groups, c.Request.Resource.OwnerGroup) {
		entitlement = "tenant-admin"
	}
	if c.Principal.BreakGlass && c.Request.At.Before(c.Principal.PrivilegedUntil) {
		entitlement = "break-glass"
	}
	return AccessContext{
		SessionRisk: risk,
		Entitlement: entitlement,
		OfficeHours: c.Request.At.Hour() >= 8 && c.Request.At.Hour() <= 18,
		Facts: map[string]any{
			"user": map[string]any{
				"id":          c.Principal.ID,
				"role":        c.Principal.Role,
				"tenant_id":   c.Principal.TenantID,
				"entitlement": entitlement,
				"break_glass": entitlement == "break-glass",
			},
			"access": map[string]any{
				"action":      c.Request.Action,
				"tenant_id":   c.Request.Resource.TenantID,
				"sensitivity": c.Request.Resource.Sensitivity,
			},
			"session": map[string]any{
				"risk_score":              risk,
				"device_trusted":          c.Device.Trusted,
				"country_allowed":         c.Device.CountryAllowed,
				"office_hours":            c.Request.At.Hour() >= 8 && c.Request.At.Hour() <= 18,
				"mfa_age_minutes":         int(c.Request.At.Sub(c.Principal.LastMFA).Minutes()),
				"fresh_mfa_required":      c.Request.Action == "export_data" && c.Request.At.Sub(c.Principal.LastMFA) > 4*time.Hour,
				"off_hours_policy_change": c.Request.Action == "change_policy" && !(c.Request.At.Hour() >= 8 && c.Request.At.Hour() <= 18),
			},
		},
	}
}

func enforceAccess(c AccessCase, access AccessContext, effect string) {
	switch effect {
	case "allow":
		tokenScope := strings.Join([]string{c.Request.Action, c.Request.Resource.ID, access.Entitlement}, ":")
		fmt.Printf("  mint scoped token for %s scope=%s\n", c.Principal.ID, tokenScope)
	case "require_review":
		fmt.Printf("  require fresh MFA and device registration for %s\n", c.Principal.ID)
	default:
		fmt.Printf("  deny request, send audit event for tenant %s\n", c.Request.Resource.TenantID)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
