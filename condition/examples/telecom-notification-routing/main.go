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

type RecipientProfile struct {
	ID       string
	MSISDN   string
	Country  string
	OptedOut bool
}

type ProviderRoute struct {
	Name           string
	Countries      []string
	DeliveryScore  float64
	CostPerMessage float64
	SupportsFlash  bool
}

type Notification struct {
	ID                        string
	Priority                  string
	Template                  string
	ContainsProhibitedContent bool
}

type NotificationCase struct {
	Name      string
	Recipient RecipientProfile
	Message   Notification
	Routes    []ProviderRoute
}

type RoutingPlan struct {
	Primary        ProviderRoute
	Fallback       ProviderRoute
	CountryBlocked bool
	Facts          map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "telecom-notification-routing", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range notificationOutbox() {
		plan := chooseRoute(c)
		resp, err := svc.Evaluate(ctx, "telecom-notification-routing", condition.EvaluateRequest{Decision: "notification_routing", Input: plan.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Message.ID, c.Name)
		fmt.Printf("  route: primary=%s fallback=%s country_blocked=%v\n", plan.Primary.Name, plan.Fallback.Name, plan.CountryBlocked)
		fmt.Printf("  decision: effect=%s provider=%v reason=%s\n", decision.Effect, decision.Attributes["provider"], decision.ReasonCode)
		routeNotification(c, plan, decision.Effect)
	}
}

func notificationOutbox() []NotificationCase {
	return []NotificationCase{
		{Name: "healthy sms route", Recipient: RecipientProfile{ID: "rec-1", MSISDN: "+14155550100", Country: "US"}, Message: Notification{ID: "msg-1", Priority: "normal", Template: "receipt"}, Routes: routeCatalog()},
		{Name: "critical low quality", Recipient: RecipientProfile{ID: "rec-2", MSISDN: "+9779800000000", Country: "NP"}, Message: Notification{ID: "msg-2", Priority: "critical", Template: "otp"}, Routes: routeCatalog()},
		{Name: "recipient opted out", Recipient: RecipientProfile{ID: "rec-3", MSISDN: "+14155550200", Country: "US", OptedOut: true}, Message: Notification{ID: "msg-3", Priority: "normal", Template: "marketing"}, Routes: routeCatalog()},
	}
}

func chooseRoute(c NotificationCase) RoutingPlan {
	var candidates []ProviderRoute
	for _, route := range c.Routes {
		if contains(route.Countries, c.Recipient.Country) {
			candidates = append(candidates, route)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return routeScore(candidates[i], c.Message) > routeScore(candidates[j], c.Message)
	})
	primary := candidates[0]
	fallback := candidates[min(1, len(candidates)-1)]
	blocked := c.Recipient.Country == "IR" || c.Recipient.Country == "KP"
	return RoutingPlan{
		Primary:        primary,
		Fallback:       fallback,
		CountryBlocked: blocked,
		Facts: map[string]any{
			"recipient":   map[string]any{"id": c.Recipient.ID, "opted_out": c.Recipient.OptedOut},
			"destination": map[string]any{"country": c.Recipient.Country, "country_blocked": blocked},
			"provider":    map[string]any{"delivery_score": primary.DeliveryScore, "cost_per_message": primary.CostPerMessage},
			"message":     map[string]any{"id": c.Message.ID, "priority": c.Message.Priority, "contains_prohibited_content": c.Message.ContainsProhibitedContent},
		},
	}
}

func routeNotification(c NotificationCase, p RoutingPlan, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  send to %s via %s\n", maskMSISDN(c.Recipient.MSISDN), p.Primary.Name)
	case "require_review":
		fmt.Printf("  use fallback %s with retry policy for priority=%s\n", p.Fallback.Name, c.Message.Priority)
	default:
		fmt.Printf("  suppress %s and record consent/compliance event\n", c.Message.ID)
	}
}

func routeCatalog() []ProviderRoute {
	return []ProviderRoute{
		{Name: "primary-sms", Countries: []string{"US", "NP"}, DeliveryScore: 0.98, CostPerMessage: 0.02, SupportsFlash: true},
		{Name: "regional-np", Countries: []string{"NP"}, DeliveryScore: 0.86, CostPerMessage: 0.04, SupportsFlash: true},
		{Name: "secondary", Countries: []string{"US", "NP"}, DeliveryScore: 0.94, CostPerMessage: 0.03},
	}
}

func routeScore(route ProviderRoute, msg Notification) float64 {
	score := route.DeliveryScore*100 - route.CostPerMessage*100
	if msg.Priority == "critical" && route.SupportsFlash {
		score += 5
	}
	if msg.Priority == "critical" && route.Name == "primary-sms" {
		score -= 20
	}
	return score
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maskMSISDN(value string) string {
	if len(value) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}
