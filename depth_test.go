package bcl

import (
	"strings"
	"testing"
	"time"
)

func nestedSource(depth int) []byte {
	return []byte(strings.Repeat("a {\n", depth) + strings.Repeat("}\n", depth))
}

// TestLintScalesLinearlyWithNestingDepth guards a fix for three nested double
// walks that made linting super-linear in nesting depth: 800 levels (under 5 KB
// of source) took 1.2 seconds and 10,000 levels never finished. An editor lints
// on every analysis, so that was a hang reachable from a small file.
func TestLintScalesLinearlyWithNestingDepth(t *testing.T) {
	lintDeep := func(depth int) time.Duration {
		doc, err := Parse(nestedSource(depth))
		if err != nil {
			t.Fatalf("depth %d: %v", depth, err)
		}
		start := time.Now()
		_ = Lint(doc, &Options{Strict: true})
		return time.Since(start)
	}

	// Warm up so the first measurement is not paying for lazily built statics.
	lintDeep(32)

	base := lintDeep(MaxNestingDepth / 8)
	deep := lintDeep(MaxNestingDepth - 1)

	// Eight times the depth for eight times the work would be linear. The bound is
	// loose enough for a noisy machine but still fails hard on a quadratic
	// regression, which would be 64x here.
	if deep > 24*base+10*time.Millisecond {
		t.Fatalf("lint looks super-linear in depth: %d levels took %v, %d levels took %v",
			MaxNestingDepth/8, base, MaxNestingDepth-1, deep)
	}
}

// TestNestingDepthLimitIsADiagnosticNotACrash covers the input that used to end
// the process: the parser recurses per level, and at roughly 200,000 levels Go
// exceeds its 1 GB goroutine stack and dies with an unrecoverable fatal error.
func TestNestingDepthLimitIsADiagnosticNotACrash(t *testing.T) {
	if _, err := Parse(nestedSource(MaxNestingDepth - 1)); err != nil {
		t.Fatalf("%d levels should parse: %v", MaxNestingDepth-1, err)
	}

	_, err := Parse(nestedSource(MaxNestingDepth + 50))
	if err == nil {
		t.Fatalf("expected a diagnostic beyond %d levels", MaxNestingDepth)
	}
	if !strings.Contains(err.Error(), "nesting is deeper") {
		t.Fatalf("unexpected error for an over-deep document: %v", err)
	}

	// The refusal must be prompt, and it must stay prompt for input far past the
	// limit: the parser stops descending rather than walking the rest of the file.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Parse(nestedSource(300000))
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("parsing a 300,000 level document did not return in 20s")
	}
}

// TestNestingLimitAppliesToValuesNotOnlyBlocks covers the other recursive
// descents: lists, calls, object literals and schema fields.
func TestNestingLimitAppliesToValuesNotOnlyBlocks(t *testing.T) {
	cases := map[string]string{
		"list":   "a " + strings.Repeat("[", 5000) + strings.Repeat("]", 5000) + "\n",
		"call":   "a " + strings.Repeat("f(", 5000) + strings.Repeat(")", 5000) + "\n",
		"object": "a = " + strings.Repeat("{ b = ", 5000) + "1" + strings.Repeat(" }", 5000) + "\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _ = Parse([]byte(src))
			}()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatalf("deeply nested %s did not return", name)
			}
		})
	}
}

func BenchmarkLintDeepNesting(b *testing.B) {
	doc, err := Parse(nestedSource(MaxNestingDepth - 1))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = Lint(doc, &Options{Strict: true})
	}
}
