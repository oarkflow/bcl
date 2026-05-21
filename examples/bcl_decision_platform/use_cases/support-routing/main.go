package main

import (
	"fmt"

	"github.com/oarkflow/bcl/examples/bcl_decision_platform/use_cases/internal/runner"
)

func main() {
	program := runner.Program(runner.CallerFile())
	scenarios := []runner.Scenario{
		{Name: "abusive billing ticket", Input: map[string]any{"ticket": map[string]any{"severity": "normal", "age_hours": int64(1), "abuse_flag": true, "topic": "billing"}, "customer": map[string]any{"plan": "pro"}}},
		{Name: "critical enterprise database incident", Input: map[string]any{"ticket": map[string]any{"severity": "critical", "age_hours": int64(2), "abuse_flag": false, "topic": "database"}, "customer": map[string]any{"plan": "enterprise"}}},
		{Name: "standard free-plan billing question", Input: map[string]any{"ticket": map[string]any{"severity": "normal", "age_hours": int64(4), "abuse_flag": false, "topic": "billing"}, "customer": map[string]any{"plan": "free"}}},
	}
	for _, scenario := range scenarios {
		result := runner.Evaluate(program, "support_routing", scenario)
		fmt.Printf("support queue=%v ranked_queue=%s score=%.2f route=%v\n", result.Attributes["queue"], runner.RankID(result), runner.RankScore(result), result.Attributes["route"])
	}
	runner.Batch(program, "support_routing", "support_routing_batch")
	runner.Gate(program, "support_routing_bundle")
}
