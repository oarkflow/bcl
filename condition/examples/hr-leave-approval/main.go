package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type Employee struct {
	ID               string
	Team             string
	LeaveBalanceDays int
	Manager          string
}

type LeaveRequest struct {
	ID    string
	From  time.Time
	To    time.Time
	Type  string
	Notes string
}

type StaffingCalendar struct {
	Team            string
	TeamSize        int
	AlreadyOut      map[string]int
	BlackoutPeriods []DateRange
}

type DateRange struct {
	From time.Time
	To   time.Time
}

type LeaveCase struct {
	Name     string
	Employee Employee
	Request  LeaveRequest
	Calendar StaffingCalendar
}

type LeaveAnalysis struct {
	Days             int
	CoveragePercent  int
	OverlapsBlackout bool
	Facts            map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "hr-leave-approval", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range leaveInbox() {
		analysis := analyzeLeave(c)
		resp, err := svc.Evaluate(ctx, "hr-leave-approval", condition.EvaluateRequest{Decision: "leave_approval", Input: analysis.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Request.ID, c.Name)
		fmt.Printf("  analysis: days=%d coverage=%d%% blackout=%v balance=%d\n", analysis.Days, analysis.CoveragePercent, analysis.OverlapsBlackout, c.Employee.LeaveBalanceDays)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		processLeave(c, analysis, decision.Effect)
	}
}

func leaveInbox() []LeaveCase {
	return []LeaveCase{
		{Name: "standard vacation", Employee: Employee{ID: "emp-1", Team: "platform", LeaveBalanceDays: 12, Manager: "mgr-1"}, Request: LeaveRequest{ID: "lr-1", From: date("2026-06-03"), To: date("2026-06-05"), Type: "vacation"}, Calendar: platformCalendar()},
		{Name: "holiday blackout", Employee: Employee{ID: "emp-2", Team: "support", LeaveBalanceDays: 20, Manager: "mgr-2"}, Request: LeaveRequest{ID: "lr-2", From: date("2026-11-24"), To: date("2026-11-27"), Type: "vacation"}, Calendar: supportCalendar()},
		{Name: "not enough balance", Employee: Employee{ID: "emp-3", Team: "sales", LeaveBalanceDays: 2, Manager: "mgr-3"}, Request: LeaveRequest{ID: "lr-3", From: date("2026-07-06"), To: date("2026-07-13"), Type: "vacation"}, Calendar: salesCalendar()},
	}
}

func analyzeLeave(c LeaveCase) LeaveAnalysis {
	days := businessDays(c.Request.From, c.Request.To)
	out := c.Calendar.AlreadyOut[c.Request.From.Format("2006-01-02")]
	coverage := int(float64(c.Calendar.TeamSize-out-1) / float64(c.Calendar.TeamSize) * 100)
	blackout := overlaps(c.Request.From, c.Request.To, c.Calendar.BlackoutPeriods)
	return LeaveAnalysis{
		Days:             days,
		CoveragePercent:  coverage,
		OverlapsBlackout: blackout,
		Facts: map[string]any{
			"employee": map[string]any{"id": c.Employee.ID, "leave_balance_days": c.Employee.LeaveBalanceDays},
			"leave":    map[string]any{"id": c.Request.ID, "days": days, "overlaps_blackout": blackout},
			"team":     map[string]any{"id": c.Employee.Team, "coverage_percent": coverage},
		},
	}
}

func processLeave(c LeaveCase, a LeaveAnalysis, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  approve and reserve %d days on %s calendar\n", a.Days, c.Employee.Team)
	case "require_review":
		fmt.Printf("  ask manager %s to review staffing and suggest alternate dates\n", c.Employee.Manager)
	default:
		fmt.Printf("  reject: requested %d days, balance %d\n", a.Days, c.Employee.LeaveBalanceDays)
	}
}

func platformCalendar() StaffingCalendar {
	return StaffingCalendar{Team: "platform", TeamSize: 12, AlreadyOut: map[string]int{"2026-06-03": 1}}
}

func supportCalendar() StaffingCalendar {
	return StaffingCalendar{Team: "support", TeamSize: 10, AlreadyOut: map[string]int{"2026-11-24": 2}, BlackoutPeriods: []DateRange{{From: date("2026-11-20"), To: date("2026-11-30")}}}
}

func salesCalendar() StaffingCalendar {
	return StaffingCalendar{Team: "sales", TeamSize: 8, AlreadyOut: map[string]int{}}
}

func businessDays(from, to time.Time) int {
	days := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			days++
		}
	}
	return days
}

func overlaps(from, to time.Time, ranges []DateRange) bool {
	for _, r := range ranges {
		if !to.Before(r.From) && !from.After(r.To) {
			return true
		}
	}
	return false
}

func date(value string) time.Time {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return t
}
