package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

//go:embed decision.bcl
var policySource string

type PipelineRun struct {
	ID               string
	Service          string
	CommitSHA        string
	Author           string
	Tests            map[string]bool
	SecurityFindings []string
}

type CanarySample struct {
	At         time.Time
	ErrorRate  float64
	LatencyP95 int
}

type ChangeWindow struct {
	RiskScore    int
	FreezeWindow bool
	Approvers    []string
}

type DeploymentCase struct {
	Name   string
	Run    PipelineRun
	Canary []CanarySample
	Change ChangeWindow
}

type GateInput struct {
	TestsPassed        bool
	SecurityScanPassed bool
	ErrorRatePercent   float64
	LatencyP95MS       int
	Facts              map[string]any
}

func main() {
	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "devops-deployment-gate", Version: "1", Source: policySource, RunTests: true, BaseDir: examplebase.Dir()}); err != nil {
		log.Fatal(err)
	}

	for _, c := range releaseTrain() {
		gate := summarizeRelease(c)
		resp, err := svc.Evaluate(ctx, "devops-deployment-gate", condition.EvaluateRequest{Decision: "deployment_gate", Input: gate.Facts})
		if err != nil {
			log.Fatal(err)
		}
		decision := resp.Report.Decision
		fmt.Printf("\n%s %s\n", c.Run.ID, c.Name)
		fmt.Printf("  pipeline: tests=%v security=%v canary_error=%.2f latency=%dms\n", gate.TestsPassed, gate.SecurityScanPassed, gate.ErrorRatePercent, gate.LatencyP95MS)
		fmt.Printf("  decision: effect=%s route=%v reason=%s\n", decision.Effect, decision.Attributes["route"], decision.ReasonCode)
		enforceDeployment(c, gate, decision.Effect)
	}
}

func releaseTrain() []DeploymentCase {
	now := time.Date(2026, 5, 24, 16, 0, 0, 0, time.UTC)
	return []DeploymentCase{
		{Name: "healthy release", Run: PipelineRun{ID: "chg-1", Service: "billing-api", CommitSHA: "a1b2c3", Author: "dev1", Tests: map[string]bool{"unit": true, "integration": true, "e2e": true}}, Canary: canary(now, []float64{0.2, 0.4, 0.3}, []int{210, 230, 220}), Change: ChangeWindow{RiskScore: 20, Approvers: []string{"sre"}}},
		{Name: "canary regression", Run: PipelineRun{ID: "chg-2", Service: "checkout", CommitSHA: "d4e5f6", Author: "dev2", Tests: map[string]bool{"unit": true, "integration": true, "e2e": true}}, Canary: canary(now, []float64{1.9, 2.8, 3.2}, []int{390, 430, 520}), Change: ChangeWindow{RiskScore: 45, Approvers: []string{"sre"}}},
		{Name: "security scan failed", Run: PipelineRun{ID: "chg-3", Service: "auth", CommitSHA: "badc0d", Author: "dev3", Tests: map[string]bool{"unit": true, "integration": true}, SecurityFindings: []string{"critical-cve"}}, Canary: canary(now, []float64{0.4}, []int{240}), Change: ChangeWindow{RiskScore: 30}},
	}
}

func summarizeRelease(c DeploymentCase) GateInput {
	testsPassed := allTestsPassed(c.Run.Tests)
	securityPassed := len(c.Run.SecurityFindings) == 0
	errorRate := maxErrorRate(c.Canary)
	latency := maxLatency(c.Canary)
	return GateInput{
		TestsPassed:        testsPassed,
		SecurityScanPassed: securityPassed,
		ErrorRatePercent:   errorRate,
		LatencyP95MS:       latency,
		Facts: map[string]any{
			"checks": map[string]any{"tests_passed": testsPassed, "security_scan_passed": securityPassed},
			"canary": map[string]any{"error_rate_percent": errorRate, "latency_p95_ms": latency},
			"change": map[string]any{"id": c.Run.ID, "risk_score": c.Change.RiskScore, "freeze_window": c.Change.FreezeWindow},
		},
	}
}

func enforceDeployment(c DeploymentCase, gate GateInput, effect string) {
	switch effect {
	case "allow":
		fmt.Printf("  promote %s@%s and tag release artifact\n", c.Run.Service, c.Run.CommitSHA)
	case "require_review":
		fmt.Printf("  pause rollout at 10%% and attach canary report %.2f/%dms\n", gate.ErrorRatePercent, gate.LatencyP95MS)
	default:
		fmt.Printf("  block deploy, findings=%v missing_tests=%v\n", c.Run.SecurityFindings, failedTests(c.Run.Tests))
	}
}

func allTestsPassed(tests map[string]bool) bool {
	return len(failedTests(tests)) == 0
}

func failedTests(tests map[string]bool) []string {
	var failed []string
	for name, passed := range tests {
		if !passed {
			failed = append(failed, name)
		}
	}
	sort.Strings(failed)
	return failed
}

func canary(now time.Time, errors []float64, latencies []int) []CanarySample {
	out := make([]CanarySample, 0, len(errors))
	for i := range errors {
		out = append(out, CanarySample{At: now.Add(time.Duration(i) * time.Minute), ErrorRate: errors[i], LatencyP95: latencies[i]})
	}
	return out
}

func maxErrorRate(samples []CanarySample) float64 {
	var max float64
	for _, sample := range samples {
		if sample.ErrorRate > max {
			max = sample.ErrorRate
		}
	}
	return max
}

func maxLatency(samples []CanarySample) int {
	var max int
	for _, sample := range samples {
		if sample.LatencyP95 > max {
			max = sample.LatencyP95
		}
	}
	return max
}
