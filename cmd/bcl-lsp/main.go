package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/oarkflow/bcl"
)

type server struct {
	in                    *bufio.Reader
	out                   io.Writer
	mu                    sync.Mutex
	files                 map[string]string
	index                 map[string]*bcl.Analysis
	recent                []string
	rootURI               string
	customHoverDetailMode bool
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rangeLSP struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

func main() {
	s := &server{in: bufio.NewReader(os.Stdin), out: os.Stdout, files: map[string]string{}, index: map[string]*bcl.Analysis{}}
	if err := s.serve(); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (s *server) serve() error {
	for {
		msg, err := readMessage(s.in)
		if err != nil {
			return err
		}
		if msg.Method == "" {
			continue
		}
		s.handle(msg)
	}
}

func (s *server) handle(msg rpcMessage) {
	switch msg.Method {
	case "initialize":
		var p struct {
			RootURI               string `json:"rootUri"`
			InitializationOptions struct {
				UseCustomHoverDetail bool `json:"useCustomHoverDetail"`
			} `json:"initializationOptions"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.rootURI = p.RootURI
		s.customHoverDetailMode = p.InitializationOptions.UseCustomHoverDetail
		s.respond(msg.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           2,
				"completionProvider":         map[string]any{"resolveProvider": false, "triggerCharacters": []string{".", "\"", " "}},
				"hoverProvider":              true,
				"definitionProvider":         true,
				"referencesProvider":         true,
				"renameProvider":             true,
				"documentSymbolProvider":     true,
				"workspaceSymbolProvider":    true,
				"documentFormattingProvider": true,
				"codeActionProvider":         true,
				"semanticTokensProvider": map[string]any{
					"legend": map[string]any{
						"tokenTypes":     semanticTokenTypes,
						"tokenModifiers": []string{"declaration", "deprecated", "readonly"},
					},
					"full": true,
				},
			},
			"serverInfo": map[string]any{"name": "bcl-lsp"},
		})
	case "initialized":
		s.indexWorkspace()
	case "shutdown":
		s.respond(msg.ID, nil)
	case "exit":
		os.Exit(0)
	case "textDocument/didOpen":
		var p struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.setFile(p.TextDocument.URI, p.TextDocument.Text)
	case "textDocument/didChange":
		var p struct {
			TextDocument   textDocumentIdentifier `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if len(p.ContentChanges) > 0 {
			s.setFile(p.TextDocument.URI, p.ContentChanges[len(p.ContentChanges)-1].Text)
		}
	case "textDocument/didSave":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Text         string                 `json:"text,omitempty"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Text != "" {
			s.setFile(p.TextDocument.URI, p.Text)
		} else {
			s.analyzeURI(p.TextDocument.URI)
		}
	case "textDocument/completion":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Position     position               `json:"position"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		a := s.analyzeURI(p.TextDocument.URI)
		s.respond(msg.ID, s.completions(a, p.TextDocument.URI, p.Position))
	case "textDocument/hover":
		if s.customHoverDetailMode {
			s.respond(msg.ID, nil)
			return
		}
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Position     position               `json:"position"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		a := s.analyzeURI(p.TextDocument.URI)
		if sym, ok := bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1); ok {
			s.touch(sym.Name)
			s.respond(msg.ID, map[string]any{"contents": map[string]any{"kind": "markdown", "value": bcl.RichHoverMarkdown(a, sym, []byte(s.fileText(p.TextDocument.URI)))}, "range": lspRange(sym.SelectionSpan)})
			return
		}
		s.respond(msg.ID, nil)
	case "bcl/hoverDetail":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Position     position               `json:"position"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		a := s.analyzeURI(p.TextDocument.URI)
		if sym, ok := bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1); ok {
			s.touch(sym.Name)
			s.respond(msg.ID, map[string]any{"contents": bcl.RichHoverMarkdown(a, sym, []byte(s.fileText(p.TextDocument.URI))), "range": lspRange(sym.SelectionSpan)})
			return
		}
		s.respond(msg.ID, nil)
	case "textDocument/definition":
		s.definitionLike(msg, false)
	case "textDocument/references":
		s.definitionLike(msg, true)
	case "textDocument/documentSymbol":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, documentSymbols(s.analyzeURI(p.TextDocument.URI).Symbols))
	case "workspace/symbol":
		var p struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.workspaceSymbols(p.Query))
	case "textDocument/formatting":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.formatEdits(p.TextDocument.URI))
	case "textDocument/rename":
		s.rename(msg)
	case "textDocument/codeAction":
		s.respond(msg.ID, s.codeActions(msg.Params))
	case "textDocument/semanticTokens/full":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, map[string]any{"data": s.semanticTokens(p.TextDocument.URI)})
	case "bcl/recentSymbols":
		s.respond(msg.ID, s.recentSymbols())
	default:
		if msg.ID != nil {
			s.respondError(msg.ID, -32601, "method not found")
		}
	}
}

func (s *server) setFile(uri, text string) {
	s.mu.Lock()
	s.files[uri] = text
	s.mu.Unlock()
	s.analyzeURI(uri)
	s.reanalyzeDependents(uri)
}

func (s *server) analyzeURI(uri string) *bcl.Analysis {
	s.mu.Lock()
	text, ok := s.files[uri]
	s.mu.Unlock()
	path := uriPath(uri)
	analysisPath := path
	analysisURI := uri
	if owner := s.ownerEntrypoint(path); owner != "" && !samePath(owner, path) {
		analysisPath = owner
		analysisURI = pathURI(owner)
		if b, err := os.ReadFile(owner); err == nil {
			text = string(b)
			ok = true
		}
	}
	if !ok {
		if b, err := os.ReadFile(analysisPath); err == nil {
			text = string(b)
		}
	}
	partial := s.suppressVersionWarning(analysisPath)
	if !samePath(analysisPath, path) {
		partial = true
	}
	a, diags := bcl.AnalyzeFile(analysisPath, []byte(text), &bcl.Options{Strict: true, Partial: partial, ResolveImports: true, BaseDir: filepath.Dir(analysisPath)})
	includeDiags := missingIncludeDiagnostics(analysisPath, []byte(text))
	if len(includeDiags) > 0 {
		diags = replaceRawMissingFileDiagnostics(diags, includeDiags)
		diags = append(diags, includeDiags...)
		a.Diagnostics = diags
	}
	s.mu.Lock()
	s.index[uri] = a
	if analysisURI != uri {
		s.index[analysisURI] = a
	}
	s.mu.Unlock()
	s.publishDiagnostics(analysisURI, diags, append([]string{path}, sourceGraphPaths(analysisPath)...))
	return a
}

func (s *server) indexWorkspace() {
	root := uriPath(s.rootURI)
	if root == "" {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBCLSourceFile(path) {
			return nil
		}
		s.analyzeURI(pathURI(path))
		return nil
	})
}

func (s *server) publishDiagnostics(uri string, diags []bcl.Diagnostic, clearPaths []string) {
	byURI := map[string][]bcl.Diagnostic{uri: nil}
	for _, path := range clearPaths {
		if path != "" {
			byURI[pathURI(path)] = nil
		}
	}
	for _, d := range diags {
		target := diagnosticURI(uri, d)
		byURI[target] = append(byURI[target], d)
	}
	for target, targetDiags := range byURI {
		items := make([]any, 0, len(targetDiags))
		for _, d := range targetDiags {
			items = append(items, map[string]any{
				"range":    lspRange(d.Span),
				"severity": severity(d.Severity),
				"source":   "bcl",
				"message":  d.Message,
			})
		}
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": target, "diagnostics": items})
	}
}

func (s *server) ownerEntrypoint(path string) string {
	if path == "" || !isBCLSourceFile(path) {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if filepath.Base(path) == "decision.bcl" || filepath.Base(path) == "main.bcl" {
		return path
	}
	root := uriPath(s.rootURI)
	if root == "" {
		root = filepath.Dir(path)
	}
	if absRoot, err := filepath.Abs(root); err == nil {
		root = absRoot
	}
	for dir := filepath.Dir(path); dir != "" && pathWithinOrSame(dir, root); dir = filepath.Dir(dir) {
		for _, name := range []string{"decision.bcl", "main.bcl"} {
			candidate := filepath.Join(dir, name)
			if samePath(candidate, path) {
				return candidate
			}
			if sourceGraphContains(candidate, path) {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return ""
}

func diagnosticURI(baseURI string, d bcl.Diagnostic) string {
	if d.Span.File == "" {
		return baseURI
	}
	basePath := uriPath(baseURI)
	if samePath(basePath, d.Span.File) {
		return baseURI
	}
	return pathURI(d.Span.File)
}

func (s *server) completions(a *bcl.Analysis, uri string, pos position) []any {
	rank := map[string]int{}
	for i, name := range s.recent {
		rank[name] = len(s.recent) - i
	}
	comps, _ := bcl.CompletionsAt(a, []byte(s.fileText(uri)), pos.Line+1, pos.Character+1)
	comps = append(comps, s.workspaceCompletions()...)
	sort.SliceStable(comps, func(i, j int) bool {
		if rank[comps[i].Label] != rank[comps[j].Label] {
			return rank[comps[i].Label] > rank[comps[j].Label]
		}
		return comps[i].Label < comps[j].Label
	})
	out := make([]any, 0, len(comps))
	for _, c := range comps {
		insert := c.InsertText
		if insert == "" {
			insert = c.Label
		}
		out = append(out, map[string]any{"label": c.Label, "kind": completionKind(c.Kind), "detail": c.Detail, "documentation": c.Documentation, "insertText": insert, "insertTextFormat": 2})
	}
	return out
}

func (s *server) definitionLike(msg rpcMessage, refs bool) {
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Position     position               `json:"position"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	a := s.analyzeURI(p.TextDocument.URI)
	target, refSpan, fromRef := s.targetAt(a, p.TextDocument.URI, p.Position)
	sym, ok := s.declarationFor(a, target)
	if !ok {
		var local bcl.LanguageSymbol
		local, ok = bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1)
		if ok {
			sym = local
			target = local.Name
			refSpan = local.SelectionSpan
		}
	}
	if !ok {
		s.respond(msg.ID, nil)
		return
	}
	s.touch(sym.Name)
	if !refs {
		s.respond(msg.ID, locationForSymbol(p.TextDocument.URI, sym))
		return
	}
	locs := []any{locationForSymbol(p.TextDocument.URI, sym)}
	if fromRef && refSpan.Start.Line > 0 {
		locs = append(locs, map[string]any{"uri": p.TextDocument.URI, "range": lspRange(refSpan)})
	}
	locs = append(locs, s.referenceLocations(target)...)
	s.respond(msg.ID, locs)
}

func (s *server) rename(msg rpcMessage) {
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Position     position               `json:"position"`
		NewName      string                 `json:"newName"`
	}
	_ = json.Unmarshal(msg.Params, &p)
	a := s.analyzeURI(p.TextDocument.URI)
	target, _, _ := s.targetAt(a, p.TextDocument.URI, p.Position)
	sym, ok := s.declarationFor(a, target)
	if !ok {
		var local bcl.LanguageSymbol
		local, ok = bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1)
		if ok {
			sym = local
			target = local.Name
		}
	}
	if !ok {
		s.respond(msg.ID, nil)
		return
	}
	changes := map[string][]any{}
	declURI := symbolURI(p.TextDocument.URI, sym)
	changes[declURI] = append(changes[declURI], map[string]any{"range": lspRange(sym.SelectionSpan), "newText": p.NewName})
	for _, loc := range s.referenceEdits(target) {
		changes[loc.uri] = append(changes[loc.uri], map[string]any{"range": lspRange(loc.span), "newText": p.NewName})
	}
	out := map[string]any{}
	for uri, edits := range changes {
		out[uri] = edits
	}
	s.respond(msg.ID, map[string]any{"changes": out})
}

func (s *server) formatEdits(uri string) []any {
	text := s.fileText(uri)
	out, err := bcl.Format([]byte(text))
	if err != nil {
		return nil
	}
	lines := strings.Count(text, "\n") + 1
	return []any{map[string]any{"range": map[string]any{"start": position{}, "end": position{Line: lines, Character: 0}}, "newText": string(out)}}
}

func (s *server) semanticTokens(uri string) []int {
	text := s.fileText(uri)
	toks, _ := bcl.TokenizeFile(uriPath(uri), []byte(text))
	sort.Slice(toks, func(i, j int) bool {
		if toks[i].Span.Start.Line == toks[j].Span.Start.Line {
			return toks[i].Span.Start.Column < toks[j].Span.Start.Column
		}
		return toks[i].Span.Start.Line < toks[j].Span.Start.Line
	})
	var data []int
	prevLine, prevCol := 0, 0
	for _, tok := range toks {
		line := tok.Span.Start.Line - 1
		col := tok.Span.Start.Column - 1
		length := tok.Span.End.Offset - tok.Span.Start.Offset
		if length <= 0 {
			length = len(tok.Text)
		}
		dLine := line - prevLine
		dCol := col
		if dLine == 0 {
			dCol = col - prevCol
		}
		data = append(data, dLine, dCol, length, semanticTypeIndex(tok.Type), 0)
		prevLine, prevCol = line, col
	}
	return data
}

func (s *server) fileText(uri string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if text, ok := s.files[uri]; ok {
		return text
	}
	b, _ := os.ReadFile(uriPath(uri))
	return string(b)
}

func (s *server) workspaceSymbols(query string) []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	for uri, a := range s.index {
		for _, sym := range flatten(a.Symbols) {
			if query == "" || strings.Contains(strings.ToLower(sym.Name), strings.ToLower(query)) {
				out = append(out, map[string]any{"name": sym.Name, "kind": symbolKind(sym.Kind), "containerName": sym.Container, "location": map[string]any{"uri": uri, "range": lspRange(sym.SelectionSpan)}})
			}
		}
	}
	return out
}

func (s *server) workspaceCompletions() []bcl.Completion {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	var out []bcl.Completion
	for _, a := range s.index {
		for _, decl := range a.Index.Declarations {
			if decl.CanonicalName == "" || seen[decl.CanonicalName] {
				continue
			}
			seen[decl.CanonicalName] = true
			out = append(out, bcl.Completion{Label: decl.CanonicalName, Kind: string(decl.Kind), Detail: decl.Container})
		}
	}
	return out
}

type editLocation struct {
	uri  string
	span bcl.Span
}

func (s *server) targetAt(a *bcl.Analysis, uri string, pos position) (string, bcl.Span, bool) {
	line, col := pos.Line+1, pos.Character+1
	for _, r := range a.References {
		if lspContains(r.Span, line, col) {
			return r.Name, r.Span, true
		}
	}
	if sym, ok := bcl.SymbolAt(a, line, col); ok {
		return sym.Name, sym.SelectionSpan, false
	}
	return "", bcl.Span{}, false
}

func (s *server) declarationFor(a *bcl.Analysis, target string) (bcl.LanguageSymbol, bool) {
	if target == "" {
		return bcl.LanguageSymbol{}, false
	}
	if decl, ok := a.Declarations[target]; ok {
		return decl, true
	}
	if decl, ok := a.Declarations[canonicalTarget(target)]; ok {
		return decl, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, indexed := range s.index {
		if decl, ok := indexed.Declarations[target]; ok {
			return decl, true
		}
		if decl, ok := indexed.Declarations[canonicalTarget(target)]; ok {
			return decl, true
		}
	}
	return bcl.LanguageSymbol{}, false
}

func (s *server) referenceLocations(target string) []any {
	if target == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	for uri, a := range s.index {
		for _, r := range a.References {
			if referenceMatchesTarget(r.Name, target) {
				out = append(out, map[string]any{"uri": uriForSpan(uri, r.Span), "range": lspRange(r.Span)})
			}
		}
	}
	return out
}

func (s *server) referenceEdits(target string) []editLocation {
	if target == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []editLocation
	for uri, a := range s.index {
		for _, r := range a.References {
			if referenceMatchesTarget(r.Name, target) && (r.Kind == "reference" || r.Kind == "set") {
				out = append(out, editLocation{uri: uriForSpan(uri, r.Span), span: r.Span})
			}
		}
	}
	return out
}

func referenceMatchesTarget(ref, target string) bool {
	return ref == target || canonicalTarget(ref) == target || ref == canonicalTarget(target)
}

func canonicalTarget(s string) string {
	if strings.Count(s, ".") == 0 {
		return s
	}
	parts := strings.Split(s, ".")
	return parts[len(parts)-1]
}

func locationForSymbol(defaultURI string, sym bcl.LanguageSymbol) map[string]any {
	return map[string]any{"uri": symbolURI(defaultURI, sym), "range": lspRange(symbolDefinitionSpan(sym))}
}

func symbolDefinitionSpan(sym bcl.LanguageSymbol) bcl.Span {
	if sym.SelectionSpan.Start.Line > 0 {
		// References returned by SymbolAt carry the reference selection, but the declaration
		// span remains on Span. Prefer the declaration span when it points at another file.
		if sym.Span.File != "" && sym.Span.File != sym.SelectionSpan.File {
			return sym.Span
		}
		return sym.SelectionSpan
	}
	return sym.Span
}

func symbolURI(defaultURI string, sym bcl.LanguageSymbol) string {
	return uriForSpan(defaultURI, sym.Span)
}

func uriForSpan(defaultURI string, sp bcl.Span) string {
	if sp.File == "" {
		return defaultURI
	}
	return pathURI(sp.File)
}

func lspContains(sp bcl.Span, line, col int) bool {
	if sp.Start.Line == 0 {
		return false
	}
	if line < sp.Start.Line || line > sp.End.Line {
		return false
	}
	if line == sp.Start.Line && col < sp.Start.Column {
		return false
	}
	if line == sp.End.Line && col > sp.End.Column {
		return false
	}
	return true
}

func (s *server) reanalyzeDependents(changedURI string) {
	changed := uriPath(changedURI)
	s.mu.Lock()
	uris := make([]string, 0, len(s.index))
	for uri := range s.index {
		if uri != changedURI {
			uris = append(uris, uri)
		}
	}
	s.mu.Unlock()
	for _, uri := range uris {
		path := uriPath(uri)
		doc, err := bcl.ParsePath(path)
		if err != nil {
			continue
		}
		base := filepath.Dir(path)
		for _, dep := range append(importedPaths(doc.Items, base), moduleSourceDirs(doc.Items, base)...) {
			if samePath(changed, dep) || pathWithin(changed, dep) {
				s.analyzeURI(uri)
				break
			}
		}
	}
}

func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathWithinOrSame(path, dir string) bool {
	if samePath(path, dir) {
		return true
	}
	return pathWithin(path, dir)
}

func (s *server) codeActions(raw json.RawMessage) []any {
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Context      struct {
			Diagnostics []struct {
				Message string   `json:"message"`
				Range   rangeLSP `json:"range"`
			} `json:"diagnostics"`
		} `json:"context"`
	}
	_ = json.Unmarshal(raw, &p)
	actions := []any{
		map[string]any{"title": "Format BCL document", "kind": "source.format", "command": map[string]any{"title": "Format BCL document", "command": "editor.action.formatDocument"}},
		map[string]any{"title": "Restart BCL language server", "kind": "quickfix", "command": map[string]any{"title": "Restart BCL language server", "command": "bcl.restartLanguageServer"}},
	}
	appendLine := strings.Count(s.fileText(p.TextDocument.URI), "\n") + 1
	for _, d := range p.Context.Diagnostics {
		if strings.Contains(d.Message, "missing required input") {
			if input := quotedTail(d.Message); input != "" {
				actions = append(actions, map[string]any{
					"title":       fmt.Sprintf("Add module input %q", input),
					"kind":        "quickfix",
					"diagnostics": []any{d},
					"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.End.Line+1, fmt.Sprintf("  inputs {\n    %s value\n  }\n", input)),
				})
			}
		}
		if strings.Contains(d.Message, "unknown reference") {
			if ref := quotedTail(d.Message); ref != "" {
				actions = append(actions, createReferenceAction(p.TextDocument.URI, ref, d, appendLine))
			}
		}
		if strings.Contains(d.Message, "unknown action") {
			if name := quotedTail(d.Message); name != "" {
				actions = append(actions, appendBlockAction(p.TextDocument.URI, fmt.Sprintf("Create action %q", name), fmt.Sprintf("\naction %q {\n  type http\n}\n", name), d, appendLine))
			}
		}
		if strings.Contains(d.Message, "unknown reason code") {
			if code := quotedTail(d.Message); code != "" {
				actions = append(actions, appendBlockAction(p.TextDocument.URI, fmt.Sprintf("Create reason code %q", code), fmt.Sprintf("\nreason_code_catalog \"default\" {\n  code %q { description \"description\" }\n}\n", code), d, appendLine))
			}
		}
		if strings.Contains(d.Message, "missing required field") {
			if field := quotedTail(d.Message); field != "" {
				actions = append(actions, map[string]any{
					"title":       fmt.Sprintf("Insert required field %q", field),
					"kind":        "quickfix",
					"diagnostics": []any{d},
					"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.End.Line+1, fmt.Sprintf("  %s value\n", field)),
				})
			}
		}
		if strings.Contains(d.Message, "sensitive") {
			actions = append(actions, map[string]any{
				"title":       "Wrap value with sensitive(...)",
				"kind":        "quickfix",
				"diagnostics": []any{d},
				"edit":        wrapRangeEdit(p.TextDocument.URI, d.Range, "sensitive(", ")"),
			})
		}
	}
	if hasDiagnostic(p.Context.Diagnostics, "missing bcl version declaration") {
		edit := map[string]any{
			"changes": map[string]any{
				p.TextDocument.URI: []any{
					map[string]any{
						"range":   rangeLSP{Start: position{Line: 0, Character: 0}, End: position{Line: 0, Character: 0}},
						"newText": "bcl {\n  version \"1.0\"\n}\n\n",
					},
				},
			},
		}
		actions = append([]any{map[string]any{
			"title":       "Insert BCL version declaration",
			"kind":        "quickfix",
			"diagnostics": p.Context.Diagnostics,
			"edit":        edit,
		}}, actions...)
	}
	return actions
}

func createReferenceAction(uri, ref string, diag any, line int) map[string]any {
	typ, id, ok := strings.Cut(ref, ".")
	if !ok || typ == "" || id == "" {
		return appendBlockAction(uri, fmt.Sprintf("Create constant %q", ref), fmt.Sprintf("\nconst %s = value\n", ref), diag, line)
	}
	return appendBlockAction(uri, fmt.Sprintf("Create %s %q", typ, id), fmt.Sprintf("\n%s %q {\n  field value\n}\n", typ, id), diag, line)
}

func appendBlockAction(uri, title, text string, diag any, line int) map[string]any {
	return map[string]any{
		"title":       title,
		"kind":        "quickfix",
		"diagnostics": []any{diag},
		"edit": map[string]any{"changes": map[string]any{
			uri: []any{map[string]any{"range": rangeLSP{Start: position{Line: line, Character: 0}, End: position{Line: line, Character: 0}}, "newText": text}},
		}},
	}
}

func lineInsertEdit(uri string, line int, text string) map[string]any {
	return map[string]any{"changes": map[string]any{
		uri: []any{map[string]any{"range": rangeLSP{Start: position{Line: line, Character: 0}, End: position{Line: line, Character: 0}}, "newText": text}},
	}}
}

func wrapRangeEdit(uri string, r rangeLSP, prefix, suffix string) map[string]any {
	return map[string]any{"changes": map[string]any{
		uri: []any{
			map[string]any{"range": rangeLSP{Start: r.Start, End: r.Start}, "newText": prefix},
			map[string]any{"range": rangeLSP{Start: r.End, End: r.End}, "newText": suffix},
		},
	}}
}

func quotedTail(s string) string {
	last := ""
	for {
		start := strings.Index(s, `"`)
		if start < 0 {
			return last
		}
		s = s[start+1:]
		end := strings.Index(s, `"`)
		if end < 0 {
			return last
		}
		last = s[:end]
		s = s[end+1:]
	}
}

func hasDiagnostic(diags []struct {
	Message string   `json:"message"`
	Range   rangeLSP `json:"range"`
}, message string) bool {
	for _, d := range diags {
		if d.Message == message {
			return true
		}
	}
	return false
}

func (s *server) touch(name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := []string{name}
	for _, old := range s.recent {
		if old != name {
			next = append(next, old)
		}
		if len(next) >= 30 {
			break
		}
	}
	s.recent = next
}

func (s *server) recentSymbols() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]any, 0, len(s.recent))
	for _, name := range s.recent {
		out = append(out, map[string]any{"label": name})
	}
	return out
}

func documentSymbols(symbols []bcl.LanguageSymbol) []any {
	out := make([]any, 0, len(symbols))
	for _, s := range symbols {
		out = append(out, map[string]any{"name": s.Name, "detail": s.Detail, "kind": symbolKind(s.Kind), "range": lspRange(s.Span), "selectionRange": lspRange(s.SelectionSpan), "children": documentSymbols(s.Children)})
	}
	return out
}

func flatten(symbols []bcl.LanguageSymbol) []bcl.LanguageSymbol {
	var out []bcl.LanguageSymbol
	var walk func([]bcl.LanguageSymbol)
	walk = func(xs []bcl.LanguageSymbol) {
		for _, s := range xs {
			out = append(out, s)
			walk(s.Children)
		}
	}
	walk(symbols)
	return out
}

func readMessage(r *bufio.Reader) (rpcMessage, error) {
	var length int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil {
				return rpcMessage{}, err
			}
			length = n
		}
	}
	if length <= 0 {
		return rpcMessage{}, fmt.Errorf("missing content length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return rpcMessage{}, err
	}
	var msg rpcMessage
	err := json.Unmarshal(body, &msg)
	return msg, err
}

func (s *server) respond(id any, result any) {
	s.writePayload(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *server) respondError(id any, code int, message string) {
	s.writePayload(map[string]any{"jsonrpc": "2.0", "id": id, "error": &rpcError{Code: code, Message: message}})
}

func (s *server) notify(method string, params any) {
	s.writePayload(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *server) writePayload(payload any) {
	b, _ := json.Marshal(payload)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(b))
	buf.Write(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.out.Write(buf.Bytes())
}

func lspRange(sp bcl.Span) rangeLSP {
	return rangeLSP{
		Start: position{Line: max(0, sp.Start.Line-1), Character: max(0, sp.Start.Column-1)},
		End:   position{Line: max(0, sp.End.Line-1), Character: max(0, sp.End.Column-1)},
	}
}

func uriPath(uri string) string {
	if uri == "" {
		return ""
	}
	u, err := url.Parse(uri)
	if err == nil && u.Scheme == "file" {
		return u.Path
	}
	return uri
}

func pathURI(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}

func (s *server) suppressVersionWarning(path string) bool {
	if filepath.Ext(path) == ".schema" {
		return true
	}
	return s.isPartialBCLFile(path) || s.importsVersionDeclaration(path)
}

func (s *server) isPartialBCLFile(path string) bool {
	if path == "" || !isBCLSourceFile(path) {
		return false
	}
	root := uriPath(s.rootURI)
	if root == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if filepath.Base(abs) != "main.bcl" {
		if _, err := os.Stat(filepath.Join(filepath.Dir(abs), "main.bcl")); err == nil {
			return true
		}
	}
	var partial bool
	_ = filepath.WalkDir(root, func(candidate string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isBCLSourceFile(candidate) || partial {
			return nil
		}
		doc, err := bcl.ParsePath(candidate)
		if err != nil {
			return nil
		}
		base := filepath.Dir(candidate)
		for _, imported := range importedPaths(doc.Items, base) {
			if samePath(abs, imported) {
				partial = true
				return nil
			}
		}
		for _, dir := range moduleSourceDirs(doc.Items, base) {
			rel, err := filepath.Rel(dir, abs)
			if err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				partial = true
				return nil
			}
		}
		return nil
	})
	return partial
}

func (s *server) importsVersionDeclaration(path string) bool {
	if path == "" || !isBCLSourceFile(path) {
		return false
	}
	doc, err := bcl.ParsePath(path)
	if err != nil {
		return false
	}
	base := filepath.Dir(path)
	for _, imported := range importedPaths(doc.Items, base) {
		importedDoc, err := bcl.ParsePath(imported)
		if err != nil {
			continue
		}
		if hasBCLVersionBlock(importedDoc.Items) {
			return true
		}
	}
	return false
}

func isBCLSourceFile(path string) bool {
	switch filepath.Ext(path) {
	case ".bcl", ".schema":
		return true
	default:
		return false
	}
}

func hasBCLVersionBlock(nodes []bcl.Node) bool {
	for _, n := range nodes {
		b, ok := n.(*bcl.Block)
		if !ok || b.Type != "bcl" {
			continue
		}
		if blockStringAssignment(b, "version") != "" {
			return true
		}
	}
	return false
}

func importedPaths(nodes []bcl.Node, base string) []string {
	var out []string
	for _, n := range nodes {
		switch x := n.(type) {
		case *bcl.ImportDecl:
			pattern := x.Path
			if !filepath.IsAbs(pattern) {
				pattern = filepath.Join(base, pattern)
			}
			if matches, err := filepath.Glob(pattern); err == nil && len(matches) > 0 {
				out = append(out, matches...)
			} else {
				out = append(out, pattern)
			}
		case *bcl.Block:
			out = append(out, importedPaths(x.Body, base)...)
		}
	}
	return out
}

func sourceGraphContains(rootPath, targetPath string) bool {
	if rootPath == "" || targetPath == "" {
		return false
	}
	for _, path := range sourceGraphPaths(rootPath) {
		if samePath(path, targetPath) {
			return true
		}
	}
	return false
}

func sourceGraphPaths(rootPath string) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(string)
	walk = func(path string) {
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		seen[clean] = true
		out = append(out, clean)
		doc, err := bcl.ParsePath(clean)
		if err != nil {
			return
		}
		for _, imported := range importedPaths(doc.Items, filepath.Dir(clean)) {
			walk(imported)
		}
		for _, dir := range moduleSourceDirs(doc.Items, filepath.Dir(clean)) {
			if entries, err := os.ReadDir(dir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && isBCLSourceFile(entry.Name()) {
						walk(filepath.Join(dir, entry.Name()))
					}
				}
			}
		}
	}
	walk(rootPath)
	return out
}

func missingIncludeDiagnostics(path string, src []byte) []bcl.Diagnostic {
	doc, err := bcl.ParseFile(path, src)
	if err != nil {
		return nil
	}
	return missingIncludeDiagnosticsForNodes(doc.Items, filepath.Dir(path))
}

func missingIncludeDiagnosticsForNodes(nodes []bcl.Node, base string) []bcl.Diagnostic {
	var out []bcl.Diagnostic
	for _, n := range nodes {
		switch x := n.(type) {
		case *bcl.ImportDecl:
			if resolved, ok := includedPathExists(x.Path, base); !ok {
				out = append(out, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("included file not found: %s", resolved), Span: x.Span})
			}
		case *bcl.Block:
			if x.Type == "module" {
				if source, span, ok := blockStringAssignmentWithSpan(x, "source"); ok {
					if resolved, exists := includedPathExists(source, base); !exists {
						out = append(out, bcl.Diagnostic{Severity: "error", Message: fmt.Sprintf("module source not found: %s", resolved), Span: span})
					}
				}
			}
			out = append(out, missingIncludeDiagnosticsForNodes(x.Body, base)...)
		}
	}
	return out
}

func includedPathExists(path, base string) (string, bool) {
	if isRemoteSourcePath(path) {
		return path, true
	}
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
	}
	if strings.ContainsAny(resolved, "*?[") {
		matches, err := filepath.Glob(resolved)
		return resolved, err == nil && len(matches) > 0
	}
	_, err := os.Stat(resolved)
	return resolved, err == nil || !errors.Is(err, os.ErrNotExist)
}

func replaceRawMissingFileDiagnostics(diags, replacements []bcl.Diagnostic) []bcl.Diagnostic {
	if len(diags) == 0 || len(replacements) == 0 {
		return diags
	}
	out := diags[:0]
	for _, d := range diags {
		if isRawMissingFileDiagnostic(d, replacements) {
			continue
		}
		out = append(out, d)
	}
	return out
}

func isRawMissingFileDiagnostic(d bcl.Diagnostic, replacements []bcl.Diagnostic) bool {
	msg := strings.ToLower(d.Message)
	if !strings.Contains(msg, "no such file") && !strings.Contains(msg, "cannot find") {
		return false
	}
	for _, repl := range replacements {
		if sameSpan(d.Span, repl.Span) {
			return true
		}
	}
	return false
}

func sameSpan(a, b bcl.Span) bool {
	return samePath(a.File, b.File) &&
		a.Start.Line == b.Start.Line &&
		a.Start.Column == b.Start.Column &&
		a.End.Line == b.End.Line &&
		a.End.Column == b.End.Column
}

func moduleSourceDirs(nodes []bcl.Node, base string) []string {
	var out []string
	for _, n := range nodes {
		b, ok := n.(*bcl.Block)
		if !ok {
			continue
		}
		if b.Type == "module" {
			if source := blockStringAssignment(b, "source"); source != "" {
				if !filepath.IsAbs(source) {
					source = filepath.Join(base, source)
				}
				out = append(out, source)
			}
		}
		out = append(out, moduleSourceDirs(b.Body, base)...)
	}
	return out
}

func blockStringAssignment(b *bcl.Block, name string) string {
	value, _, ok := blockStringAssignmentWithSpan(b, name)
	if !ok {
		return ""
	}
	return value
}

func blockStringAssignmentWithSpan(b *bcl.Block, name string) (string, bcl.Span, bool) {
	for _, n := range b.Body {
		a, ok := n.(*bcl.Assignment)
		if !ok || a.Name != name {
			continue
		}
		if lit, ok := a.Value.(*bcl.Literal); ok {
			if s, ok := lit.Data.(string); ok {
				return s, a.Span, true
			}
		}
	}
	return "", bcl.Span{}, false
}

func isRemoteSourcePath(path string) bool {
	return strings.HasPrefix(path, "git::") || strings.HasSuffix(path, ".git") || strings.Contains(path, "://")
}

func samePath(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err == nil {
		a = aa
	}
	bb, err := filepath.Abs(b)
	if err == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func severity(s string) int {
	switch s {
	case "error":
		return 1
	case "warning":
		return 2
	case "info":
		return 3
	default:
		return 4
	}
}

func completionKind(kind string) int {
	switch kind {
	case "function":
		return 3
	case "constant":
		return 21
	case "field":
		return 5
	case "schema", "type":
		return 7
	case "snippet":
		return 15
	default:
		return 14
	}
}

func symbolKind(kind bcl.SymbolKind) int {
	switch kind {
	case bcl.SymbolConst:
		return 14
	case bcl.SymbolImport:
		return 2
	case bcl.SymbolSchema, bcl.SymbolType:
		return 5
	case bcl.SymbolParam:
		return 8
	case bcl.SymbolField, bcl.SymbolAssignment:
		return 8
	case bcl.SymbolFunction:
		return 12
	default:
		return 23
	}
}

var semanticTokenTypes = []string{"namespace", "type", "class", "enum", "interface", "struct", "typeParameter", "parameter", "variable", "property", "enumMember", "event", "function", "method", "macro", "keyword", "modifier", "comment", "string", "number", "regexp", "operator", "decorator"}

func semanticTypeIndex(kind string) int {
	switch kind {
	case "keyword":
		return 15
	case "property":
		return 9
	case "function":
		return 12
	case "string":
		return 18
	case "number":
		return 19
	case "operator", "punctuation":
		return 21
	default:
		return 8
	}
}
