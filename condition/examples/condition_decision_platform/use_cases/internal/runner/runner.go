package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

type Scenario struct {
	Name  string
	Input map[string]any
}

func CallerFile() string {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("cannot locate caller")
	}
	return file
}

func Service(callerFile, name string) *condition.Service {
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example"})
	_, err := svc.Publish(context.Background(), condition.PublishRequest{
		Name:     name,
		Version:  "1",
		Path:     filepath.Join(filepath.Dir(callerFile), "decision.bcl"),
		RunTests: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	return svc
}

func Evaluate(svc *condition.Service, definition, decision string, scenario Scenario) *condition.EvaluateResponse {
	resp, err := svc.Evaluate(context.Background(), definition, condition.EvaluateRequest{
		Decision:        decision,
		Input:           scenario.Input,
		IncludeFeatures: true,
		Counterfactuals: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n== %s ==\n", scenario.Name)
	Print(resp.Report.Decision.Answer())
	return resp
}

func Simulate(svc *condition.Service, definition, decision, candidatePath string, cases []map[string]any) {
	batch := make([]conditionCase, 0, len(cases))
	for i, input := range cases {
		batch = append(batch, conditionCase{ID: fmt.Sprintf("case-%d", i+1), Input: input})
	}
	_ = batch
	resp, err := svc.Simulate(context.Background(), definition, condition.SimulationRequest{
		Decision:      decision,
		CandidatePath: candidatePath,
	})
	if err == nil {
		Print(resp.Compare.EffectTransitions)
	}
}

type conditionCase struct {
	ID    string         `json:"id,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

func Audits(svc *condition.Service) {
	records, err := svc.ListAudits(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("audit records: %d\n", len(records))
	if err := svc.VerifyAudits(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func Print(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}
