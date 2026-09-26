package bcl

import "testing"

// Regression tests: CompileExpression previously only tokenized and cached
// the raw text; actual parsing happened lazily inside Eval, which needs real
// variables to walk the grammar. That meant `(a`, `a ==`, `x y`, and similar
// typos compiled successfully and only failed on first evaluation. A
// checkOnly pass over the same grammar (no variable lookups, no function
// calls, no arithmetic) now runs inside CompileExpression so syntax errors
// surface without any data.
func TestCompileExpressionRejectsUnclosedBrackets(t *testing.T) {
	for _, expr := range []string{"(a", "(x + 1", "x in [1, 2"} {
		if _, err := CompileExpression(expr); err == nil {
			t.Errorf("%q: expected compile error for unclosed bracket", expr)
		}
	}
}

func TestCompileExpressionRejectsDanglingOperators(t *testing.T) {
	for _, expr := range []string{"a ==", "a +", "x >", "'a' == 'b' &&"} {
		if _, err := CompileExpression(expr); err == nil {
			t.Errorf("%q: expected compile error for dangling operator", expr)
		}
	}
}

func TestCompileExpressionRejectsLeftoverTokens(t *testing.T) {
	for _, expr := range []string{"x y", `"USD" "NPR"`, "(x > 0) )"} {
		if _, err := CompileExpression(expr); err == nil {
			t.Errorf("%q: expected compile error for leftover tokens", expr)
		}
	}
}

func TestCompileExpressionChecksPastShortCircuit(t *testing.T) {
	if _, err := CompileExpression("false && (a"); err == nil {
		t.Fatal("expected compile error for syntax error after a short-circuiting operand")
	}
}

func TestCompileExpressionAllowsDataProblems(t *testing.T) {
	for _, expr := range []string{"a.b.c == 1", "len(missing) > 0", "input.amount / 0"} {
		if _, err := CompileExpression(expr); err != nil {
			t.Errorf("%q: expected compile OK (data problems are runtime errors), got %v", expr, err)
		}
	}
	// The data problems still surface at eval time.
	p, _ := CompileExpression("input.amount / 0")
	if _, err := p.Eval(map[string]any{"input": map[string]any{"amount": 10}}, nil); err == nil {
		t.Fatal("expected a runtime error dividing by zero")
	}
}

func TestCompileExpressionFailuresAreNotCached(t *testing.T) {
	raw := "a == "
	if _, err := CompileExpression(raw); err == nil {
		t.Fatal("expected initial compile to fail")
	}
	if _, err := CompileExpression(raw); err == nil {
		t.Fatal("expected repeated compile of the same bad expression to keep failing (not cached as valid)")
	}
}

// Unknown function names are a silent no-op by default (legacy behavior),
// but become an error when the caller opts into EvalOptions.StrictFunctions.
func TestUnknownFunctionDefaultBehaviorUnchanged(t *testing.T) {
	v, err := Eval(`unknown_fn(5)`, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != int64(5) {
		t.Fatalf("expected legacy passthrough behavior, got %#v (%T)", v, v)
	}
}

func TestUnknownFunctionStrictModeErrors(t *testing.T) {
	_, err := EvalExpr(`unknown_fn(5)`, &EvalOptions{StrictFunctions: true})
	if err == nil {
		t.Fatal("expected error for unknown function under StrictFunctions")
	}
}

func TestKnownBuiltinFunctionStillWorks(t *testing.T) {
	v, err := EvalExpr(`upper("a")`, &EvalOptions{StrictFunctions: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != "A" {
		t.Fatalf("expected \"A\", got %#v", v)
	}
}

func TestRegisteredCustomFunctionStillWorksUnderStrictMode(t *testing.T) {
	opts := &EvalOptions{
		StrictFunctions: true,
		Functions: map[string]EvalFunction{
			"double": func(args []any, _ *EvalOptions) (any, error) {
				n, _ := num(args[0])
				return n * 2, nil
			},
		},
	}
	v, err := EvalExpr(`double(21)`, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != float64(42) {
		t.Fatalf("expected 42, got %#v", v)
	}
}

// A block-shaped raw expression (e.g. `include { ... }`) captured after a
// command-statement keyword must not be flagged by the editor's expression
// diagnostics, since it isn't arithmetic/comparison grammar at all.
func TestCommandStatementRawBlockNotFlaggedAsInvalidExpression(t *testing.T) {
	src := `q { include { roles groups attrs } }`
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	diags := Validate(doc, nil)
	for _, d := range diags {
		if d.Severity == "error" {
			t.Fatalf("unexpected error diagnostic: %+v", d)
		}
	}
}
