package bcl

import (
	"strings"
	"testing"
)

func TestFormatDiagnosticsWithSourceExcerptAndHint(t *testing.T) {
	src := []byte("server {\n  port\n}\n")
	diags := []Diagnostic{{
		Severity: "error",
		Message:  "expected value",
		Span: Span{
			File:  "config.bcl",
			Start: Position{Line: 2, Column: 7},
			End:   Position{Line: 2, Column: 7},
		},
	}}
	got := FormatDiagnosticsWithOptions(diags, DiagnosticFormatOptions{
		SourceFiles: map[string][]byte{"config.bcl": src},
	})
	for _, want := range []string{
		"error: expected value",
		"--> config.bcl:2:7",
		"2 |   port",
		"^",
		"help:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatted diagnostic missing %q:\n%s", want, got)
		}
	}
}
