package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
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

func TestLSPDoesNotRequireVersionDeclaration(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	if err := os.WriteFile(mainPath, []byte("policy \"p\" {\n  effect allow\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(mainPath))
	for _, d := range a.Diagnostics {
		if d.Message == "missing bcl version declaration" {
			t.Fatalf("bcl version is optional: %#v", a.Diagnostics)
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
	edits := s.formatEdits(pathURI(schemaPath), formattingOptions{TabSize: 2, InsertSpaces: true})
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

func TestLSPReportsMissingImportedFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	if err := os.WriteFile(mainPath, []byte(`bcl {
  version "1.0"
}

import "./missing.schema"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(mainPath))
	var sawMissing bool
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, "included file not found:") && strings.Contains(d.Message, "missing.schema") {
			sawMissing = true
		}
		if strings.Contains(d.Message, "no such file") {
			t.Fatalf("expected friendly missing include diagnostic, got raw diagnostic: %#v", a.Diagnostics)
		}
	}
	if !sawMissing {
		t.Fatalf("expected missing import diagnostic: %#v", a.Diagnostics)
	}
}

func TestLSPReportsMissingModuleSource(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.bcl")
	if err := os.WriteFile(mainPath, []byte(`bcl {
  version "1.0"
  strict true
}

module "demo" {
  source "./missing-module.bcl"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(mainPath))
	var sawMissing bool
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, "module source not found:") && strings.Contains(d.Message, "missing-module.bcl") {
			sawMissing = true
			if d.Span.Start.Line != 7 {
				t.Fatalf("expected diagnostic on source assignment, got span %#v", d.Span)
			}
		}
	}
	if !sawMissing {
		t.Fatalf("expected missing module source diagnostic: %#v", a.Diagnostics)
	}
}

func TestLSPReportsMissingParentModuleSource(t *testing.T) {
	dir := t.TempDir()
	useCaseDir := filepath.Join(dir, "use_cases", "provider-routing")
	if err := os.MkdirAll(useCaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decisionPath := filepath.Join(useCaseDir, "decision.bcl")
	if err := os.WriteFile(decisionPath, []byte(`bcl {
  version "1.0"
  strict true
}

module "provider-routing-condition" {
  source "../module.bcl"
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	a := s.analyzeURI(pathURI(decisionPath))
	var sawMissing bool
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, "module source not found:") && strings.Contains(d.Message, filepath.Join("use_cases", "module.bcl")) {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Fatalf("expected missing parent module source diagnostic: %#v", a.Diagnostics)
	}
}

func TestLSPResolvesImportedDefinitionAndWorkspaceRename(t *testing.T) {
	dir := t.TempDir()
	commonPath := filepath.Join(dir, "common.bcl")
	appPath := filepath.Join(dir, "app.bcl")
	if err := os.WriteFile(commonPath, []byte(`bcl {
  version "1.0"
}

const LIMIT = 10
`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := `import "./common.bcl"

policy "p" {
  max LIMIT
}
`
	if err := os.WriteFile(appPath, []byte(app), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	a := s.analyzeURI(pathURI(appPath))
	target, _, fromRef := s.targetAt(a, pathURI(appPath), position{Line: 3, Character: 7})
	if !fromRef || target != "LIMIT" {
		t.Fatalf("expected LIMIT reference target, got target=%q fromRef=%v", target, fromRef)
	}
	decl, ok := s.declarationFor(a, target)
	if !ok {
		t.Fatal("expected imported declaration")
	}
	if !samePath(decl.Span.File, commonPath) {
		t.Fatalf("definition should point to imported file, got %+v", decl.Span)
	}
	edits := s.referenceEdits("LIMIT")
	if len(edits) != 1 || edits[0].uri != pathURI(appPath) {
		t.Fatalf("expected workspace reference edit in app file, got %+v", edits)
	}
}

func TestLSPCompletionUsesPositionContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "decision.bcl")
	src := `bcl {
  version "1.0"
}

decision_table "fraud_aml" {
  hit_policy first
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	a := s.analyzeURI(pathURI(path))
	items := s.completions(a, pathURI(path), position{Line: 5, Character: len("  hit_policy ")})
	if !lspCompletionLabelsContain(items, "first", "priority", "collect", "unique") {
		t.Fatalf("missing hit_policy completions: %+v", items)
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

func TestLSPAnalyzesImportedConditionFileThroughEntrypoint(t *testing.T) {
	dir := t.TempDir()
	rulesDir := filepath.Join(dir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decisionPath := filepath.Join(dir, "decision.bcl")
	guardPath := filepath.Join(rulesDir, "guard.bcl")
	lifecyclePath := filepath.Join(rulesDir, "lifecycle.bcl")
	if err := os.WriteFile(decisionPath, []byte(`bcl {
  version "1.0"
}

import "./rules/guard.bcl"
import "./rules/lifecycle.bcl"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(guardPath, []byte(`decision_table "pre_request_guard" {
  default allow
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lifecyclePath, []byte(`routes "http" {
  route "login" {
    method "POST"
    pattern "/login"
  }
}

lifecycle "http_request" {
  routes "http"
  phase "pre" {
    decision "pre_request_guard"
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	s := &server{out: &out, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	a := s.analyzeURI(pathURI(lifecyclePath))
	for _, d := range a.Diagnostics {
		if strings.Contains(d.Message, `unknown decision "pre_request_guard"`) {
			t.Fatalf("imported lifecycle should resolve sibling decision through decision.bcl: %#v", a.Diagnostics)
		}
	}
	if _, ok := a.Declarations["decision_table.pre_request_guard"]; !ok {
		t.Fatalf("expected entrypoint analysis to include imported guard declaration")
	}
	published := publishedDiagnosticsByURI(t, out.String())
	if got := published[pathURI(lifecyclePath)]; len(got) != 0 {
		t.Fatalf("lifecycle should receive clear diagnostics, got %#v", got)
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

func lspCompletionLabelsContain(items []any, labels ...string) bool {
	seen := map[string]bool{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if label, ok := m["label"].(string); ok {
			seen[label] = true
		}
	}
	for _, label := range labels {
		if !seen[label] {
			return false
		}
	}
	return true
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func TestLSPFormattingKeepsCommentsAndHonoursEditorIndent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.bcl")
	src := "# platform setup\nserver {\naddress \":8080\"\n  # keep me\nread_timeout \"30s\"\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	edits := s.formatEdits(pathURI(path), formattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("expected one formatting edit, got %#v", edits)
	}
	got := edits[0].(map[string]any)["newText"].(string)
	want := "# platform setup\nserver {\n  address \":8080\"\n  # keep me\n  read_timeout \"30s\"\n}\n"
	if got != want {
		t.Fatalf("formatted commented document = %q, want %q", got, want)
	}

	tabbed := s.formatEdits(pathURI(path), formattingOptions{TabSize: 4, InsertSpaces: false})
	if !strings.Contains(tabbed[0].(map[string]any)["newText"].(string), "\n\taddress") {
		t.Fatalf("editor tab settings ignored: %#v", tabbed[0])
	}
}

func TestLSPRangeFormattingTouchesOnlySelectedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.bcl")
	src := "server {\naddress \":8080\"\nread_timeout \"30s\"\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	edits := s.rangeFormatEdits(pathURI(path), rangeLSP{Start: position{Line: 1}, End: position{Line: 1, Character: 16}}, formattingOptions{TabSize: 2, InsertSpaces: true})
	if len(edits) != 1 {
		t.Fatalf("expected one range edit, got %#v", edits)
	}
	edit := edits[0].(map[string]any)
	if got := edit["newText"].(string); got != "  address \":8080\"\n" {
		t.Fatalf("range edit text = %q", got)
	}
	rng := edit["range"].(rangeLSP)
	if rng.Start.Line != 1 || rng.End.Line != 2 {
		t.Fatalf("range edit range = %#v", rng)
	}
}

func TestLSPAppliesIncrementalDocumentChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.bcl")
	if err := os.WriteFile(path, []byte("server {\n  address \":8080\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	s.handle(rpcMessage{Method: "textDocument/didOpen", Params: json.RawMessage(
		`{"textDocument":{"uri":"` + uri + `","text":"server {\n  address \":8080\"\n}\n"}}`)})

	// Type " read_timeout \"30s\"\n" at the start of line 2, the way an editor
	// sends it: a zero-length range plus the inserted text.
	s.handle(rpcMessage{Method: "textDocument/didChange", Params: json.RawMessage(
		`{"textDocument":{"uri":"` + uri + `"},"contentChanges":[{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":0}},"text":"  read_timeout \"30s\"\n"}]}`)})

	want := "server {\n  address \":8080\"\n  read_timeout \"30s\"\n}\n"
	if got := s.fileText(uri); got != want {
		t.Fatalf("incremental change produced %q, want %q", got, want)
	}

	// A change without a range still replaces the whole document.
	s.handle(rpcMessage{Method: "textDocument/didChange", Params: json.RawMessage(
		`{"textDocument":{"uri":"` + uri + `"},"contentChanges":[{"text":"role \"x\" {}\n"}]}`)})
	if got := s.fileText(uri); got != "role \"x\" {}\n" {
		t.Fatalf("full replacement produced %q", got)
	}
}

func TestApplyContentChangeCountsUTF16Columns(t *testing.T) {
	// "😀" is one rune but two UTF-16 code units, so the closing quote of
	// `title "😀"` sits at column 9 and end-of-line is column 10. Counting runes
	// instead would splice one byte early and corrupt every later edit.
	text := "title \"😀\"\nnext\n"
	if got := applyContentChange(text, contentChange{
		Range: &rangeLSP{Start: position{Line: 0, Character: 10}, End: position{Line: 0, Character: 10}},
		Text:  " x 1",
	}); got != "title \"😀\" x 1\nnext\n" {
		t.Fatalf("splice at end of line produced %q", got)
	}
	if got := applyContentChange(text, contentChange{
		Range: &rangeLSP{Start: position{Line: 0, Character: 7}, End: position{Line: 0, Character: 9}},
		Text:  "ok",
	}); got != "title \"ok\"\nnext\n" {
		t.Fatalf("replacing the emoji produced %q", got)
	}
}

func TestLSPSurvivesPanicInHandler(t *testing.T) {
	s := &server{out: ioDiscard{}, files: map[string]string{}, index: map[string]*bcl.Analysis{}}
	// A request whose params are not the expected shape must not kill the server.
	s.handle(rpcMessage{ID: 1, Method: "textDocument/formatting", Params: json.RawMessage(`{"textDocument":{"uri":12}}`)})
	s.handle(rpcMessage{ID: 2, Method: "textDocument/completion", Params: json.RawMessage(`null`)})
	if s.files == nil {
		t.Fatal("server state lost")
	}
}

func TestReadMessageRecoversFromMalformedBody(t *testing.T) {
	stream := "Content-Length: 7\r\n\r\n{oops!!" +
		"Content-Length: 40\r\n\r\n{\"jsonrpc\":\"2.0\",\"method\":\"initialized\"}\n"
	r := bufio.NewReader(strings.NewReader(stream))
	if _, err := readMessage(r); !errors.Is(err, errBadMessageBody) {
		t.Fatalf("expected a recoverable body error, got %v", err)
	}
	msg, err := readMessage(r)
	if err != nil {
		t.Fatalf("stream lost frame alignment: %v", err)
	}
	if msg.Method != "initialized" {
		t.Fatalf("next message = %#v", msg)
	}
}

// codeActionTitles runs a code-action request for one diagnostic and returns the
// titles offered, so a fix can be tested by what the user would actually see.
func codeActionTitles(t *testing.T, s *server, uri string, diag map[string]any) []string {
	t.Helper()
	params, err := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        diag["range"],
		"context":      map[string]any{"diagnostics": []any{diag}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, action := range s.codeActions(params) {
		titles = append(titles, action.(map[string]any)["title"].(string))
	}
	return titles
}

func hasTitle(titles []string, want string) bool {
	for _, title := range titles {
		if strings.Contains(title, want) {
			return true
		}
	}
	return false
}

func TestQuickFixesAreOfferedByDiagnosticCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.bcl")
	src := "const UNUSED = 1\nblock \"dupe\" {\n  a 1\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{uri: src}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	cases := []struct {
		name string
		diag map[string]any
		want string
	}{
		{
			name: "unused declaration",
			diag: map[string]any{"code": bcl.CodeUnused, "message": `unused constant "UNUSED"`,
				"range": rangeLSP{Start: position{Line: 0}, End: position{Line: 0, Character: 16}}},
			want: "Remove the unused declaration",
		},
		{
			name: "duplicate declaration",
			diag: map[string]any{"code": bcl.CodeDuplicateDeclaration, "message": `duplicate block "dupe"`,
				"range": rangeLSP{Start: position{Line: 1}, End: position{Line: 1, Character: 12}}},
			want: `Rename this declaration to "dupe_2"`,
		},
		{
			name: "missing version",
			diag: map[string]any{"code": bcl.CodeMissingVersion, "message": "missing bcl version declaration",
				"range": rangeLSP{Start: position{Line: 0}, End: position{Line: 0, Character: 1}}},
			want: "Insert BCL version declaration",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if titles := codeActionTitles(t, s, uri, tc.diag); !hasTitle(titles, tc.want) {
				t.Fatalf("expected a %q fix, got %v", tc.want, titles)
			}
		})
	}
}

// A diagnostic from an older server carries no code; the fix must still appear.
func TestQuickFixesFallBackToMessageWithoutACode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.bcl")
	src := "const UNUSED = 1\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{uri: src}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}
	diag := map[string]any{"message": `unused constant "UNUSED"`,
		"range": rangeLSP{Start: position{Line: 0}, End: position{Line: 0, Character: 16}}}
	if titles := codeActionTitles(t, s, uri, diag); !hasTitle(titles, "Remove the unused declaration") {
		t.Fatalf("expected the message fallback to offer the fix, got %v", titles)
	}
}

func TestQuickFixEditsAreWellFormed(t *testing.T) {
	uri := "file:///tmp/x.bcl"
	text := "const UNUSED = 1\nkeep 2\n"

	del := lineDeleteEdit(uri, 0, 0)["changes"].(map[string]any)[uri].([]any)[0].(map[string]any)
	rng := del["range"].(rangeLSP)
	if rng.Start.Line != 0 || rng.End.Line != 1 || del["newText"] != "" {
		t.Fatalf("delete edit should remove the whole line: %#v", del)
	}

	ren := renameInLineEdit(text, uri, 0, "UNUSED", "UNUSED_2")["changes"].(map[string]any)[uri].([]any)[0].(map[string]any)
	renRange := ren["range"].(rangeLSP)
	if renRange.Start.Character != 6 || renRange.End.Character != 12 || ren["newText"] != "UNUSED_2" {
		t.Fatalf("rename edit should replace just the name: %#v", ren)
	}

	// A name the line does not contain must produce no edit rather than a wrong one.
	empty := renameInLineEdit(text, uri, 0, "ABSENT", "X")["changes"].(map[string]any)[uri].([]any)
	if len(empty) != 0 {
		t.Fatalf("expected no edit when the name is not on the line: %#v", empty)
	}
}

func TestFoldingRangesAreZeroBasedAndKinded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.bcl")
	src := "# one\n# two\nserver {\n  a 1\n  b 2\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{uri: src}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	ranges := s.foldingRanges(uri)
	if len(ranges) != 2 {
		t.Fatalf("folding ranges = %#v", ranges)
	}
	comment := ranges[0].(map[string]any)
	if comment["startLine"] != 0 || comment["endLine"] != 1 || comment["kind"] != "comment" {
		t.Fatalf("comment fold = %#v", comment)
	}
	block := ranges[1].(map[string]any)
	if block["startLine"] != 2 || block["endLine"] != 4 {
		t.Fatalf("block fold = %#v", block)
	}
	if _, hasKind := block["kind"]; hasKind {
		t.Fatalf("a block fold should carry no kind: %#v", block)
	}
}

func TestDocumentLinksPointAtExistingImports(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "common.bcl"), []byte("shared 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.bcl")
	src := "import \"./common.bcl\"\nimport \"./missing.bcl\"\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{uri: src}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	links := s.documentLinks(uri)
	if len(links) != 1 {
		t.Fatalf("expected only the import that exists to be linked, got %#v", links)
	}
	link := links[0].(map[string]any)
	if link["target"] != pathURI(filepath.Join(dir, "common.bcl")) {
		t.Fatalf("link target = %#v", link["target"])
	}
}

func TestProjectFormatConfigBeatsEditorSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bcl.FormatConfigName), []byte("indent 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "doc.bcl")
	src := "a {\nb 1\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := pathURI(path)
	s := &server{out: ioDiscard{}, files: map[string]string{uri: src}, index: map[string]*bcl.Analysis{}, rootURI: pathURI(dir)}

	// The editor asks for two spaces; the project says four.
	edits := s.formatEdits(uri, formattingOptions{TabSize: 2, InsertSpaces: true})
	got := edits[0].(map[string]any)["newText"].(string)
	if got != "a {\n    b 1\n}\n" {
		t.Fatalf("project config ignored: %q", got)
	}
}
