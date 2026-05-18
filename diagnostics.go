package bcl

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type DiagnosticFormatOptions struct {
	SourceFiles  map[string][]byte
	ContextLines int
}

func FormatDiagnostics(diags []Diagnostic) string {
	return FormatDiagnosticsWithOptions(diags, DiagnosticFormatOptions{})
}

func FormatDiagnosticsWithOptions(diags []Diagnostic, opts DiagnosticFormatOptions) string {
	if len(diags) == 0 {
		return ""
	}
	if opts.ContextLines < 0 {
		opts.ContextLines = 0
	}
	cache := make(map[string][]byte, len(opts.SourceFiles))
	for k, v := range opts.SourceFiles {
		cache[k] = v
	}
	var b strings.Builder
	for i, d := range diags {
		if i > 0 {
			b.WriteByte('\n')
		}
		formatDiagnostic(&b, d, opts.ContextLines, cache)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatDiagnostic(b *strings.Builder, d Diagnostic, contextLines int, cache map[string][]byte) {
	severity := d.Severity
	if severity == "" {
		severity = "error"
	}
	fmt.Fprintf(b, "%s: %s\n", severity, d.Message)
	if d.Span.Start.Line <= 0 {
		if hint := diagnosticHint(d.Message); hint != "" {
			fmt.Fprintf(b, "help: %s\n", hint)
		}
		return
	}
	file := d.Span.File
	if file == "" {
		file = "<input>"
	}
	fmt.Fprintf(b, " --> %s:%d:%d\n", file, d.Span.Start.Line, d.Span.Start.Column)
	source := diagnosticSource(file, cache)
	if len(source) > 0 {
		writeSourceExcerpt(b, source, d.Span, contextLines)
	}
	if hint := diagnosticHint(d.Message); hint != "" {
		fmt.Fprintf(b, "help: %s\n", hint)
	}
}

func diagnosticSource(file string, cache map[string][]byte) []byte {
	if file == "" {
		return nil
	}
	if src, ok := cache[file]; ok {
		return src
	}
	if strings.HasPrefix(file, "<") && strings.HasSuffix(file, ">") {
		return nil
	}
	src, err := os.ReadFile(file)
	if err != nil {
		cache[file] = nil
		return nil
	}
	cache[file] = src
	return src
}

func writeSourceExcerpt(b *strings.Builder, source []byte, sp Span, contextLines int) {
	lines := bytes.Split(source, []byte{'\n'})
	line := sp.Start.Line
	if line <= 0 || line > len(lines) {
		return
	}
	start := line - contextLines
	if start < 1 {
		start = 1
	}
	end := line + contextLines
	if end > len(lines) {
		end = len(lines)
	}
	width := len(strconv.Itoa(end))
	pipePad := strings.Repeat(" ", width)
	fmt.Fprintf(b, "%s |\n", pipePad)
	for n := start; n <= end; n++ {
		text := string(lines[n-1])
		fmt.Fprintf(b, "%*d | %s\n", width, n, text)
		if n == line {
			col := sp.Start.Column
			if col < 1 {
				col = 1
			}
			length := sp.End.Column - sp.Start.Column
			if sp.End.Line != sp.Start.Line || length < 1 {
				length = 1
			}
			if length > 80 {
				length = 80
			}
			fmt.Fprintf(b, "%s | %s%s\n", pipePad, strings.Repeat(" ", col-1), strings.Repeat("^", length))
		}
	}
}

func diagnosticHint(msg string) string {
	switch {
	case strings.Contains(msg, "expected declaration"):
		return "Start statements with a name, for example `field \"value\"` or `block \"id\" { ... }`."
	case strings.Contains(msg, "expected value"):
		return "Add a scalar value, list, object, reference, or function call after the field name."
	case strings.Contains(msg, "unterminated string"):
		return "Close the string with the same quote style that opened it."
	case strings.Contains(msg, "unterminated multiline string"):
		return "Close the multiline string with triple double quotes: `\"\"\"`."
	case strings.Contains(msg, "unterminated raw string"):
		return "Close the raw string with a backtick."
	case strings.Contains(msg, "unterminated heredoc"):
		return "End the heredoc with its marker on a line by itself."
	case strings.Contains(msg, "unterminated block comment"):
		return "Close the block comment with `*/`."
	case strings.Contains(msg, "env function requires AllowEnv"):
		return "Enable environment access with Options.AllowEnv or the CLI `--allow-env` flag."
	case strings.Contains(msg, "required env") && strings.Contains(msg, "is not set"):
		return "Set the environment variable, provide an env file, or use `env(\"KEY\", default)`."
	case strings.Contains(msg, "invalid expression"):
		return "Check operator spelling, balanced brackets, and whether function capabilities are enabled."
	case strings.Contains(msg, "unknown reference"):
		return "Define the referenced block/constant/set, import it, or fix the reference path."
	case strings.Contains(msg, "missing required field"):
		return "Add the field or define a schema default."
	case strings.Contains(msg, "duplicate"):
		return "Rename one declaration or remove the duplicate definition."
	case strings.Contains(msg, "missing lock entry"):
		return "Run `bcl modules lock <file>` and commit the generated lockfile."
	default:
		return ""
	}
}
