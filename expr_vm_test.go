package bcl

import (
	"reflect"
	"testing"
)

func TestCompileExpressionProgramEval(t *testing.T) {
	vars := map[string]any{
		"subject": map[string]any{
			"roles":  []any{"member", "admin"},
			"status": "active",
		},
		"request": map[string]any{
			"path": "/admin/settings",
		},
	}
	tests := []struct {
		name string
		expr string
		want bool
	}{
		{name: "has any", expr: `subject.roles has_any ["admin", "superadmin"]`, want: true},
		{name: "not equal", expr: `subject.status != "blocked"`, want: true},
		{name: "matches", expr: `request.path matches regex("^/admin/.*")`, want: true},
		{name: "exists", expr: `subject.roles exists`, want: true},
		{name: "empty", expr: `subject.missing empty`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog, err := CompileExpression(tt.expr)
			if err != nil {
				t.Fatalf("compile expression: %v", err)
			}
			got, err := prog.Eval(vars, nil)
			if err != nil {
				t.Fatalf("eval program: %v", err)
			}
			if truthy(got) != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileExpressionCapabilities(t *testing.T) {
	prog, err := CompileExpression(`sha256("secret")`)
	if err != nil {
		t.Fatalf("compile expression: %v", err)
	}
	if _, err := prog.Eval(nil, nil); err == nil {
		t.Fatal("expected hash capability error")
	}
	got, err := prog.Eval(nil, &EvalOptions{AllowHash: true})
	if err != nil {
		t.Fatalf("eval with hash capability: %v", err)
	}
	if got == "" {
		t.Fatal("expected sha256 output")
	}
}

func TestEvalBuiltinStringFunctions(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{`starts_with("gateway", "gate")`, true},
		{`ends_with("gateway", "way")`, true},
		{`trim_prefix("prod-api", "prod-")`, "api"},
		{`trim_suffix("report.json", ".json")`, "report"},
		{`substr("abcdef", 2, 3)`, "cde"},
		{`at("hello", -1)`, "o"},
		{`to_string(42)`, "42"},
		{`repeat("ha", 3)`, "hahaha"},
		{`pad_left("7", 3, "0")`, "007"},
		{`pad_right("go", 4, ".")`, "go.."},
		{`index_of("gateway", "way")`, 4},
		{`last_index_of("bananas", "na")`, 4},
		{`regex_match("svc-42", "^svc-[0-9]+$")`, true},
		{`regex_replace("svc-42", "[0-9]+", "99")`, "svc-99"},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpr(tt.expr, nil)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEvalBuiltinCollectionFunctions(t *testing.T) {
	vars := map[string]any{
		"items": []any{"b", "a", "b", "", nil, "c"},
		"obj":   map[string]any{"name": "api", "tier": "gold", "replicas": 3},
	}
	tests := []struct {
		expr string
		want any
	}{
		{`first(items)`, "b"},
		{`last(items)`, "c"},
		{`slice(items, 1, 3)`, []any{"a", "b"}},
		{`append(["a"], "b", "c")`, []any{"a", "b", "c"}},
		{`prepend(["b"], "a")`, []any{"a", "b"}},
		{`reverse(["a", "b", "c"])`, []any{"c", "b", "a"}},
		{`sort([3, 1, 2])`, []any{int64(1), int64(2), int64(3)}},
		{`unique(items)`, []any{"b", "a", "", nil, "c"}},
		{`compact(items)`, []any{"b", "a", "b", "c"}},
		{`flatten([["a", "b"], "c"])`, []any{"a", "b", "c"}},
		{`union(["a", "b"], ["b", "c"])`, []any{"a", "b", "c"}},
		{`intersect(["a", "b", "c"], ["b", "c", "d"])`, []any{"b", "c"}},
		{`difference(["a", "b", "c"], ["b"])`, []any{"a", "c"}},
		{`without(["a", "b", "c"], "b", "x")`, []any{"a", "c"}},
		{`range(2, 7, 2)`, []any{2, 4, 6}},
		{`keys(obj)`, []any{"name", "replicas", "tier"}},
		{`values(obj)`, []any{"api", 3, "gold"}},
		{`entries(obj)`, []any{
			map[string]any{"key": "name", "value": "api"},
			map[string]any{"key": "replicas", "value": 3},
			map[string]any{"key": "tier", "value": "gold"},
		}},
		{`get(obj, "name")`, "api"},
		{`get(obj, "missing", "fallback")`, "fallback"},
		{`has_key(obj, "tier")`, true},
		{`has_path(obj, "replicas")`, true},
		{`pick(obj, "name", "tier")`, map[string]any{"name": "api", "tier": "gold"}},
		{`omit(obj, "replicas")`, map[string]any{"name": "api", "tier": "gold"}},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpr(tt.expr, &EvalOptions{Variables: vars})
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEvalBuiltinMathAndConversionFunctions(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{`abs(-5)`, float64(5)},
		{`floor(4.8)`, float64(4)},
		{`ceil(4.2)`, float64(5)},
		{`round(4.5)`, float64(5)},
		{`sqrt(9)`, float64(3)},
		{`pow(2, 3)`, float64(8)},
		{`min([3, 1, 2])`, float64(1)},
		{`max(3, 1, 2)`, float64(3)},
		{`sum([1, 2, 3])`, float64(6)},
		{`avg([2, 4, 6])`, float64(4)},
		{`product([2, 3, 4])`, float64(24)},
		{`median([3, 1, 2, 4])`, float64(2.5)},
		{`clamp(15, 0, 10)`, float64(10)},
		{`sign(-8)`, float64(-1)},
		{`log10(100)`, float64(2)},
		{`sin(0)`, float64(0)},
		{`to_int("42")`, 42},
		{`to_float("4.25")`, float64(4.25)},
		{`to_bool("true")`, true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalExpr(tt.expr, nil)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
