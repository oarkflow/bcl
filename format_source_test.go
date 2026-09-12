package bcl

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatReindentsAndKeepsComments(t *testing.T) {
	src := []byte(`# platform setup
server {
address ":8080"
    read_timeout    "30s"
      # nested note
max_request_body_bytes 33554432
}
role "applicant" { permissions ["session.view",   "session.submit"] }
workspace "kyc-hq" {
      name "KYC Compliance HQ"
  metadata { region "alpha" }
}
`)
	want := `# platform setup
server {
  address ":8080"
  read_timeout "30s"
  # nested note
  max_request_body_bytes 33554432
}
role "applicant" { permissions ["session.view", "session.submit"] }
workspace "kyc-hq" {
  name "KYC Compliance HQ"
  metadata { region "alpha" }
}
`
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("format output:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatIndentsMultipleGroupsOpenedOnOneLine(t *testing.T) {
	src := []byte(`workspace_workflow "catalog" {
lookup "reason_codes" { options = [
{ value = "a", label = "A" },
{ value = "b", label = "B" },
] }
}
`)
	want := `workspace_workflow "catalog" {
  lookup "reason_codes" { options = [
    { value = "a", label = "A" },
    { value = "b", label = "B" },
  ] }
}
`
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("format output:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatCollapsesBlankLinesAndTrimsBlockPadding(t *testing.T) {
	src := []byte("node \"intake\" {\n\n  type page\n\n\n\n  handler \"x\"\n\n}\n\n\n\nnode \"next\" { type page }\n\n\n")
	want := "node \"intake\" {\n  type page\n\n  handler \"x\"\n}\n\nnode \"next\" { type page }\n"
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("format output:\n%q\nwant:\n%q", out, want)
	}

	kept, err := FormatWithOptions(src, FormatOptions{KeepBlankLines: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(kept, []byte("{\n\n  type page\n\n\n\n")) {
		t.Fatalf("KeepBlankLines dropped blank lines:\n%q", kept)
	}
}

func TestFormatPreservesTokenTextAndOperators(t *testing.T) {
	cases := map[string]string{
		"a { score += 25 }\n":                  "a { score += 25 }\n",
		"optional cases list<object>\n":        "optional cases list<object>\n",
		"x   =   uuid(  )\n":                   "x = uuid()\n",
		"case { m: v, ...rest } => lower(v)\n": "case { m: v, ...rest } => lower(v)\n",
		"pick tail[0]\n":                       "pick tail[0]\n",
		"perms [\"a\",\"b\"]\n":                "perms [\"a\", \"b\"]\n",
		"empty {}\n":                           "empty {}\n",
		"&analytics-db-readable {\n}\n":        "&analytics-db-readable {\n}\n",
		"expr `a != \"b\"`\n":                  "expr `a != \"b\"`\n",
		"nested [[\"read\", \"write\"]]\n":     "nested [[\"read\", \"write\"]]\n",
	}
	for src, want := range cases {
		out, err := Format([]byte(src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if string(out) != want {
			t.Errorf("format(%q) = %q, want %q", src, out, want)
		}
	}
}

func TestFormatKeepsHeredocAndMultilineStringBodies(t *testing.T) {
	src := []byte("job \"x\" {\nscript <<SH\n  keep   this\n    indentation\nSH\nnote \"\"\"\n  raw   block\n\"\"\"\n}\n")
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"  script <<SH\n  keep   this\n    indentation\nSH\n", "  raw   block\n"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("body not preserved verbatim (missing %q):\n%s", want, out)
		}
	}
}

func TestFormatHonoursIndentStyle(t *testing.T) {
	src := []byte("a {\nb {\nc 1\n}\n}\n")
	out, err := FormatWithOptions(src, FormatOptions{IndentWidth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a {\n    b {\n        c 1\n    }\n}\n" {
		t.Fatalf("indent 4 output:\n%q", out)
	}
	out, err = FormatWithOptions(src, FormatOptions{UseTabs: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a {\n\tb {\n\t\tc 1\n\t}\n}\n" {
		t.Fatalf("tab output:\n%q", out)
	}
}

func TestFormatWorksOnDocumentsTheValidatorRejects(t *testing.T) {
	// Duplicate declarations are a lint error, not a lexical one, so formatting
	// must still work: an editor formats files that are not yet valid.
	src := []byte("lookup \"codes\" { source \"a\" }\nlookup \"codes\" { source \"b\" }\n")
	if _, err := Format(src); err != nil {
		t.Fatalf("format rejected a lintable-but-lexable document: %v", err)
	}
	if _, err := Format([]byte("a \"unterminated\n")); err == nil {
		t.Fatal("expected an error for an unterminated string")
	}
}

func TestFormatLinesReportSourceSpans(t *testing.T) {
	src := []byte("a {\n\n  b 1\n}\n")
	lines, err := FormatLines(src, FormatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0].Start != 1 || lines[1].Text != "  b 1" || lines[1].Start != 3 || lines[2].Start != 4 {
		t.Fatalf("line spans = %#v", lines)
	}
}

// TestFormatIsIdempotentAndMeaningPreservingAcrossRepo formats every BCL file in
// the repository and checks that formatting is stable and never changes what the
// document means (compared through the AST-derived canonical form).
func TestFormatIsIdempotentAndMeaningPreservingAcrossRepo(t *testing.T) {
	var files []string
	for _, root := range []string{"example", "examples"} {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil //nolint:nilerr // missing directories are simply skipped
			}
			switch filepath.Ext(path) {
			case ".bcl", ".schema":
				files = append(files, path)
			}
			return nil
		})
	}
	if len(files) < 50 {
		t.Fatalf("expected the repository's BCL corpus, found %d files", len(files))
	}
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		once, err := Format(src)
		if err != nil {
			t.Errorf("%s: format: %v", path, err)
			continue
		}
		twice, err := Format(once)
		if err != nil {
			t.Errorf("%s: reformat: %v", path, err)
			continue
		}
		if !bytes.Equal(once, twice) {
			t.Errorf("%s: format is not idempotent", path)
			continue
		}
		before, err := Canonicalize(src)
		if err != nil {
			continue // unparseable on its own (module fragments); lexical check above still applies
		}
		after, err := Canonicalize(once)
		if err != nil {
			t.Errorf("%s: formatted output no longer parses: %v", path, err)
			continue
		}
		if !bytes.Equal(before, after) {
			t.Errorf("%s: formatting changed the document's meaning", path)
		}
	}
}

func TestFormatKeepsCRLFLineEndings(t *testing.T) {
	out, err := Format([]byte("server {\r\naddress \":8080\"\r\n}\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "server {\r\n  address \":8080\"\r\n}\r\n" {
		t.Fatalf("crlf output = %q", out)
	}
	if bytes.Contains(bytes.ReplaceAll(out, []byte("\r\n"), nil), []byte("\n")) {
		t.Fatalf("mixed line endings: %q", out)
	}
}
