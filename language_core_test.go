package bcl

import "testing"

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
