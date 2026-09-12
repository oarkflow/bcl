package bcl

import (
	"os"
	"sync"
	"testing"
)

func TestConcurrentCompileIsRaceFree(t *testing.T) {
	src, err := os.ReadFile("testdata/large_pipeline.bcl")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				if _, err := CompileBytes(src, &Options{AllowTime: true}); err != nil {
					t.Error(err)
					return
				}
				if _, err := Format(src); err != nil {
					t.Error(err)
					return
				}
				AnalyzeFile("x.bcl", src, &Options{Strict: true, Partial: true})
			}
		}()
	}
	wg.Wait()
}

func FuzzParseFormatCompile(f *testing.F) {
	seeds := []string{
		// Regressions found by this target. Kept as explicit seeds so they are
		// replayed on every run, not only when the corpus directory survives.
		"decision_table \"d\" {\n  row \"r\" { when { ! ==0 } }\n}\n", // "!" + "==" ran together as "!=="
		"&A. ..", // three "." tokens merged into "..."
		"a 1\n",
		"block \"x\" { y \"z\" }\n",
		"# c\nserver {\n  port 8080\n}\n",
		"schema s {\n  required id string\n}\n",
		"x 1 + 2 * 3\n",
		"decision_table \"d\" {\n  row \"r\" { when { a == 1 } then { decision allow } }\n}\n",
		"m match v {\n  case ANY => 1\n}\n",
		"h <<EOF\nbody\nEOF\n",
	}
	if src, err := os.ReadFile("testdata/large_pipeline.bcl"); err == nil {
		seeds = append(seeds, string(src))
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		// None of these may panic on any input, valid or not.
		out, err := Format([]byte(src))
		if err == nil {
			// Formatting must be stable and must not change the token stream.
			again, err2 := Format(out)
			if err2 != nil {
				t.Fatalf("reformat failed: %v", err2)
			}
			if string(again) != string(out) {
				t.Fatalf("format not idempotent for %q", src)
			}
		}
		doc, err := Parse([]byte(src))
		if err != nil {
			return
		}
		_ = Lint(doc, &Options{Strict: true})
		_ = Validate(doc, &Options{Strict: true})
		_, _ = Compile(doc, &Options{})
		AnalyzeFile("fuzz.bcl", []byte(src), &Options{Strict: true, Partial: true})
	})
}

// FuzzEvalExpr exercises the expression evaluator on arbitrary text. Expressions
// arrive from documents, so nothing here may panic however malformed the input.
func FuzzEvalExpr(f *testing.F) {
	for _, seed := range []string{
		`1 + 2 * 3`,
		`a.b.c == "x"`,
		`upper(trim(name)) starts_with "A"`,
		`items has_any ["a", "b"]`,
		`cond ? "yes" : "no"`,
		`len(split("a,b", ",")) >= 2`,
		`match(v, case({k: 1}, true), false)`,
		`not (x > 1 and y < 2) or z != null`,
		`concat(["a"], ["b"])`,
		`(((((1)))))`,
	} {
		f.Add(seed)
	}
	vars := map[string]any{
		"a":     map[string]any{"b": map[string]any{"c": "x"}},
		"name":  "  ada  ",
		"items": []any{"a", "c"},
		"cond":  true,
		"x":     2,
		"y":     1,
		"z":     "set",
		"v":     map[string]any{"k": 1},
	}
	f.Fuzz(func(t *testing.T, raw string) {
		opts := &EvalOptions{Variables: vars, AllowHash: true, AllowEncoding: true, AllowTime: true}
		// A result or an error are both fine; a panic is not.
		_, _ = EvalExpr(raw, opts)
		if compiled, err := CompileExpression(raw); err == nil {
			_, _ = compiled.Eval(vars, opts)
		}
	})
}

// FuzzDecisionEvaluate compiles arbitrary text as a decision program and
// evaluates every decision it declares. It covers the compile path and the
// evaluator together, which is how a decision document is actually consumed.
func FuzzDecisionEvaluate(f *testing.F) {
	for _, seed := range []string{
		"decision_table \"d\" {\n  default deny\n  row \"r\" { when { a == 1 } then { decision allow } }\n}\n",
		"decision_table \"d\" {\n  hit_policy unique\n  row \"x\" { priority 2 when { a > 0 } then { decision allow } }\n  row \"y\" { when { a > 0 } then { decision deny } }\n}\n",
		"rule_set \"d\" {\n  rule \"s\" { phase score when { a == 1 } then { score += 5 } }\n}\n",
		"decision_schema \"d\" { effects [allow, deny] default deny strategy first_match }\n",
		"module \"m\" {\n  decision_table \"d\" {\n    row \"r\" { when { any { a == 1 b == 2 } } then { outcome { decision allow reason \"ok\" } } }\n  }\n}\n",
	} {
		f.Add(seed)
	}
	input := map[string]any{"a": 1, "b": 2, "nested": map[string]any{"k": "v"}}
	f.Fuzz(func(t *testing.T, src string) {
		doc, err := Parse([]byte(src))
		if err != nil {
			return
		}
		prog, err := CompileDecisionDocument(doc, &Options{})
		if err != nil || prog == nil {
			return
		}
		for id := range prog.Decisions {
			_, _ = EvaluateDecision(prog, id, input, &Options{})
		}
	})
}
