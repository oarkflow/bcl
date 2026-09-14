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
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

	// deps maps an analyzed document to the files it pulls in, and importers is
	// its inverse. Keeping the graph in memory is what makes a keystroke cost one
	// file's analysis instead of re-parsing every indexed file from disk.
	deps      map[string][]string
	importers map[string][]string
	// owners caches ownerEntrypoint per path: resolving it walks parent
	// directories parsing candidate entrypoints, far too expensive per keystroke.
	owners map[string]string
	// pending holds debounce timers so a burst of keystrokes analyzes once.
	pending map[string]*time.Timer
}

// initMaps makes a directly-constructed server usable. Callers hold s.mu.
func (s *server) initMaps() {
	if s.files == nil {
		s.files = map[string]string{}
	}
	if s.index == nil {
		s.index = map[string]*bcl.Analysis{}
	}
	if s.deps == nil {
		s.deps = map[string][]string{}
	}
	if s.importers == nil {
		s.importers = map[string][]string{}
	}
	if s.owners == nil {
		s.owners = map[string]string{}
	}
	if s.pending == nil {
		s.pending = map[string]*time.Timer{}
	}
}

func newServer(in io.Reader, out io.Writer) *server {
	return &server{
		in:        bufio.NewReader(in),
		out:       out,
		files:     map[string]string{},
		index:     map[string]*bcl.Analysis{},
		deps:      map[string][]string{},
		importers: map[string][]string{},
		owners:    map[string]string{},
		pending:   map[string]*time.Timer{},
	}
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
	s := newServer(os.Stdin, os.Stdout)
	if err := s.serve(); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (s *server) serve() error {
	for {
		msg, err := readMessage(s.in)
		if err != nil {
			// A single unparseable body must not take the server down: the frame was
			// consumed, so the stream is still aligned and the next message is fine.
			if errors.Is(err, errBadMessageBody) {
				logf("skipping unparseable message: %v", err)
				continue
			}
			return err
		}
		if msg.Method == "" {
			continue
		}
		s.handle(msg)
	}
}

// handle runs one message with a panic guard. An editor keeps the server alive
// for a whole session, so a panic on one half-typed document must degrade to a
// single failed request instead of killing the process and every open file's
// diagnostics with it.
func (s *server) handle(msg rpcMessage) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		logf("recovered panic handling %s: %v\n%s", msg.Method, r, debug.Stack())
		if msg.ID != nil {
			s.respondError(msg.ID, -32603, fmt.Sprintf("internal error handling %s", msg.Method))
		}
	}()
	s.handleMessage(msg)
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bcl-lsp: "+format+"\n", args...)
}

func (s *server) handleMessage(msg rpcMessage) {
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
				"textDocumentSync":                2,
				"completionProvider":              map[string]any{"resolveProvider": false, "triggerCharacters": []string{".", "\"", " "}},
				"hoverProvider":                   true,
				"definitionProvider":              true,
				"referencesProvider":              true,
				"renameProvider":                  true,
				"documentSymbolProvider":          true,
				"workspaceSymbolProvider":         true,
				"documentFormattingProvider":      true,
				"documentRangeFormattingProvider": true,
				"foldingRangeProvider":            true,
				"documentLinkProvider":            map[string]any{"resolveProvider": false},
				"codeActionProvider":              true,
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
			ContentChanges []contentChange        `json:"contentChanges"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if len(p.ContentChanges) == 0 {
			return
		}
		text := s.fileText(p.TextDocument.URI)
		for _, change := range p.ContentChanges {
			text = applyContentChange(text, change)
		}
		s.storeFile(p.TextDocument.URI, text)
		s.scheduleAnalysis(p.TextDocument.URI)
	case "textDocument/didSave":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Text         string                 `json:"text,omitempty"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.invalidateDiskCaches()
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
			Options      formattingOptions      `json:"options"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.formatEdits(p.TextDocument.URI, p.Options))
	case "textDocument/rangeFormatting":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
			Range        rangeLSP               `json:"range"`
			Options      formattingOptions      `json:"options"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.rangeFormatEdits(p.TextDocument.URI, p.Range, p.Options))
	case "textDocument/foldingRange":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.foldingRanges(p.TextDocument.URI))
	case "textDocument/documentLink":
		var p struct {
			TextDocument textDocumentIdentifier `json:"textDocument"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		s.respond(msg.ID, s.documentLinks(p.TextDocument.URI))
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
	case "workspace/didChangeWatchedFiles":
		// A BCL file changed outside the editor; everything derived from disk is stale.
		s.invalidateDiskCaches()
	case "bcl/recentSymbols":
		s.respond(msg.ID, s.recentSymbols())
	case "bcl/analyzeNow":
		// Internal: a debounced analysis firing. Routed through handle so it gets
		// the same panic guard as any client request.
		var uri string
		_ = json.Unmarshal(msg.Params, &uri)
		s.analyzeURI(uri)
		s.reanalyzeDependents(uri)
	default:
		if msg.ID != nil {
			s.respondError(msg.ID, -32601, "method not found")
		}
	}
}

func (s *server) setFile(uri, text string) {
	s.storeFile(uri, text)
	s.analyzeURI(uri)
	s.reanalyzeDependents(uri)
}

// storeFile records the new text without analyzing it. Typing must never wait on
// analysis: the document is what later requests read, and analysis is scheduled.
func (s *server) storeFile(uri, text string) {
	s.mu.Lock()
	s.initMaps()
	s.files[uri] = text
	delete(s.owners, uriPath(uri))
	s.mu.Unlock()
}

// analysisDebounce is how long a document rests before it is analyzed. Editors
// send one change per character; analyzing each one wastes the whole budget on
// intermediate states nobody sees.
const analysisDebounce = 150 * time.Millisecond

// scheduleAnalysis coalesces a burst of edits into a single analysis pass.
// On-demand requests (completion, hover, formatting) still analyze synchronously,
// so a debounced document is never stale when something actually reads it.
func (s *server) scheduleAnalysis(uri string) {
	s.mu.Lock()
	s.initMaps()
	if timer, ok := s.pending[uri]; ok {
		timer.Stop()
	}
	s.pending[uri] = time.AfterFunc(analysisDebounce, func() {
		s.mu.Lock()
		delete(s.pending, uri)
		s.mu.Unlock()
		s.handle(rpcMessage{Method: "bcl/analyzeNow", Params: json.RawMessage(strconv.Quote(uri))})
	})
	s.mu.Unlock()
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
	partial := s.isPartialBCLFile(analysisPath)
	if !samePath(analysisPath, path) {
		partial = true
	}
	// Completions are skipped here and computed when the client actually asks:
	// analysis runs on every edit, completion requests are comparatively rare.
	a, diags := bcl.AnalyzeFile(analysisPath, []byte(text), &bcl.Options{Strict: true, Partial: partial, ResolveImports: true, BaseDir: filepath.Dir(analysisPath), SkipCompletions: true})
	includeDiags := missingIncludeDiagnostics(analysisPath, []byte(text))
	if len(includeDiags) > 0 {
		diags = replaceRawMissingFileDiagnostics(diags, includeDiags)
		diags = append(diags, includeDiags...)
		a.Diagnostics = diags
	}
	s.mu.Lock()
	s.initMaps()
	s.index[uri] = a
	if analysisURI != uri {
		s.index[analysisURI] = a
	}
	s.mu.Unlock()
	graph := sourceGraphPaths(analysisPath)
	s.recordDependencies(uri, graph)
	if analysisURI != uri {
		s.recordDependencies(analysisURI, graph)
	}
	s.publishDiagnostics(analysisURI, diags, append([]string{path}, graph...))
	return a
}

// maxIndexedFiles bounds the startup scan. Indexing exists to make workspace
// symbols and cross-file references work, not to parse an entire monorepo before
// the editor becomes responsive.
const maxIndexedFiles = 2000

// skippedIndexDirs are never worth walking: they hold dependencies and build
// output, not the workspace's own BCL sources.
var skippedIndexDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"out":          true,
	"target":       true,
}

func skipIndexDir(name string) bool {
	return strings.HasPrefix(name, ".") || skippedIndexDirs[name]
}

func (s *server) indexWorkspace() {
	root := uriPath(s.rootURI)
	if root == "" {
		return
	}
	indexed := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable subtree is skipped, not fatal
		}
		if d.IsDir() {
			if path != root && skipIndexDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isBCLSourceFile(path) {
			return nil
		}
		if indexed >= maxIndexedFiles {
			logf("workspace index stopped at %d files; workspace symbols may be incomplete", maxIndexedFiles)
			return filepath.SkipAll
		}
		indexed++
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
			item := map[string]any{
				"range":    lspRange(d.Span),
				"severity": severity(d.Severity),
				"source":   "bcl",
				"message":  d.Message,
			}
			if d.Code != "" {
				item["code"] = d.Code
			}
			items = append(items, item)
		}
		s.notify("textDocument/publishDiagnostics", map[string]any{"uri": target, "diagnostics": items})
	}
}

func (s *server) ownerEntrypoint(path string) string {
	if path == "" || !isBCLSourceFile(path) {
		return ""
	}
	s.mu.Lock()
	s.initMaps()
	cached, ok := s.owners[path]
	s.mu.Unlock()
	if ok {
		return cached
	}
	owner := s.resolveOwnerEntrypoint(path)
	s.mu.Lock()
	s.initMaps()
	s.owners[path] = owner
	s.mu.Unlock()
	return owner
}

// resolveOwnerEntrypoint finds the entrypoint document that pulls in path, by
// walking up to the workspace root and parsing each candidate's source graph.
// Expensive, hence the cache in ownerEntrypoint.
func (s *server) resolveOwnerEntrypoint(path string) string {
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

// formattingOptions is the subset of the LSP FormattingOptions the formatter
// understands, so a document follows the editor's own indentation settings.
type formattingOptions struct {
	TabSize      int  `json:"tabSize"`
	InsertSpaces bool `json:"insertSpaces"`
}

func (o formattingOptions) format() bcl.FormatOptions {
	return bcl.FormatOptions{IndentWidth: o.TabSize, UseTabs: o.TabSize > 0 && !o.InsertSpaces}
}

// formatOptionsFor prefers the project's .bclfmt over the editor's own settings:
// a file checked into the repository is a deliberate decision by the team, while
// the editor's tab size is a personal one, and formatting is shared output.
func (s *server) formatOptionsFor(uri string, client formattingOptions) bcl.FormatOptions {
	opts := client.format()
	project, path, err := bcl.FindFormatConfig(filepath.Dir(uriPath(uri)))
	if err != nil {
		logf("ignoring %s: %v", path, err)
		return opts
	}
	if path == "" {
		return opts
	}
	if project.IndentWidth > 0 {
		opts.IndentWidth = project.IndentWidth
	}
	opts.UseTabs = project.UseTabs
	opts.KeepBlankLines = project.KeepBlankLines
	return opts
}

func (s *server) formatEdits(uri string, opts formattingOptions) []any {
	text := s.fileText(uri)
	out, err := bcl.FormatWithOptions([]byte(text), s.formatOptionsFor(uri, opts))
	if err != nil {
		return nil
	}
	lines := strings.Count(text, "\n") + 1
	return []any{map[string]any{"range": map[string]any{"start": position{}, "end": position{Line: lines, Character: 0}}, "newText": string(out)}}
}

// rangeFormatEdits formats only the lines the selection touches. Formatting is
// line-order preserving, so each output line carries the source line it came
// from and the edit covers exactly the source lines that produced the selection.
func (s *server) rangeFormatEdits(uri string, rng rangeLSP, opts formattingOptions) []any {
	text := s.fileText(uri)
	lines, err := bcl.FormatLines([]byte(text), s.formatOptionsFor(uri, opts))
	if err != nil {
		return nil
	}
	first, last := rng.Start.Line+1, rng.End.Line+1
	if rng.End.Character == 0 && last > first {
		last--
	}
	var selected []string
	startLine, endLine := 0, 0
	for _, ln := range lines {
		if ln.End < first || ln.Start > last {
			continue
		}
		if startLine == 0 || ln.Start < startLine {
			startLine = ln.Start
		}
		if ln.End > endLine {
			endLine = ln.End
		}
		selected = append(selected, ln.Text)
	}
	if len(selected) == 0 {
		return nil
	}
	eol := bcl.LineEnding([]byte(text))
	newText := strings.Join(selected, eol) + eol
	return []any{map[string]any{
		"range":   rangeLSP{Start: position{Line: startLine - 1}, End: position{Line: endLine}},
		"newText": newText,
	}}
}

// foldingRanges folds by real block structure rather than by indentation, so a
// one-line block is not foldable and a block whose body is indented oddly still
// is. LSP line numbers are zero-based.
func (s *server) foldingRanges(uri string) []any {
	ranges := bcl.FoldingRanges([]byte(s.fileText(uri)))
	out := make([]any, 0, len(ranges))
	for _, r := range ranges {
		item := map[string]any{"startLine": r.Start - 1, "endLine": r.End - 1}
		if r.Kind == bcl.FoldComment || r.Kind == bcl.FoldImports {
			item["kind"] = string(r.Kind)
		}
		out = append(out, item)
	}
	return out
}

// documentLinks makes every import path clickable. It reads the tokens rather
// than the analysis because import resolution replaces each import with the
// nodes it pulled in, leaving nothing to link; tokens also keep working while
// the document is mid-edit. A glob or a missing file yields no link rather than
// a broken one.
func (s *server) documentLinks(uri string) []any {
	path := uriPath(uri)
	toks, _ := bcl.TokenizeFile(path, []byte(s.fileText(uri)))
	base := filepath.Dir(path)
	var out []any
	for i, tok := range toks {
		if tok.Text != "import" || i+1 >= len(toks) {
			continue
		}
		next := toks[i+1]
		target := next.Text
		if target == "" || strings.ContainsAny(target, "*?[") {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(base, target)
		}
		if info, err := os.Stat(target); err != nil || info.IsDir() {
			continue
		}
		out = append(out, map[string]any{
			"range":   lspRange(next.Span),
			"target":  pathURI(target),
			"tooltip": "Open " + filepath.Base(target),
		})
	}
	return out
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

// reanalyzeDependents re-runs only the documents that actually import the changed
// file, using the dependency graph recorded when each document was analyzed.
func (s *server) reanalyzeDependents(changedURI string) {
	changed := uriPath(changedURI)
	s.mu.Lock()
	targets := make([]string, 0, 4)
	seen := map[string]bool{changedURI: true}
	for dep, uris := range s.importers {
		if !samePath(changed, dep) && !pathWithin(changed, dep) {
			continue
		}
		for _, uri := range uris {
			if !seen[uri] {
				seen[uri] = true
				targets = append(targets, uri)
			}
		}
	}
	s.mu.Unlock()
	for _, uri := range targets {
		s.analyzeURI(uri)
	}
}

// recordDependencies stores the files an analyzed document pulls in, replacing
// whatever the previous pass recorded for it.
func (s *server) recordDependencies(uri string, deps []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initMaps()
	for _, old := range s.deps[uri] {
		if list := s.importers[old]; len(list) > 0 {
			kept := list[:0]
			for _, candidate := range list {
				if candidate != uri {
					kept = append(kept, candidate)
				}
			}
			if len(kept) == 0 {
				delete(s.importers, old)
			} else {
				s.importers[old] = kept
			}
		}
	}
	if len(deps) == 0 {
		delete(s.deps, uri)
		return
	}
	s.deps[uri] = deps
	for _, dep := range deps {
		s.importers[dep] = append(s.importers[dep], uri)
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

// lspDiagnostic is the subset of a client-sent diagnostic the code actions need.
// Code is what a fix keys on: message wording may change, a code may not.
type lspDiagnostic struct {
	Message string   `json:"message"`
	Range   rangeLSP `json:"range"`
	Code    string   `json:"code"`
}

// is reports whether the diagnostic carries this code, falling back to the
// message for a diagnostic that has not been classified yet.
func (d lspDiagnostic) is(code string, messageFallback string) bool {
	if d.Code != "" {
		return d.Code == code
	}
	return messageFallback != "" && strings.Contains(d.Message, messageFallback)
}

func (s *server) codeActions(raw json.RawMessage) []any {
	var p struct {
		TextDocument textDocumentIdentifier `json:"textDocument"`
		Context      struct {
			Diagnostics []lspDiagnostic `json:"diagnostics"`
		} `json:"context"`
	}
	_ = json.Unmarshal(raw, &p)
	actions := []any{
		map[string]any{"title": "Format BCL document", "kind": "source.format", "command": map[string]any{"title": "Format BCL document", "command": "editor.action.formatDocument"}},
		map[string]any{"title": "Restart BCL language server", "kind": "quickfix", "command": map[string]any{"title": "Restart BCL language server", "command": "bcl.restartLanguageServer"}},
	}
	appendLine := strings.Count(s.fileText(p.TextDocument.URI), "\n") + 1
	for _, d := range p.Context.Diagnostics {
		if d.is(bcl.CodeModuleInputs, "missing required input") {
			if input := quotedTail(d.Message); input != "" {
				actions = append(actions, map[string]any{
					"title":       fmt.Sprintf("Add module input %q", input),
					"kind":        "quickfix",
					"diagnostics": []any{d},
					"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.End.Line+1, fmt.Sprintf("  inputs {\n    %s value\n  }\n", input)),
				})
			}
		}
		if d.is(bcl.CodeUnknownReference, "unknown reference") {
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
		if d.is(bcl.CodeMissingRequiredField, "missing required field") && strings.Contains(d.Message, "missing required field") {
			if field := quotedTail(d.Message); field != "" {
				actions = append(actions, map[string]any{
					"title":       fmt.Sprintf("Insert required field %q", field),
					"kind":        "quickfix",
					"diagnostics": []any{d},
					"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.End.Line+1, fmt.Sprintf("  %s value\n", field)),
				})
			}
		}
		if d.is(bcl.CodeUnused, "unused") {
			actions = append(actions, map[string]any{
				"title":       "Remove the unused declaration",
				"kind":        "quickfix",
				"diagnostics": []any{d},
				"edit":        lineDeleteEdit(p.TextDocument.URI, d.Range.Start.Line, d.Range.End.Line),
			})
		}
		if d.is(bcl.CodeUnknownField, "unknown field") {
			actions = append(actions, map[string]any{
				"title":       "Remove the unknown field",
				"kind":        "quickfix",
				"diagnostics": []any{d},
				"edit":        lineDeleteEdit(p.TextDocument.URI, d.Range.Start.Line, d.Range.End.Line),
			})
		}
		if d.is(bcl.CodeDuplicateDeclaration, "duplicate") {
			if name := quotedTail(d.Message); name != "" {
				// Renaming keeps both declarations: whichever one is wrong, the author
				// can see it. Deleting would silently discard whatever it held.
				actions = append(actions, map[string]any{
					"title":       fmt.Sprintf("Rename this declaration to %q", name+"_2"),
					"kind":        "quickfix",
					"diagnostics": []any{d},
					"edit":        renameInLineEdit(s.fileText(p.TextDocument.URI), p.TextDocument.URI, d.Range.Start.Line, name, name+"_2"),
				})
			}
		}
		if d.is(bcl.CodeDeprecated, "deprecated") {
			actions = append(actions, map[string]any{
				"title":       "Add replaced_by",
				"kind":        "quickfix",
				"diagnostics": []any{d},
				"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.Start.Line+1, "  replaced_by \"\"\n"),
			})
		}
		if d.is(bcl.CodeMatchNoCatchAll, "no catch-all case") {
			actions = append(actions, map[string]any{
				"title":       "Add a catch-all case",
				"kind":        "quickfix",
				"diagnostics": []any{d},
				"edit":        lineInsertEdit(p.TextDocument.URI, d.Range.End.Line+1, "  case ANY => null\n"),
			})
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
	if hasCode(p.Context.Diagnostics, bcl.CodeMissingVersion, "missing bcl version declaration") {
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

// lineDeleteEdit removes whole lines, which is what "remove this declaration"
// means for a line-oriented language: leaving an empty line behind would be a
// second edit the author has to make.
func lineDeleteEdit(uri string, startLine, endLine int) map[string]any {
	if endLine < startLine {
		endLine = startLine
	}
	return map[string]any{"changes": map[string]any{
		uri: []any{map[string]any{
			"range":   rangeLSP{Start: position{Line: startLine, Character: 0}, End: position{Line: endLine + 1, Character: 0}},
			"newText": "",
		}},
	}}
}

// renameInLineEdit replaces the first occurrence of old on one line. The
// diagnostic points at the declaration, so that occurrence is its name.
func renameInLineEdit(text, uri string, line int, old, replacement string) map[string]any {
	lines := strings.Split(text, "\n")
	if line < 0 || line >= len(lines) {
		return map[string]any{"changes": map[string]any{uri: []any{}}}
	}
	col := strings.Index(lines[line], old)
	if col < 0 {
		return map[string]any{"changes": map[string]any{uri: []any{}}}
	}
	return map[string]any{"changes": map[string]any{
		uri: []any{map[string]any{
			"range":   rangeLSP{Start: position{Line: line, Character: col}, End: position{Line: line, Character: col + len(old)}},
			"newText": replacement,
		}},
	}}
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

func hasCode(diags []lspDiagnostic, code, messageFallback string) bool {
	for _, d := range diags {
		if d.is(code, messageFallback) {
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
	if err := json.Unmarshal(body, &msg); err != nil {
		return rpcMessage{}, fmt.Errorf("%w: %v", errBadMessageBody, err)
	}
	return msg, nil
}

// errBadMessageBody marks a frame whose header was read correctly but whose JSON
// body was not, which is recoverable: the stream is still frame-aligned.
var errBadMessageBody = errors.New("malformed message body")

// contentChange is one textDocument/didChange delta. Range is nil for a
// whole-document replacement.
type contentChange struct {
	Range *rangeLSP `json:"range"`
	Text  string    `json:"text"`
}

// applyContentChange applies one incremental change to text. The server
// advertises incremental sync, so every keystroke arrives as a range delta and
// must be spliced into the document rather than replacing it.
func applyContentChange(text string, change contentChange) string {
	if change.Range == nil {
		return change.Text
	}
	start := offsetAt(text, change.Range.Start)
	end := offsetAt(text, change.Range.End)
	if end < start {
		start, end = end, start
	}
	var b strings.Builder
	b.Grow(len(text) - (end - start) + len(change.Text))
	b.WriteString(text[:start])
	b.WriteString(change.Text)
	b.WriteString(text[end:])
	return b.String()
}

// offsetAt converts an LSP position to a byte offset. LSP columns count UTF-16
// code units, so anything outside the BMP (an emoji in a description string)
// advances the column by two.
func offsetAt(text string, pos position) int {
	i := 0
	for line := 0; line < pos.Line; line++ {
		nl := strings.IndexByte(text[i:], '\n')
		if nl < 0 {
			return len(text)
		}
		i += nl + 1
	}
	for units := 0; units < pos.Character && i < len(text) && text[i] != '\n'; {
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if r > 0xFFFF {
			units += 2
		} else {
			units++
		}
	}
	return i
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

// invalidateDiskCaches drops everything derived from files on disk. Called when a
// document is saved or the workspace changes underneath us.
func (s *server) invalidateDiskCaches() {
	s.mu.Lock()
	s.owners = map[string]string{}
	s.mu.Unlock()
	sourceGraphCache.Clear()
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
	// The dependency graph already records what every analyzed document pulls in,
	// so consult it before falling back to walking the workspace.
	s.mu.Lock()
	for dep := range s.importers {
		if samePath(abs, dep) {
			s.mu.Unlock()
			return true
		}
	}
	s.mu.Unlock()
	var partial bool
	_ = filepath.WalkDir(root, func(candidate string, d os.DirEntry, err error) error {
		if err != nil || partial {
			return nil
		}
		if d.IsDir() {
			if candidate != root && skipIndexDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isBCLSourceFile(candidate) {
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

func isBCLSourceFile(path string) bool {
	switch filepath.Ext(path) {
	case ".bcl", ".schema":
		return true
	default:
		return false
	}
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

// sourceGraphCache memoizes import-graph walks. The walk reads files from disk,
// so its result only changes when a file on disk changes - not on every
// keystroke in an unsaved buffer, which is when it used to be recomputed.
var sourceGraphCache sync.Map // rootPath -> *sourceGraphEntry

type sourceGraphEntry struct {
	stamp string
	paths []string
}

// graphStamp fingerprints every file the previous walk visited, so an edit to
// any of them invalidates the entry.
func graphStamp(paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		b.WriteString(path)
		if info, err := os.Stat(path); err == nil {
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
			b.WriteByte(':')
			b.WriteString(strconv.FormatInt(info.Size(), 10))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func sourceGraphPaths(rootPath string) []string {
	if cached, ok := sourceGraphCache.Load(rootPath); ok {
		entry := cached.(*sourceGraphEntry)
		if entry.stamp == graphStamp(entry.paths) {
			return entry.paths
		}
	}
	paths := walkSourceGraph(rootPath)
	sourceGraphCache.Store(rootPath, &sourceGraphEntry{stamp: graphStamp(paths), paths: paths})
	return paths
}

func walkSourceGraph(rootPath string) []string {
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
