package bcl

import "testing"

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
