package bcl

import "testing"

// Regression tests: `when` and `to` are also expression/block keywords, but
// the parser previously misparsed them when used as plain attribute names.
// `when "expr"` without a following `{` was mistaken for the start of an
// unterminated `when <condition> { ... }` block, and `to` (listed as an
// expression operator word) caused a line like `from "USD" to "NPR"` to be
// swallowed into a single expression, silently dropping the `to` attribute.
func TestWhenAsPlainAttribute(t *testing.T) {
	src := `on "case.completed" { hook "x" when "request.service == 'fast_track'" }`
	if _, err := Parse([]byte(src)); err != nil {
		t.Fatalf("parse error: %v", err)
	}
}

func TestWhenConditionalBlockStillWorks(t *testing.T) {
	src := `row "x" { when { subject.blocked == true } }`
	if _, err := Parse([]byte(src)); err != nil {
		t.Fatalf("unexpected error for when block: %v", err)
	}
}

func TestToAsPlainAttributeSameLine(t *testing.T) {
	src := `from "USD" to "NPR"`
	var out map[string]any
	if err := Unmarshal([]byte(src), &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out["from"] != "USD" {
		t.Fatalf("expected from=USD, got %v", out["from"])
	}
	if out["to"] != "NPR" {
		t.Fatalf("expected to=NPR, got %v (dropped by expression parsing?)", out["to"])
	}
}

func TestArrayIndexingExpression(t *testing.T) {
	v, err := (&ExpressionProgram{Raw: "tail[0]"}).Eval(map[string]any{"tail": []any{"a", "b"}}, nil)
	if err != nil {
		t.Fatalf("index eval error: %v", err)
	}
	if v != "a" {
		t.Fatalf("expected \"a\", got %v", v)
	}
}

// Regression tests: the expression evaluator previously stopped after the
// first complete value and silently ignored anything left over, so `&&`
// and `||` (unrecognized as operators) truncated the expression instead of
// failing, and malformed expressions like `(a`, `a ==`, or `"a" "b"`
// compiled and evaluated without error.
func TestLogicalAndOrOperators(t *testing.T) {
	vars := map[string]any{"a": true, "b": false, "x": 1, "y": 2}
	cases := []struct {
		expr string
		want any
	}{
		{"a && b", false},
		{"b && a", false},
		{"true && false", false},
		{"false || false", false},
		{"x > 0 && y > 5", false},
		{"x > 5 || y > 1", true},
	}
	for _, c := range cases {
		v, err := (&ExpressionProgram{Raw: c.expr}).Eval(vars, nil)
		if err != nil {
			t.Fatalf("%s: eval error: %v", c.expr, err)
		}
		if v != c.want {
			t.Fatalf("%s: got %#v, want %#v", c.expr, v, c.want)
		}
	}
}

func TestMalformedExpressionsAreRejected(t *testing.T) {
	vars := map[string]any{"a": true, "x": 1}
	for _, expr := range []string{"(a", "(x + 1", `"USD" "NPR"`, `"USD" to "NPR"`, "x y", "a ==", "a +", "x >", "(x > 0) )"} {
		p, err := CompileExpression(expr)
		if err != nil {
			continue // compile-time rejection is fine
		}
		if _, err := p.Eval(vars, nil); err == nil {
			t.Fatalf("%q: expected compile or eval error, got none", expr)
		}
	}
}

// The multi-condition decision-table `when { ... }` blocks (e.g. bare
// comparisons on separate lines, implicitly ANDed/ORed by `all`/`any`)
// must keep working once trailing-token rejection is enabled.
func TestMultiConditionWhenBlockStillWorks(t *testing.T) {
	src := `row "x" {
  when {
    subject.blocked == false
    subject.mfa == true
  }
}`
	if _, err := Parse([]byte(src)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// `const` is only treated as a declaration keyword when followed by an
// identifier name; used as a plain attribute key it should bind normally
// instead of erroring with "expected constant name".
func TestConstAsPlainAttributeKey(t *testing.T) {
	var out map[string]any
	if err := Unmarshal([]byte("cfg {\n  const \"x\"\n}\n"), &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	cfg, ok := out["cfg"].(map[string]any)
	if !ok || cfg["const"] != "x" {
		t.Fatalf("expected cfg.const=x, got %#v", out)
	}
}

func TestConstDeclarationStillWorks(t *testing.T) {
	if _, err := Parse([]byte("const pi = 3.14\n")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
