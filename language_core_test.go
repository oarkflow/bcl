package bcl

import (
	"strings"
	"testing"
)

func TestExpressionPrecedenceTernaryAndVariables(t *testing.T) {
	src := []byte(`
name "api"
port 8080
debug true
debug_port port + 1000
level debug ? "debug" : "info"
label name + ":" + debug_port
math 2 + 3 * 4
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body["debug_port"] != float64(9080) {
		t.Fatalf("debug_port = %#v", n.Body["debug_port"])
	}
	if n.Body["level"] != "debug" {
		t.Fatalf("level = %#v", n.Body["level"])
	}
	if n.Body["label"] != "api:9080" {
		t.Fatalf("label = %#v", n.Body["label"])
	}
	if n.Body["math"] != float64(14) {
		t.Fatalf("math = %#v", n.Body["math"])
	}
}

func TestMatchExpressionBlockPatterns(t *testing.T) {
	src := []byte(`
decision match request {
  case { method: "GET", path: p } if p starts_with "/admin" => "admin_read"
  case { method: m, ...rest } => m
  case ANY => "unknown"
}
`)
	n, err := CompileBytes(src, &Options{Context: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	n.Body["request"] = map[string]any{"method": "GET", "path": "/admin/settings"}
	got, err := EvalExpr(`match request {
  case { method: "GET", path: p } if p starts_with "/admin" => "admin_read"
  case { method: m, ...rest } => m
  case ANY => "unknown"
}`, &EvalOptions{Variables: n.Body})
	if err != nil {
		t.Fatal(err)
	}
	if got != "admin_read" {
		t.Fatalf("match = %#v", got)
	}
}

func TestOptionalPatternSentinels(t *testing.T) {
	vars := map[string]any{
		"present": OptionalValue{Present: true, Value: map[string]any{"id": "u1"}},
		"missing": OptionalValue{Present: false},
		"roles":   []any{"admin", "editor"},
	}
	got, err := EvalExpr(`match present {
  case SOME(user) => user.id
  case NONE => "anonymous"
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != "u1" {
		t.Fatalf("SOME match = %#v", got)
	}
	got, err = EvalExpr(`match missing {
  case SOME(user) => user.id
  case NONE => "anonymous"
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != "anonymous" {
		t.Fatalf("NONE match = %#v", got)
	}
	got, err = EvalExpr(`roles matches ALL(ANY)`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("ALL match = %#v", got)
	}
}

func TestFunctionStyleMatch(t *testing.T) {
	got, err := EvalExpr(`match(status, case("active", "ok"), case(ANY, "fallback"))`, &EvalOptions{
		Variables: map[string]any{"status": "blocked"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "fallback" {
		t.Fatalf("function match = %#v", got)
	}
}

func TestValidateDuplicatePatternBindings(t *testing.T) {
	doc, err := Parse([]byte(`
decision match request {
  case { left: x, right: x } => x
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) == 0 {
		t.Fatal("expected duplicate pattern binding diagnostic")
	}
}

func TestPatternMatchingAnyExistsRestAndNot(t *testing.T) {
	vars := map[string]any{
		"request": map[string]any{
			"kind":   "loan",
			"amount": int64(250),
			"tags":   []any{"prime", "manual"},
			"risk":   "low",
		},
		"roles": []any{"viewer", "admin"},
	}
	got, err := EvalExpr(`match request {
  case { kind: "loan", tags: ANY("prime"), risk: not "blocked", ...rest } => rest.amount
  case ANY => 0
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(250) {
		t.Fatalf("object rest/ANY/not match = %#v", got)
	}
	got, err = EvalExpr(`match request.tags {
  case ["prime", ...tail] => tail
  case ANY => []
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	tail, ok := got.([]any)
	if !ok || len(tail) != 1 || tail[0] != "manual" {
		t.Fatalf("list rest = %#v", got)
	}
	got, err = EvalExpr(`match roles {
  case EXISTS("admin") => "privileged"
  case ANY => "ordinary"
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != "privileged" {
		t.Fatalf("EXISTS match = %#v", got)
	}
}

func TestValidateMatchWithoutCatchAllWarning(t *testing.T) {
	doc, err := Parse([]byte(`
decision match request {
  case { kind: "loan" } => "loan"
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	text := FormatDiagnostics(diags)
	if !strings.Contains(text, "match expression has no catch-all case") {
		t.Fatalf("missing catch-all warning: %#v", diags)
	}
}

func TestPatternMissingNullAndAlias(t *testing.T) {
	vars := map[string]any{
		"request": map[string]any{"kind": "loan", "note": nil, "amount": int64(25)},
	}
	got, err := EvalExpr(`match request {
  case { missing: MISSING, note: NULL, amount: _:number as original } => original
  case ANY => 0
}`, &EvalOptions{Variables: vars})
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(25) {
		t.Fatalf("alias match = %#v", got)
	}
}

func TestValidatePatternDiagnostics(t *testing.T) {
	doc, err := Parse([]byte(`
a match status {
  case "active" => true
  case "active" => false
  case ANY => true
  case "blocked" => false
}
b match status {
  case left | right:x => true
  case ANY => false
}
`))
	if err != nil {
		t.Fatal(err)
	}
	text := FormatDiagnostics(Validate(doc, nil))
	for _, want := range []string{"duplicate match literal case", "unreachable match case after catch-all", "pattern alternatives bind different names"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q diagnostic in:\n%s", want, text)
		}
	}
}
