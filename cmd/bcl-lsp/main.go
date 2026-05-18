package main

import (
	"bufio"
	"bytes"
	"encoding/json"
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
		}
		_ = json.Unmarshal(msg.Params, &p)
		a := s.analyzeURI(p.TextDocument.URI)
		s.respond(msg.ID, s.completions(a))
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
		s.respond(msg.ID, s.codeActions())
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
}

func (s *server) analyzeURI(uri string) *bcl.Analysis {
	s.mu.Lock()
	text, ok := s.files[uri]
	s.mu.Unlock()
	path := uriPath(uri)
	if !ok {
		if b, err := os.ReadFile(path); err == nil {
			text = string(b)
		}
	}
	a, diags := bcl.AnalyzeFile(path, []byte(text), &bcl.Options{Strict: true})
	s.mu.Lock()
	s.index[uri] = a
	s.mu.Unlock()
	s.publishDiagnostics(uri, diags)
	return a
}

func (s *server) indexWorkspace() {
	root := uriPath(s.rootURI)
	if root == "" {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".bcl" {
			return nil
		}
		s.analyzeURI(pathURI(path))
		return nil
	})
}

func (s *server) publishDiagnostics(uri string, diags []bcl.Diagnostic) {
	items := make([]any, 0, len(diags))
	for _, d := range diags {
		items = append(items, map[string]any{
			"range":    lspRange(d.Span),
			"severity": severity(d.Severity),
			"source":   "bcl",
			"message":  d.Message,
		})
	}
	s.notify("textDocument/publishDiagnostics", map[string]any{"uri": uri, "diagnostics": items})
}

func (s *server) completions(a *bcl.Analysis) []any {
	rank := map[string]int{}
	for i, name := range s.recent {
		rank[name] = len(s.recent) - i
	}
	comps := append([]bcl.Completion(nil), a.Completions...)
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
	sym, ok := bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1)
	if !ok {
		s.respond(msg.ID, nil)
		return
	}
	s.touch(sym.Name)
	if !refs {
		s.respond(msg.ID, map[string]any{"uri": p.TextDocument.URI, "range": lspRange(sym.SelectionSpan)})
		return
	}
	locs := []any{map[string]any{"uri": p.TextDocument.URI, "range": lspRange(sym.SelectionSpan)}}
	for _, r := range a.References {
		if r.Name == sym.Name {
			locs = append(locs, map[string]any{"uri": p.TextDocument.URI, "range": lspRange(r.Span)})
		}
	}
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
	sym, ok := bcl.SymbolAt(a, p.Position.Line+1, p.Position.Character+1)
	if !ok {
		s.respond(msg.ID, nil)
		return
	}
	edits := []any{map[string]any{"range": lspRange(sym.SelectionSpan), "newText": p.NewName}}
	for _, r := range a.References {
		if r.Name == sym.Name {
			edits = append(edits, map[string]any{"range": lspRange(r.Span), "newText": p.NewName})
		}
	}
	s.respond(msg.ID, map[string]any{"changes": map[string]any{p.TextDocument.URI: edits}})
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

func (s *server) codeActions() []any {
	return []any{
		map[string]any{"title": "Format BCL document", "kind": "source.format", "command": map[string]any{"title": "Format BCL document", "command": "editor.action.formatDocument"}},
		map[string]any{"title": "Insert BCL version declaration", "kind": "quickfix"},
		map[string]any{"title": "Wrap value with sensitive(...)", "kind": "quickfix"},
		map[string]any{"title": "Add missing required module input", "kind": "quickfix"},
		map[string]any{"title": "Create predicate or test stub", "kind": "quickfix"},
		map[string]any{"title": "Restart BCL language server", "kind": "quickfix", "command": map[string]any{"title": "Restart BCL language server", "command": "bcl.restartLanguageServer"}},
	}
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
