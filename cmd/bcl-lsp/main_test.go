package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oarkflow/bcl"
)

func TestLSPRangeConvertsOneBasedBCLSpans(t *testing.T) {
	got := lspRange(bcl.Span{Start: bcl.Position{Line: 2, Column: 3}, End: bcl.Position{Line: 2, Column: 8}})
	if got.Start.Line != 1 || got.Start.Character != 2 || got.End.Line != 1 || got.End.Character != 7 {
		t.Fatalf("unexpected range: %+v", got)
	}
}

func TestReadMessageParsesContentLengthFrame(t *testing.T) {
	body := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"shutdown\",\"params\":{}}"
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	msg, err := readMessage(bufio.NewReader(strings.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Method != "shutdown" || msg.ID.(float64) != 1 {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestRespondIncludesNullResult(t *testing.T) {
	var out bytes.Buffer
	s := &server{out: &out}
	s.respond(float64(7), nil)

	raw := out.String()
	parts := strings.SplitN(raw, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("invalid frame: %q", raw)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(parts[1]), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["result"]; !ok {
		t.Fatalf("expected explicit null result, got %s", parts[1])
	}
	if payload["error"] != nil {
		t.Fatalf("unexpected error field: %s", parts[1])
	}
}

func TestCodeActionIncludesVersionDeclarationEdit(t *testing.T) {
	uri := "file:///workspace/main.bcl"
	raw := json.RawMessage(fmt.Sprintf(`{
		"textDocument": {"uri": %q},
		"context": {
			"diagnostics": [
				{"message": "missing bcl version declaration", "range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 4}}}
			]
		}
	}`, uri))
	s := &server{}

	actions := s.codeActions(raw)
	if len(actions) == 0 {
		t.Fatal("expected code actions")
	}
	first, ok := actions[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected action type: %T", actions[0])
	}
	if first["title"] != "Insert BCL version declaration" {
		t.Fatalf("expected version action first, got %#v", first["title"])
	}
	edit, ok := first["edit"].(map[string]any)
	if !ok {
		t.Fatalf("expected edit: %#v", first)
	}
	changes := edit["changes"].(map[string]any)
	edits := changes[uri].([]any)
	insert := edits[0].(map[string]any)
	if insert["newText"] != "bcl {\n  version \"1.0\"\n}\n\n" {
		t.Fatalf("unexpected insert text: %#v", insert["newText"])
	}
}

func TestLSPTreatsImportedBCLAsPartialForVersionWarning(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	partialPath := filepath.Join(dir, "integrations.bcl")
	if err := os.WriteFile(mainPath, []byte("import \"./integrations.bcl\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partialPath, []byte("runtime {\n  mode sandboxed\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(partialPath))
	for _, d := range a.Diagnostics {
		if d.Message == "missing bcl version declaration" {
			t.Fatalf("partial file should not warn about version declaration: %#v", a.Diagnostics)
		}
	}
}

func TestLSPSuppressesVersionWarningWhenImportedFileDeclaresVersion(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	commonPath := filepath.Join(dir, "common.bcl")
	if err := os.WriteFile(mainPath, []byte("import \"./common.bcl\"\npolicy \"p\" {\n  effect allow\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commonPath, []byte("bcl {\n  version \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(mainPath))
	for _, d := range a.Diagnostics {
		if d.Message == "missing bcl version declaration" {
			t.Fatalf("main file should accept imported version declaration: %#v", a.Diagnostics)
		}
	}
}

func TestLSPTreatsSiblingBCLBesideMainAsPartial(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	workflowPath := filepath.Join(dir, "workflows.bcl")
	if err := os.WriteFile(mainPath, []byte("bcl {\n  version \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte("pipeline \"loan\" {\n  version \"2026.05\"\n  entrypoint \"start\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(workflowPath))
	for _, d := range a.Diagnostics {
		if d.Message == "missing bcl version declaration" {
			t.Fatalf("sibling partial file should not warn about version declaration: %#v", a.Diagnostics)
		}
	}
}

func TestLSPSupportsSchemaFiles(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "commands.schema")
	if err := os.WriteFile(schemaPath, []byte("schema   Column   {\noptional   type   string\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(schemaPath))
	for _, d := range a.Diagnostics {
		if d.Message == "missing bcl version declaration" {
			t.Fatalf("schema file should not warn about version declaration: %#v", a.Diagnostics)
		}
	}
	edits := s.formatEdits(pathURI(schemaPath))
	if len(edits) != 1 {
		t.Fatalf("expected one formatting edit, got %#v", edits)
	}
	edit := edits[0].(map[string]any)
	if !strings.Contains(edit["newText"].(string), "schema Column {\n  optional type string\n}") {
		t.Fatalf("schema formatting did not use BCL formatter: %#v", edit["newText"])
	}
	if !isBCLSourceFile(schemaPath) {
		t.Fatal(".schema should be treated as a BCL source file")
	}
}

func TestLSPRoutesImportedDiagnosticsToImportedFileURI(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app.bcl")
	commonPath := filepath.Join(dir, "common.bcl")
	if err := os.WriteFile(commonPath, []byte(`bcl {
  version "1.0"
}

const ADMIN_ROLES = ["admin"]
const ADMIN_ROLES = ["superadmin"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(appPath, []byte(`import "./common.bcl"

description null
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	s := &server{out: &out, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	a := s.analyzeURI(pathURI(appPath))
	var sawDuplicate bool
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, `duplicate constant "ADMIN_ROLES"`) && samePath(d.Span.File, commonPath) {
			sawDuplicate = true
		}
	}
	if !sawDuplicate {
		t.Fatalf("expected imported duplicate diagnostic with common.bcl span: %#v", a.Diagnostics)
	}

	published := publishedDiagnosticsByURI(t, out.String())
	if got := published[pathURI(appPath)]; len(got) != 0 {
		t.Fatalf("app.bcl should not receive imported diagnostics: %#v", got)
	}
	var routed bool
	for _, d := range published[pathURI(commonPath)] {
		if strings.Contains(d, `duplicate constant "ADMIN_ROLES"`) {
			routed = true
		}
	}
	if !routed {
		t.Fatalf("expected duplicate diagnostic routed to common.bcl, got %#v", published)
	}
}

func publishedDiagnosticsByURI(t *testing.T, raw string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	r := bufio.NewReader(strings.NewReader(raw))
	for {
		msg, err := readMessage(r)
		if err != nil {
			break
		}
		if msg.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params struct {
			URI         string `json:"uri"`
			Diagnostics []struct {
				Message string `json:"message"`
			} `json:"diagnostics"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			t.Fatal(err)
		}
		for _, d := range params.Diagnostics {
			out[params.URI] = append(out[params.URI], d.Message)
		}
		if _, ok := out[params.URI]; !ok {
			out[params.URI] = nil
		}
	}
	return out
}

func TestLSPResolvesImportedSchemaForDiagnostics(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	schemaPath := filepath.Join(dir, "commands.schema")
	if err := os.WriteFile(schemaPath, []byte(`
schema Migration {
}

schema CreateTable {
}

schema Column {
  optional type string
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(`
bcl {
  version "1.0"
}

import "./commands.schema"

Migration "a" {
  CreateTable "users" {
    Column "id" {
      type integer
    }
  }
}

Migration "b" {
  CreateTable "events" {
    Column "id" {
      type integer
    }
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(mainPath))
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, "duplicate block Column.id") {
			t.Fatalf("LSP should resolve imported schemas before validation: %#v", a.Diagnostics)
		}
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
