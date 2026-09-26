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
