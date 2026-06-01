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

type Account struct {
	ID              string
	Plan            string
	ARR             int
	CSM             string
	EntitledSupport []string
}

type TicketEvent struct {
	At      time.Time
	Message string
	Public  bool
}

type Ticket struct {
	ID          string
	CreatedAt   time.Time
	Severity    int
	Category    string
	Component   string
	ContainsPII bool
	Events      []TicketEvent
}

type QueueState struct {
	Name          string
	OpenTickets   int
	AgentsOnline  int
	SkillCoverage map[string]int
}

type TriageCase struct {
	Name    string
	Account Account
	Ticket  Ticket
	Queues  []QueueState
}

type TriagePlan struct {
	Facts       map[string]any
	Transcript  []string
	SLADeadline time.Time
	BestQueue   string
	AgentSkill  string
	Article     string
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "support-routing", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	now := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	for _, c := range intakeQueue(now) {
		plan := triage(c, now)
		resp, err := svc.Evaluate(ctx, "support-routing", condition.EvaluateRequest{Decision: "support_routing", Input: plan.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Ticket.ID, c.Name)
		fmt.Printf("  normalized: category=%s severity=%d skill=%s sla=%s\n", c.Ticket.Category, c.Ticket.Severity, plan.AgentSkill, plan.SLADeadline.Format(time.RFC822))
		fmt.Printf("  decision: effect=%s queue=%v reason=%s\n", decision.Effect, decision.Attributes["queue"], decision.ReasonCode)
		dispatchTicket(c, plan, decision.Effect)
	}
}

func intakeQueue(now time.Time) []TriageCase {
	return []TriageCase{
		{
			Name:    "starter password reset",
			Account: Account{ID: "cus-1", Plan: "starter", EntitledSupport: []string{"knowledge_base"}},
			Ticket:  Ticket{ID: "t-1", CreatedAt: now.Add(-12 * time.Minute), Severity: 4, Category: "how_to", Component: "login", Events: []TicketEvent{{At: now.Add(-12 * time.Minute), Message: "How do I reset my password?", Public: true}}},
			Queues:  defaultQueues(),
		},
		{
			Name:    "enterprise checkout outage",
			Account: Account{ID: "cus-2", Plan: "enterprise", ARR: 650000, CSM: "Asha", EntitledSupport: []string{"priority", "incident_bridge"}},
			Ticket:  Ticket{ID: "t-2", CreatedAt: now.Add(-8 * time.Minute), Severity: 1, Category: "incident", Component: "payments", Events: []TicketEvent{{At: now.Add(-8 * time.Minute), Message: "Checkout is returning 502s in prod.", Public: true}}},
			Queues:  defaultQueues(),
		},
		{
			Name:    "security disclosure with logs",
			Account: Account{ID: "cus-3", Plan: "pro", ARR: 45000, EntitledSupport: []string{"email"}},
			Ticket:  Ticket{ID: "t-3", CreatedAt: now.Add(-3 * time.Minute), Severity: 2, Category: "security", Component: "api", ContainsPII: true, Events: []TicketEvent{{At: now.Add(-3 * time.Minute), Message: "Attached logs contain customer tokens.", Public: false}}},
			Queues:  defaultQueues(),
		},
	}
}

func triage(c TriageCase, now time.Time) TriagePlan {
	transcript := redactTranscript(c.Ticket.Events)
	skill := requiredSkill(c.Ticket)
	bestQueue := chooseQueue(c.Queues, skill)
	sla := slaDeadline(c, now)
	loadPercent := queueLoadPercent(c.Queues, bestQueue)
	ageMinutes := int(now.Sub(c.Ticket.CreatedAt).Minutes())
	article := ""
	if c.Ticket.Category == "how_to" {
		article = "kb://identity/password-reset"
	}
	return TriagePlan{
		Facts: map[string]any{
			"customer": map[string]any{
				"id":   c.Account.ID,
				"plan": c.Account.Plan,
				"arr":  c.Account.ARR,
			},
			"ticket": map[string]any{
				"id":                    c.Ticket.ID,
				"severity":              c.Ticket.Severity,
				"category":              c.Ticket.Category,
				"component":             c.Ticket.Component,
				"contains_pii":          c.Ticket.ContainsPII,
				"age_minutes":           ageMinutes,
				"sla_breach_risk":       c.Account.Plan == "enterprise" && c.Ticket.Severity <= 2 && ageMinutes >= 20,
				"enterprise_priority":   c.Account.Plan == "enterprise" && c.Ticket.Severity <= 2,
				"self_service_eligible": c.Ticket.Severity >= 4 && c.Account.Plan != "enterprise" && c.Ticket.Category == "how_to",
			},
			"queue": map[string]any{
				"best":              bestQueue,
				"skill":             skill,
				"load_percent":      loadPercent,
				"overflow_required": loadPercent >= 90 && c.Ticket.Severity <= 3,
			},
		},
		Transcript:  transcript,
		SLADeadline: sla,
		BestQueue:   bestQueue,
		AgentSkill:  skill,
		Article:     article,
	}
}

func queueLoadPercent(queues []QueueState, name string) int {
	for _, q := range queues {
		if q.Name != name || q.AgentsOnline == 0 {
			continue
		}
		capacity := q.AgentsOnline * 8
		return min(100, q.OpenTickets*100/capacity)
	}
	return 100
}

func dispatchTicket(c TriageCase, plan TriagePlan, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  deflect with %s and keep ticket in assisted-self-service\n", plan.Article)
	case "require_review":
		if c.Ticket.Category == "security" || c.Ticket.ContainsPII {
			fmt.Printf("  seal attachments, page secops, assign skill=%s\n", plan.AgentSkill)
			return
		}
		fmt.Printf("  open incident bridge for CSM %s, assign queue=%s before %s\n", c.Account.CSM, plan.BestQueue, plan.SLADeadline.Format("15:04"))
	default:
		fmt.Printf("  close ticket after policy review, transcript lines=%d\n", len(plan.Transcript))
	}
}

func defaultQueues() []QueueState {
	return []QueueState{
		{Name: "enterprise", OpenTickets: 18, AgentsOnline: 6, SkillCoverage: map[string]int{"payments": 3, "database": 1}},
		{Name: "secops", OpenTickets: 4, AgentsOnline: 2, SkillCoverage: map[string]int{"security": 2, "api": 1}},
		{Name: "general", OpenTickets: 42, AgentsOnline: 8, SkillCoverage: map[string]int{"login": 5, "billing": 2}},
	}
}

func chooseQueue(queues []QueueState, skill string) string {
	type scored struct {
		name  string
		score float64
	}
	var scores []scored
	for _, q := range queues {
		capacity := float64(q.AgentsOnline*8 - q.OpenTickets)
		scores = append(scores, scored{name: q.Name, score: capacity + float64(q.SkillCoverage[skill]*10)})
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })
	if len(scores) == 0 {
		return "overflow"
	}
	return scores[0].name
}

func requiredSkill(ticket Ticket) string {
	if ticket.Category == "security" {
		return "security"
	}
	if ticket.Component != "" {
		return ticket.Component
	}
	return "general"
}

func slaDeadline(c TriageCase, now time.Time) time.Time {
	switch {
	case c.Account.Plan == "enterprise" && c.Ticket.Severity <= 2:
		return c.Ticket.CreatedAt.Add(30 * time.Minute)
	case c.Ticket.Severity <= 2:
		return c.Ticket.CreatedAt.Add(2 * time.Hour)
	default:
		return now.Add(24 * time.Hour)
	}
}

func redactTranscript(events []TicketEvent) []string {
	lines := make([]string, 0, len(events))
	for _, event := range events {
		msg := event.Message
		if !event.Public {
			msg = "[internal/private content redacted]"
		}
		lines = append(lines, fmt.Sprintf("%s %s", event.At.Format("15:04"), msg))
	}
	return lines
}

func hasEntitlement(account Account, value string) bool {
	for _, entitlement := range account.EntitledSupport {
		if strings.EqualFold(entitlement, value) {
			return true
		}
	}
	return false
}
