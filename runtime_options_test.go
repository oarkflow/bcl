package bcl

import (
	"testing"
	"time"
)

func TestEvalExprUsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 5, 26, 12, 34, 56, 789000000, time.UTC)
	got, err := EvalExpr("now()", &EvalOptions{AllowTime: true, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-05-26T12:34:56Z" {
		t.Fatalf("now() = %#v", got)
	}
	got, err = EvalExpr("unix_millis()", &EvalOptions{AllowTime: true, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	if got != fixed.UnixMilli() {
		t.Fatalf("unix_millis() = %#v", got)
	}
}

func TestEvalExprDeniesTimeByDefault(t *testing.T) {
	if _, err := EvalExpr("now()", &EvalOptions{}); err == nil {
		t.Fatal("expected time capability error")
	}
}

func TestCompileUsesInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 5, 26, 12, 34, 56, 0, time.UTC)
	doc, err := Parse([]byte(`value now()`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := Compile(doc, &Options{AllowTime: true, Now: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	if n.Body["value"] != "2026-05-26T12:34:56Z" {
		t.Fatalf("value = %#v", n.Body["value"])
	}
}

func TestOpenDecisionDatasetEnforcesAdapterPolicy(t *testing.T) {
	program := &DecisionProgram{Datasets: map[string]*DatasetDefinition{
		"external": {ID: "external", Source: DatasetSource{Adapter: "file", Config: map[string]any{"path": "missing.json"}}},
	}}
	_, err := OpenDecisionDataset(nil, program, "external", &Options{AllowedDatasetAdapters: []string{"http"}})
	if err == nil {
		t.Fatal("expected adapter policy error")
	}
}
