package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/oarkflow/bcl"
)

type Scenario struct {
	Name  string
	Input map[string]any
}

type DecisionOutput struct {
	Decision    bcl.DecisionAnswer `json:"decision"`
	Explanation []string           `json:"explanation,omitempty"`
}

func Program(callerFile string) *bcl.DecisionProgram {
	program, err := bcl.CompileDecisionFile(filepath.Join(filepath.Dir(callerFile), "decision.bcl"), &bcl.Options{AllowTime: true, Verbose: Verbose()})
	if err != nil {
		log.Fatal(err)
	}
	return program
}

func CallerFile() string {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("cannot locate caller")
	}
	return file
}

func Evaluate(program *bcl.DecisionProgram, decision string, scenario Scenario) *bcl.DecisionResult {
	engine := bcl.NewDecisionEngine(program, &bcl.Options{Verbose: Verbose()})
	result, err := engine.Evaluate(decision, scenario.Input)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== %s ==\n", scenario.Name)
	PrintDecision(result)
	return result
}

func Batch(program *bcl.DecisionProgram, decision, dataset string) *bcl.DecisionBatchReport {
	report, err := bcl.EvaluateDecisionDataset(program, decision, dataset, &bcl.Options{Verbose: Verbose()})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== %s batch ==\n", decision)
	if Verbose() {
		Print(report)
	} else {
		Print(report.EffectCounts)
	}
	return report
}

func Gate(program *bcl.DecisionProgram, bundle string) *bcl.DecisionGateReport {
	report, err := bcl.EvaluateDecisionGates(program, bundle, &bcl.Options{Verbose: Verbose()})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("gate %s passed: %v\n", bundle, report.Passed)
	return report
}

func Observation(result *bcl.DecisionResult, input map[string]any) bcl.DecisionObservation {
	obs := bcl.DecisionResultObservation(result, input, nil)
	fmt.Println("\n== observation ==")
	if Verbose() {
		Print(obs)
	} else {
		Print(map[string]any{"decision_id": obs.DecisionID, "effect": obs.Effect, "matched_rules": obs.MatchedRules, "selected_rules": obs.SelectedRules})
	}
	return obs
}

func RankID(result *bcl.DecisionResult) string {
	if result == nil || result.Rank == nil {
		return "none"
	}
	return result.Rank.ID
}

func RankScore(result *bcl.DecisionResult) float64 {
	if result == nil || result.Rank == nil {
		return 0
	}
	return result.Rank.Score
}

func PrintDecision(result *bcl.DecisionResult) {
	if Verbose() {
		Print(result)
		return
	}
	Print(DecisionOutput{Decision: result.Answer(), Explanation: Explanation(result)})
}

func Explanation(result *bcl.DecisionResult) []string {
	if result == nil {
		return nil
	}
	ruleReasons := map[string]string{}
	for _, step := range result.Explain {
		if step.RuleID != "" && step.Reason != "" {
			ruleReasons[step.RuleID] = step.Reason
		}
	}

	var lines []string
	seen := map[string]bool{}
	add := func(line string) {
		if line == "" || seen[line] {
			return
		}
		seen[line] = true
		lines = append(lines, line)
	}

	for _, step := range result.Explain {
		if step.Status != "selected" || step.RuleID == "" {
			continue
		}
		reason := firstNonEmpty(step.Reason, ruleReasons[step.RuleID], result.Reason)
		if step.ReasonCode != "" {
			add(fmt.Sprintf("selected rule %q because %s (reason_code=%s)", step.RuleID, reason, step.ReasonCode))
		} else {
			add(fmt.Sprintf("selected rule %q because %s", step.RuleID, reason))
		}
	}

	for _, step := range result.Explain {
		if step.Status != "score" || step.RuleID == "" || step.ScoreDelta == 0 {
			continue
		}
		reason := firstNonEmpty(ruleReasons[step.RuleID], "score rule matched")
		add(fmt.Sprintf("score %+g from %q because %s", step.ScoreDelta, step.RuleID, reason))
	}

	if result.Rank != nil {
		var matched []string
		var scores []string
		for _, step := range result.Explain {
			switch {
			case step.Status == "matched" && step.Source == "ranking" && step.Candidate == result.Rank.ID && step.RuleID != "":
				matched = append(matched, step.RuleID)
			case step.Status == "candidate_score" && step.Candidate != "" && step.CandidateScore != nil:
				scores = append(scores, fmt.Sprintf("%s=%.3f", step.Candidate, *step.CandidateScore))
			}
		}
		if len(matched) > 0 {
			add(fmt.Sprintf("selected candidate %q because ranking rules matched: %s", result.Rank.ID, strings.Join(matched, ", ")))
		}
		if len(scores) > 0 {
			add(fmt.Sprintf("candidate scores: %s", strings.Join(scores, ", ")))
		}
	}

	if len(lines) == 0 && result.Reason != "" {
		add(result.Reason)
	}
	return lines
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func Print(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}

func Verbose() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BCL_VERBOSE"))) {
	case "1", "true", "yes", "on", "verbose":
		return true
	default:
		return false
	}
}
