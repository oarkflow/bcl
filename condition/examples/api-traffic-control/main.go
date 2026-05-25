package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type EdgeRequest struct {
	ID        string
	Path      string
	Method    string
	IP        string
	UserAgent string
	Country   string
	At        time.Time
}

type TenantWindow struct {
	ID                 string
	Plan               string
	QuotaPerMinute     int
	RequestsThisMinute int
}

type EdgeCase struct {
	Name    string
	Request EdgeRequest
	Tenant  TenantWindow
}

type EdgeSignals struct {
	BotScore       int
	CountryBlocked bool
	RateRatio      float64
	BackendPool    string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "api-traffic-control", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range edgeTraffic() {
		signals := inspectEdgeRequest(c)
		resp, err := svc.Evaluate(ctx, "api-traffic-control", condition.EvaluateRequest{Decision: "api_traffic_control", Input: facts(c, signals)})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s %s\n", c.Request.ID, c.Request.Method, c.Request.Path)
		fmt.Printf("  edge: ip=%s bot=%d rate=%.1fx pool=%s\n", anonymizeIP(c.Request.IP), signals.BotScore, signals.RateRatio, signals.BackendPool)
		fmt.Printf("  decision: effect=%s action=%v reason=%s\n", decision.Effect, decision.Attributes["action"], decision.ReasonCode)
		applyGatewayAction(c, signals, decision.Effect)
	}
}

func edgeTraffic() []EdgeCase {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return []EdgeCase{
		{Name: "normal pro traffic", Request: EdgeRequest{ID: "edge-1", Method: "GET", Path: "/v1/invoices", IP: "203.0.113.8", UserAgent: "AcmeSDK/2.1", Country: "US", At: now}, Tenant: TenantWindow{ID: "ten-1", Plan: "pro", RequestsThisMinute: 120, QuotaPerMinute: 1000}},
		{Name: "free tenant burst", Request: EdgeRequest{ID: "edge-2", Method: "POST", Path: "/v1/search", IP: "198.51.100.4", UserAgent: "AcmeSDK/1.9", Country: "US", At: now}, Tenant: TenantWindow{ID: "ten-2", Plan: "free", RequestsThisMinute: 900, QuotaPerMinute: 300}},
		{Name: "credential stuffing bot", Request: EdgeRequest{ID: "edge-3", Method: "POST", Path: "/v1/login", IP: "192.0.2.99", UserAgent: "python-requests/2.31", Country: "RU", At: now}, Tenant: TenantWindow{ID: "ten-3", Plan: "enterprise", RequestsThisMinute: 200, QuotaPerMinute: 5000}},
	}
}

func inspectEdgeRequest(c EdgeCase) EdgeSignals {
	bot := 5
	if strings.Contains(strings.ToLower(c.Request.UserAgent), "python") {
		bot += 60
	}
	if c.Request.Path == "/v1/login" && c.Request.Method == "POST" {
		bot += 30
	}
	ratio := float64(c.Tenant.RequestsThisMinute) / float64(c.Tenant.QuotaPerMinute)
	if ratio > 1 {
		bot += 10
	}
	pool := "primary-us"
	if c.Request.Country != "US" {
		pool = "global-edge"
	}
	return EdgeSignals{BotScore: min(bot, 100), CountryBlocked: c.Request.Country == "RU", RateRatio: ratio, BackendPool: pool}
}

func facts(c EdgeCase, s EdgeSignals) map[string]any {
	return map[string]any{
		"request": map[string]any{
			"id":              c.Request.ID,
			"bot_score":       s.BotScore,
			"country_blocked": s.CountryBlocked,
			"path":            c.Request.Path,
		},
		"tenant": map[string]any{
			"id":                  c.Tenant.ID,
			"plan":                c.Tenant.Plan,
			"requests_per_minute": c.Tenant.RequestsThisMinute,
			"quota_per_minute":    c.Tenant.QuotaPerMinute,
		},
	}
}

func applyGatewayAction(c EdgeCase, s EdgeSignals, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  forward to %s with tenant budget remaining=%d\n", s.BackendPool, c.Tenant.QuotaPerMinute-c.Tenant.RequestsThisMinute)
	case "require_review":
		fmt.Printf("  attach Retry-After=30 and reduce concurrency for tenant %s\n", c.Tenant.ID)
	default:
		fmt.Printf("  drop request before upstream, fingerprint user-agent=%q\n", c.Request.UserAgent)
	}
}

func anonymizeIP(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return "invalid"
	}
	parts := strings.Split(ip.String(), ".")
	if len(parts) == 4 {
		parts[3] = "0"
	}
	return strings.Join(parts, ".")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
