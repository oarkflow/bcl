package bcl

import (
	"os"
	"testing"
)

// benchFixture is a representative production-sized BCL document: a multi-stage
// workflow pipeline with nested pages, forms, lookups, conditions and comments.
const benchFixture = "testdata/large_pipeline.bcl"

func benchSrc(tb testing.TB) []byte {
	tb.Helper()
	b, err := os.ReadFile(benchFixture)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

func BenchmarkPipelineLex(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = lex("bench.bcl", src)
	}
}

func BenchmarkPipelineParse(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseFile("bench.bcl", src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPipelineLint(b *testing.B) {
	src := benchSrc(b)
	doc, err := ParseFile("bench.bcl", src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = Lint(doc, &Options{Strict: true})
	}
}

func BenchmarkPipelineValidate(b *testing.B) {
	src := benchSrc(b)
	doc, err := ParseFile("bench.bcl", src)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = Validate(doc, &Options{Strict: true})
	}
}

func BenchmarkPipelineCompile(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := CompileBytes(src, &Options{AllowTime: true}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPipelineAnalyze(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for b.Loop() {
		AnalyzeFile("bench.bcl", src, &Options{Strict: true, Partial: true, ResolveImports: true})
	}
}

func BenchmarkPipelineFormat(b *testing.B) {
	src := benchSrc(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Format(src); err != nil {
			b.Fatal(err)
		}
	}
}
