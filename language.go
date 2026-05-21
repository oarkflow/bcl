package bcl

import (
	"fmt"
	"sort"
	"strings"
)

type SyntaxToken struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Span Span   `json:"span"`
}

type SymbolKind string

const (
	SymbolAssignment SymbolKind = "assignment"
	SymbolBlock      SymbolKind = "block"
	SymbolConst      SymbolKind = "const"
	SymbolImport     SymbolKind = "import"
	SymbolSchema     SymbolKind = "schema"
	SymbolField      SymbolKind = "field"
	SymbolParam      SymbolKind = "param"
	SymbolSet        SymbolKind = "set"
	SymbolType       SymbolKind = "type"
	SymbolFunction   SymbolKind = "function"
	SymbolReference  SymbolKind = "reference"
	SymbolRuntime    SymbolKind = "runtime"
)

type LanguageSymbol struct {
	Name              string           `json:"name"`
	Detail            string           `json:"detail,omitempty"`
	Kind              SymbolKind       `json:"kind"`
	Span              Span             `json:"span"`
	SelectionSpan     Span             `json:"selection_span"`
	Container         string           `json:"container,omitempty"`
	Value             string           `json:"value,omitempty"`
	ValueKind         string           `json:"value_kind,omitempty"`
	ResolvedTarget    string           `json:"resolved_target,omitempty"`
	ReferencedTargets []string         `json:"referenced_targets,omitempty"`
	Deprecated        string           `json:"deprecated,omitempty"`
	Sensitive         bool             `json:"sensitive,omitempty"`
	Children          []LanguageSymbol `json:"children,omitempty"`
}

type ReferenceUse struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Span Span   `json:"span"`
}

type Declaration struct {
	CanonicalName string            `json:"canonical_name"`
	DisplayName   string            `json:"display_name"`
	LocalName     string            `json:"local_name,omitempty"`
	Kind          SymbolKind        `json:"kind"`
	File          string            `json:"file,omitempty"`
	Span          Span              `json:"span"`
	SelectionSpan Span              `json:"selection_span"`
	Container     string            `json:"container,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	RenameSafe    bool              `json:"rename_safe,omitempty"`
}

type LanguageReference struct {
	TargetCanonicalName string            `json:"target_canonical_name"`
	Text                string            `json:"text"`
	Role                string            `json:"role,omitempty"`
	File                string            `json:"file,omitempty"`
	Span                Span              `json:"span"`
	RenameSafe          bool              `json:"rename_safe,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type CompletionContext struct {
	File           string   `json:"file,omitempty"`
	Line           int      `json:"line"`
	Column         int      `json:"column"`
	EnclosingBlock string   `json:"enclosing_block,omitempty"`
	AssignmentName string   `json:"assignment_name,omitempty"`
	ValuePosition  bool     `json:"value_position,omitempty"`
	ExpectedValues []string `json:"expected_values,omitempty"`
}

type WorkspaceIndex struct {
	Files               map[string]*Analysis `json:"-"`
	Declarations        []Declaration        `json:"declarations"`
	References          []LanguageReference  `json:"references"`
	ReverseDependencies map[string][]string  `json:"reverse_dependencies,omitempty"`
}

type Completion struct {
	Label         string `json:"label"`
	Kind          string `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insert_text,omitempty"`
}

type hintInfo struct {
	Name        string
	Kind        SymbolKind
	Signature   string
	Description string
	InsertText  string
	Examples    []string
}

type Analysis struct {
	File         string                    `json:"file"`
	Symbols      []LanguageSymbol          `json:"symbols"`
	Declarations map[string]LanguageSymbol `json:"declarations"`
	References   []ReferenceUse            `json:"references"`
	Imports      []LanguageSymbol          `json:"imports"`
	Schemas      map[string]LanguageSymbol `json:"schemas"`
	Constants    map[string]LanguageSymbol `json:"constants"`
	Sets         map[string]LanguageSymbol `json:"sets"`
	Types        map[string]LanguageSymbol `json:"types"`
	Index        WorkspaceIndex            `json:"index"`
	Completions  []Completion              `json:"completions"`
	Diagnostics  []Diagnostic              `json:"diagnostics,omitempty"`
}

func TokenizeFile(name string, src []byte) ([]SyntaxToken, []Diagnostic) {
	toks, errs := lex(name, src)
	out := make([]SyntaxToken, 0, len(toks))
	for i, tok := range toks {
		if tok.kind == tokEOF || tok.kind == tokNewline {
			continue
		}
		out = append(out, SyntaxToken{Type: tokenEditorTypeAt(toks, i), Text: tok.text, Span: tok.span})
	}
	return out, errs
}

func AnalyzeFile(name string, src []byte, opts *Options) (*Analysis, []Diagnostic) {
	doc, err := ParseFile(name, src)
	a := &Analysis{
		File:         name,
		Declarations: map[string]LanguageSymbol{},
		Schemas:      map[string]LanguageSymbol{},
		Constants:    map[string]LanguageSymbol{},
		Sets:         map[string]LanguageSymbol{},
		Types:        map[string]LanguageSymbol{},
	}
	if err != nil {
		if e, ok := err.(ErrorList); ok {
			a.Diagnostics = e
			a.Completions = DefaultCompletions()
			return a, e
		}
		diag := Diagnostic{Severity: "error", Message: err.Error()}
		a.Diagnostics = []Diagnostic{diag}
		a.Completions = DefaultCompletions()
		return a, a.Diagnostics
	}
	analysisDoc := doc
	if opts != nil && (opts.ResolveImports || opts.ResolveModules) {
		resolved, diags := ResolveDocument(doc, opts)
		a.Diagnostics = append(a.Diagnostics, diags...)
		analysisDoc = resolved
	}
	a.Symbols = analyzeNodes(analysisDoc.Items, "", a)
	a.Index = buildLanguageFeatureIndex(name, a)
	a.Diagnostics = append(a.Diagnostics, Lint(analysisDoc, opts)...)
	a.Completions = analysisCompletions(a)
	return a, a.Diagnostics
}

func DefaultCompletions() []Completion {
	var out []Completion
	for _, item := range keywordHints {
		out = append(out, Completion{Label: item.Name, Kind: "keyword", Detail: item.Signature, Documentation: item.Description})
	}
	for _, item := range builtinFunctionHints {
		out = append(out, Completion{Label: item.Name, Kind: "function", Detail: item.Signature, Documentation: item.Description, InsertText: item.InsertText})
	}
	for _, item := range runtimeValueHints {
		out = append(out, Completion{Label: item.Name, Kind: "field", Detail: item.Signature, Documentation: item.Description})
	}
	for _, snip := range []Completion{
		{Label: "schema block", Kind: "snippet", Detail: "Schema declaration", InsertText: "schema ${1:name} {\n  required ${2:field} ${3:string}\n}"},
		{Label: "param block", Kind: "snippet", Detail: "Module input parameter", InsertText: "param ${1:name} ${2:string} {\n  required true\n}"},
		{Label: "predicate block", Kind: "snippet", Detail: "Reusable condition predicate", InsertText: "predicate \"${1:name}\" {\n  all {\n    ${2:condition}\n  }\n}"},
		{Label: "test block", Kind: "snippet", Detail: "Executable BCL test", InsertText: "test \"${1:name}\" {\n  input {\n    ${2:key} ${3:value}\n  }\n\n  expect {\n    diagnostics none\n  }\n}"},
		{Label: "policy block", Kind: "snippet", Detail: "Policy block", InsertText: "policy \"${1:name}\" {\n  effect ${2:allow}\n}"},
		{Label: "set block", Kind: "snippet", Detail: "Reusable set", InsertText: "set \"${1:name}\" {\n  ${2:item}\n}"},
		{Label: "decision table block", Kind: "snippet", Detail: "Decision table", InsertText: "decision_table \"${1:name}\" {\n  default ${2:require_review}\n  hit_policy ${3:first}\n\n  row \"${4:rule-id}\" {\n    priority ${5:10}\n    when { ${6:condition} }\n    then { outcome { decision ${7:allow} reason \"${8:reason}\" } }\n  }\n}"},
		{Label: "decision schema block", Kind: "snippet", Detail: "Decision schema", InsertText: "decision_schema \"${1:name}\" {\n  effects [${2:allow}, ${3:deny}, ${4:require_review}]\n  default ${4:require_review}\n  strategy ${5:first_match}\n}"},
		{Label: "dataset block", Kind: "snippet", Detail: "Decision dataset", InsertText: "dataset \"${1:name}\" {\n  record \"${2:case}\" {\n    ${3:field} ${4:value}\n  }\n}"},
		{Label: "test matrix block", Kind: "snippet", Detail: "Decision test matrix", InsertText: "test_matrix \"${1:name}\" {\n  decision \"${2:decision_name}\"\n\n  case \"${3:case}\" {\n    input {\n      ${4:field} ${5:value}\n    }\n    expect {\n      effect \"${6:allow}\"\n    }\n  }\n}"},
	} {
		out = append(out, snip)
	}
	return out
}

func CompletionsAt(a *Analysis, src []byte, line, column int) ([]Completion, CompletionContext) {
	ctx := CompletionContext{File: a.File, Line: line, Column: column}
	current := lineText(src, line)
	before := current
	if column > 0 && column <= len(current)+1 {
		before = current[:column-1]
	}
	ctx.AssignmentName = leadingIdentifier(before)
	ctx.ValuePosition = strings.TrimSpace(before) != "" && ctx.AssignmentName != "" && !strings.HasSuffix(strings.TrimSpace(before), ctx.AssignmentName)
	ctx.EnclosingBlock = enclosingBlockName(src, line)
	ctx.ExpectedValues = expectedValuesForContext(ctx)

	out := append([]Completion(nil), a.Completions...)
	for _, v := range ctx.ExpectedValues {
		out = append(out, Completion{Label: v, Kind: "value", Detail: "BCL value"})
	}
	if strings.Contains(strings.TrimSpace(before), ".") {
		prefix := dottedPrefix(before)
		if prefix != "" {
			for _, item := range runtimeValueHints {
				if strings.HasPrefix(item.Name, prefix) {
					out = append(out, Completion{Label: item.Name, Kind: "field", Detail: item.Signature, Documentation: item.Description})
				}
			}
			for name, decl := range a.Declarations {
				if strings.HasPrefix(name, prefix) {
					out = append(out, Completion{Label: name, Kind: string(decl.Kind), Detail: decl.Detail})
				}
			}
		}
	}
	return dedupeCompletions(out), ctx
}

func SymbolAt(a *Analysis, line, column int) (LanguageSymbol, bool) {
	var refBest LanguageSymbol
	var refOK bool
	for _, r := range a.References {
		if containsPosition(r.Span, line, column) {
			var candidate LanguageSymbol
			var found bool
			if decl, ok := a.Declarations[r.Name]; ok {
				candidate = decl
				found = true
			} else if sym, ok := builtinFunctionSymbol(r.Name, r.Span); ok {
				candidate = sym
				found = true
			} else if sym, ok := runtimeReferenceSymbol(r.Name, r.Span); ok {
				candidate = sym
				found = true
			}
			if found && (!refOK || spanSize(r.Span) < spanSize(refBest.SelectionSpan)) {
				refBest = candidate
				refBest.SelectionSpan = r.Span
				refOK = true
			}
		}
	}
	if refOK {
		return refBest, true
	}
	var best LanguageSymbol
	var ok bool
	for _, s := range flattenSymbols(a.Symbols) {
		if containsPosition(s.SelectionSpan, line, column) || containsPosition(s.Span, line, column) {
			if !ok || spanSize(s.SelectionSpan) <= spanSize(best.SelectionSpan) {
				best, ok = s, true
			}
		}
	}
	return best, ok
}

func HoverMarkdown(s LanguageSymbol) string {
	var b strings.Builder
	fmt.Fprintf(&b, "```bcl\n%s %s\n```", s.Kind, s.Name)
	if s.Detail != "" {
		fmt.Fprintf(&b, "\n\n%s", s.Detail)
	}
	if s.Container != "" {
		fmt.Fprintf(&b, "\n\nContainer: `%s`", s.Container)
	}
	if s.Deprecated != "" {
		fmt.Fprintf(&b, "\n\nDeprecated: %s", s.Deprecated)
	}
	if s.Sensitive {
		b.WriteString("\n\nSensitive value")
	}
	return b.String()
}

func RichHoverMarkdown(a *Analysis, s LanguageSymbol, src []byte) string {
	if s.Kind == SymbolRuntime {
		return richRuntimeHover(a, s, src)
	}
	if s.Kind == SymbolFunction {
		return richFunctionHover(a, s, src)
	}
	if s.Kind == SymbolBlock || s.Kind == SymbolSet {
		switch s.Detail {
		case "connection":
			return richConnectionHover(a, s, src)
		case "pipeline":
			return richPipelineHover(a, s, src)
		case "step":
			return richStepHover(a, s, src)
		case "http":
			return richHTTPHover(a, s, src)
		default:
			return richGenericBlockHover(a, s, src)
		}
	}
	if s.Kind == SymbolAssignment {
		return richAssignmentHover(a, s, src)
	}
	return richFallbackHover(a, s, src)
}

func richFallbackHover(a *Analysis, s LanguageSymbol, src []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### BCL %s `%s`\n\n", s.Kind, s.Name)
	if s.Detail != "" {
		fmt.Fprintf(&b, "**Type:** `%s`\n\n", s.Detail)
	}
	if s.Container != "" {
		fmt.Fprintf(&b, "**Scope:** `%s`\n\n", s.Container)
	}
	if snippet := sourceSnippet(src, s.Span); snippet != "" {
		b.WriteString("**Definition**\n\n")
		fmt.Fprintf(&b, "```bcl\n%s\n```\n\n", snippet)
	}
	if live := symbolLiveStructure(s); live != "" {
		b.WriteString("**Live structure**\n\n")
		b.WriteString(live)
		b.WriteString("\n\n")
	}
	b.WriteString("**What it does**\n\n")
	b.WriteString(symbolBehavior(s))
	b.WriteString("\n\n")
	b.WriteString("**How BCL evaluates it**\n\n")
	b.WriteString(symbolEvaluation(a, s))
	b.WriteString("\n\n")
	if params := symbolInputs(a, s); params != "" {
		b.WriteString("**Request / input parameters**\n\n")
		b.WriteString(params)
		b.WriteString("\n\n")
	}
	if output := symbolOutput(a, s); output != "" {
		b.WriteString("**Output / result**\n\n")
		b.WriteString(output)
		b.WriteString("\n\n")
	}
	if len(a.Diagnostics) > 0 {
		b.WriteString("**Realtime diagnostics**\n\n")
		for _, d := range diagnosticsForSpan(a.Diagnostics, s.Span) {
			fmt.Fprintf(&b, "- `%s`: %s\n", d.Severity, d.Message)
		}
		if len(diagnosticsForSpan(a.Diagnostics, s.Span)) == 0 {
			b.WriteString("- No diagnostics for this symbol.\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("[Compile current file](command:bcl.compileCurrentFile) | [Explain current file](command:bcl.explainCurrentFile) | [Restart BCL LSP](command:bcl.restartLanguageServer)")
	return b.String()
}

func tokenEditorType(tok token) string {
	return tokenEditorTypeAt([]token{tok}, 0)
}

func tokenEditorTypeAt(toks []token, i int) string {
	tok := toks[i]
	switch tok.kind {
	case tokIdent:
		if isKeyword(tok.text) {
			return "keyword"
		}
		if isExprOperator(tok.text) {
			return "operator"
		}
		if nextSignificantToken(toks, i).kind == tokLParen || isBuiltinFunction(tok.text) {
			return "function"
		}
		if i == 0 || previousSignificantToken(toks, i).kind == tokNewline || previousSignificantToken(toks, i).kind == tokLBrace {
			return "property"
		}
		return "identifier"
	case tokString, tokHeredoc:
		return "string"
	case tokNumber:
		return "number"
	case tokOperator:
		return "operator"
	default:
		return "punctuation"
	}
}

func previousSignificantToken(toks []token, i int) token {
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != tokNewline {
			return toks[j]
		}
	}
	return token{kind: tokEOF}
}

func nextSignificantToken(toks []token, i int) token {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].kind != tokNewline {
			return toks[j]
		}
	}
	return token{kind: tokEOF}
}

func isBuiltinFunction(s string) bool {
	_, ok := builtinFunctionHint(s)
	return ok
}

func analyzeNodes(nodes []Node, container string, a *Analysis) []LanguageSymbol {
	out := make([]LanguageSymbol, 0, len(nodes))
	for _, n := range nodes {
		switch x := n.(type) {
		case *ImportDecl:
			name := x.Path
			if x.Alias != "" {
				name = x.Alias
			}
			s := LanguageSymbol{Name: name, Detail: x.Path, Kind: SymbolImport, Span: x.Span, SelectionSpan: x.Span, Container: container}
			out = append(out, s)
			a.Imports = append(a.Imports, s)
			if x.Alias != "" {
				a.Declarations[x.Alias] = s
			}
		case *ConstDecl:
			value, kind, resolved := assignmentValueSummary(x.Value)
			s := LanguageSymbol{Name: x.Name, Detail: valueDetail(x.Value), Kind: SymbolConst, Span: x.Span, SelectionSpan: x.Span, Container: container, Value: value, ValueKind: kind, ResolvedTarget: resolved, ReferencedTargets: collectReferenceTargets(x.Value)}
			out = append(out, s)
			a.Constants[x.Name] = s
			a.Declarations[x.Name] = s
			analyzeValue(x.Value, a)
		case *TypeDecl:
			s := LanguageSymbol{Name: x.Name, Detail: x.Type, Kind: SymbolType, Span: x.Span, SelectionSpan: x.Span, Container: container}
			out = append(out, s)
			a.Types[x.Name] = s
			a.Declarations[x.Name] = s
		case *ParamDecl:
			s := LanguageSymbol{Name: x.Name, Detail: x.Type, Kind: SymbolParam, Span: x.Span, SelectionSpan: x.Span, Container: container}
			out = append(out, s)
			a.Declarations["param."+x.Name] = s
			a.Declarations[x.Name] = s
			if x.Default != nil {
				analyzeValue(x.Default, a)
			}
		case *SchemaDecl:
			s := LanguageSymbol{Name: x.Name, Detail: "schema", Kind: SymbolSchema, Span: x.Span, SelectionSpan: x.Span, Container: container}
			for _, f := range x.Fields {
				s.Children = append(s.Children, schemaFieldSymbol(f, x.Name))
			}
			out = append(out, s)
			a.Schemas[x.Name] = s
			a.Declarations[x.Name] = s
		case *Block:
			name := x.Type
			if x.ID != "" {
				name = x.Type + "." + x.ID
			}
			kind := SymbolBlock
			if x.Type == "set" {
				kind = SymbolSet
			}
			s := LanguageSymbol{Name: name, Detail: x.Type, Kind: kind, Span: x.Span, SelectionSpan: x.Span, Container: container}
			s.Children = analyzeNodes(x.Body, name, a)
			s.ReferencedTargets = symbolReferencedTargets(s)
			out = append(out, s)
			a.Declarations[name] = s
			if x.ID != "" {
				a.Declarations[x.ID] = s
			}
			if x.Type == "set" && x.ID != "" {
				a.Sets[x.ID] = s
			}
		case *Assignment:
			value, kind, resolved := assignmentValueSummary(x.Value)
			s := LanguageSymbol{Name: x.Name, Detail: valueDetail(x.Value), Kind: SymbolAssignment, Span: x.Span, SelectionSpan: x.Span, Container: container, Value: value, ValueKind: kind, ResolvedTarget: resolved, ReferencedTargets: collectReferenceTargets(x.Value), Sensitive: x.Sensitive}
			if obj, ok := x.Value.(*Object); ok {
				s.Children = analyzeNodes(obj.Fields, joinPath(container, x.Name), a)
				s.ReferencedTargets = mergeReferenceTargets(s.ReferencedTargets, symbolReferencedTargets(s))
			} else {
				analyzeValue(x.Value, a)
			}
			out = append(out, s)
		}
	}
	return out
}

func schemaFieldSymbol(f SchemaField, container string) LanguageSymbol {
	s := LanguageSymbol{Name: f.Name, Detail: f.Type, Kind: SymbolField, Span: f.Span, SelectionSpan: f.Span, Container: container, Deprecated: f.Deprecated, Sensitive: f.Sensitive}
	for _, child := range f.Fields {
		s.Children = append(s.Children, schemaFieldSymbol(child, f.Name))
	}
	return s
}

func analyzeValue(v Value, a *Analysis) {
	switch x := v.(type) {
	case *Reference:
		if x.Path != "" {
			a.References = append(a.References, ReferenceUse{Name: x.Path, Kind: "reference", Span: x.Span})
		}
	case *List:
		for _, item := range x.Items {
			analyzeValue(item, a)
		}
	case *Object:
		a.Symbols = append(a.Symbols, analyzeNodes(x.Fields, "", a)...)
	case *Call:
		if _, ok := builtinFunctionHint(x.Name); ok {
			a.References = append(a.References, ReferenceUse{Name: x.Name, Kind: "function", Span: x.Span})
		}
		for _, arg := range x.Args {
			analyzeValue(arg, a)
		}
		if x.Name == "set" && len(x.Args) == 1 {
			if lit, ok := x.Args[0].(*Literal); ok {
				if s, ok := lit.Data.(string); ok {
					a.References = append(a.References, ReferenceUse{Name: s, Kind: "set", Span: lit.Span})
				}
			}
		}
	case *Condition:
		if x.Expr != nil {
			analyzeValue(x.Expr, a)
		}
		for _, child := range x.Children {
			analyzeValue(child, a)
		}
	}
}

func collectReferenceTargets(v Value) []string {
	var out []string
	var walk func(Value)
	walk = func(v Value) {
		switch x := v.(type) {
		case *Reference:
			if x.Path != "" {
				out = append(out, x.Path)
			}
		case *List:
			for _, item := range x.Items {
				walk(item)
			}
		case *Object:
			for _, item := range x.Fields {
				if a, ok := item.(*Assignment); ok {
					walk(a.Value)
				}
			}
		case *Call:
			if x.Name == "set" && len(x.Args) == 1 {
				if lit, ok := x.Args[0].(*Literal); ok {
					if s, ok := lit.Data.(string); ok && s != "" {
						out = append(out, s)
					}
				}
			}
			for _, arg := range x.Args {
				walk(arg)
			}
		case *Condition:
			if x.Expr != nil {
				walk(x.Expr)
			}
			for _, child := range x.Children {
				walk(child)
			}
		}
	}
	walk(v)
	return uniqueStrings(out)
}

func analysisCompletions(a *Analysis) []Completion {
	out := DefaultCompletions()
	add := func(label, kind, detail string) {
		out = append(out, Completion{Label: label, Kind: kind, Detail: detail})
	}
	for _, name := range decisionBlockNames {
		add(name, "keyword", "Decision authoring block")
	}
	for _, name := range decisionFieldNames {
		add(name, "field", "Decision authoring field")
	}
	for _, name := range patternHelperNames {
		add(name, "function", "Pattern matching helper")
	}
	for name, s := range a.Constants {
		add(name, "constant", s.Detail)
	}
	for name, s := range a.Sets {
		add(name, "set", s.Detail)
	}
	for name, s := range a.Schemas {
		add(name, "schema", s.Detail)
		for _, child := range s.Children {
			add(child.Name, "field", child.Detail)
		}
	}
	for name, s := range a.Types {
		add(name, "type", s.Detail)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return dedupeCompletions(out)
}

func buildLanguageFeatureIndex(file string, a *Analysis) WorkspaceIndex {
	idx := WorkspaceIndex{Files: map[string]*Analysis{}, ReverseDependencies: map[string][]string{}}
	if file != "" {
		idx.Files[file] = a
	}
	for _, sym := range flattenSymbols(a.Symbols) {
		canon := sym.Name
		if canon == "" {
			continue
		}
		decl := Declaration{
			CanonicalName: canon,
			DisplayName:   canon,
			LocalName:     localSymbolName(sym),
			Kind:          sym.Kind,
			File:          symbolFile(file, sym.Span),
			Span:          sym.Span,
			SelectionSpan: sym.SelectionSpan,
			Container:     sym.Container,
			Metadata:      symbolMetadata(sym),
			RenameSafe:    renameSafeSymbol(sym),
		}
		idx.Declarations = append(idx.Declarations, decl)
	}
	for _, ref := range a.References {
		idx.References = append(idx.References, LanguageReference{
			TargetCanonicalName: ref.Name,
			Text:                ref.Name,
			Role:                ref.Kind,
			File:                symbolFile(file, ref.Span),
			Span:                ref.Span,
			RenameSafe:          renameSafeReference(ref),
		})
	}
	sort.Slice(idx.Declarations, func(i, j int) bool {
		return idx.Declarations[i].CanonicalName < idx.Declarations[j].CanonicalName
	})
	sort.Slice(idx.References, func(i, j int) bool {
		if idx.References[i].TargetCanonicalName == idx.References[j].TargetCanonicalName {
			return idx.References[i].Span.Start.Offset < idx.References[j].Span.Start.Offset
		}
		return idx.References[i].TargetCanonicalName < idx.References[j].TargetCanonicalName
	})
	return idx
}

func localSymbolName(s LanguageSymbol) string {
	if s.Detail != "" && strings.HasPrefix(s.Name, s.Detail+".") {
		return strings.TrimPrefix(s.Name, s.Detail+".")
	}
	return s.Name
}

func symbolFile(fallback string, sp Span) string {
	if sp.File != "" {
		return sp.File
	}
	return fallback
}

func symbolMetadata(s LanguageSymbol) map[string]string {
	m := map[string]string{}
	if s.Detail != "" {
		m["detail"] = s.Detail
	}
	if s.ValueKind != "" {
		m["value_kind"] = s.ValueKind
	}
	if s.Value != "" {
		m["value"] = s.Value
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func renameSafeSymbol(s LanguageSymbol) bool {
	switch s.Kind {
	case SymbolConst, SymbolParam, SymbolSet, SymbolType, SymbolBlock, SymbolSchema:
		return true
	default:
		return false
	}
}

func renameSafeReference(r ReferenceUse) bool {
	return r.Kind == "reference" || r.Kind == "set"
}

func dedupeCompletions(in []Completion) []Completion {
	seen := map[string]bool{}
	out := make([]Completion, 0, len(in))
	for _, c := range in {
		key := c.Label + "\x00" + c.Kind
		if c.Label == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

func lineText(src []byte, line int) string {
	if line <= 0 {
		return ""
	}
	lines := strings.Split(string(src), "\n")
	if line > len(lines) {
		return ""
	}
	return lines[line-1]
}

func leadingIdentifier(s string) string {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return ""
	}
	first := strings.Trim(fields[0], `"`)
	if first == "" || strings.ContainsAny(first, "{}[](),") {
		return ""
	}
	return first
}

func dottedPrefix(s string) string {
	s = strings.TrimSpace(s)
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if !(c == '.' || c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') {
			return s[i+1:]
		}
	}
	return s
}

func enclosingBlockName(src []byte, line int) string {
	lines := strings.Split(string(src), "\n")
	depth := 0
	stack := []string{}
	for i := 0; i < len(lines) && i < line; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.Contains(trimmed, "}") {
			for n := strings.Count(trimmed, "}"); n > 0 && len(stack) > 0; n-- {
				stack = stack[:len(stack)-1]
			}
			depth -= strings.Count(trimmed, "}")
			if depth < 0 {
				depth = 0
			}
		}
		if strings.Contains(trimmed, "{") {
			name := leadingIdentifier(trimmed)
			if name != "" {
				stack = append(stack, name)
			}
			depth += strings.Count(trimmed, "{")
		}
	}
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

func expectedValuesForContext(ctx CompletionContext) []string {
	switch ctx.AssignmentName {
	case "effect", "decision", "default":
		return []string{"allow", "deny", "require_review"}
	case "hit_policy":
		return []string{"first", "priority", "collect", "unique"}
	case "strategy":
		return []string{"first_match", "highest_priority", "collect_all", "allow_overrides", "deny_overrides", "all_must_pass"}
	case "phase":
		return []string{"validate", "guard", "score", "decide", "notify"}
	case "status":
		return []string{"active", "inactive", "draft", "approved"}
	case "stage":
		return []string{"dev", "test", "staging", "production"}
	case "diagnostics":
		return []string{"none"}
	case "type", "kind":
		if ctx.EnclosingBlock == "step" {
			return []string{"task", "decision", "action", "terminal"}
		}
		return []string{"http", "file", "command", "json", "form", "text", "raw"}
	case "required":
		return []string{"true", "false"}
	default:
		return nil
	}
}

func flattenSymbols(in []LanguageSymbol) []LanguageSymbol {
	var out []LanguageSymbol
	var walk func([]LanguageSymbol)
	walk = func(xs []LanguageSymbol) {
		for _, s := range xs {
			out = append(out, s)
			walk(s.Children)
		}
	}
	walk(in)
	return out
}

func valueDetail(v Value) string {
	if v == nil {
		return ""
	}
	return v.Kind()
}

func assignmentValueSummary(v Value) (value string, kind string, resolved string) {
	if v == nil {
		return "", "", ""
	}
	kind = v.Kind()
	switch x := v.(type) {
	case *Reference:
		return x.Path, kind, x.Path
	case *Literal:
		return literalScalar(x), kind, ""
	case *Expr:
		return x.Raw, kind, ""
	case *Call:
		args := make([]string, 0, len(x.Args))
		for _, arg := range x.Args {
			v, _, _ := assignmentValueSummary(arg)
			args = append(args, v)
		}
		return x.Name + "(" + strings.Join(args, ", ") + ")", kind, ""
	case *List:
		items := make([]string, 0, len(x.Items))
		for _, item := range x.Items {
			v, _, _ := assignmentValueSummary(item)
			items = append(items, v)
		}
		return "[" + strings.Join(items, ", ") + "]", kind, ""
	case *Object:
		return "object", kind, ""
	case *Condition:
		if x.Expr != nil {
			return x.Expr.Raw, "condition", ""
		}
		return x.Op + " condition", "condition", ""
	default:
		return kind, kind, ""
	}
}

func emptyDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

var keywordHints = []hintInfo{
	{Name: "import", Signature: `import "./file.bcl" [as alias]`, Description: "Loads another BCL file into the current compilation unit. Use `as` to place imported content under a namespace."},
	{Name: "bcl", Signature: `bcl { version "1.0" }`, Description: "Declares document-level BCL metadata such as language version and strict mode."},
	{Name: "const", Signature: `const NAME = value`, Description: "Declares a reusable constant available to later references."},
	{Name: "schema", Signature: `schema name { required field string }`, Description: "Defines validation rules for blocks with the same type name."},
	{Name: "type", Signature: `type Name = string`, Description: "Declares a type alias used by schemas and parameters."},
	{Name: "param", Signature: `param name string { required true }`, Description: "Declares an input contract for modules."},
	{Name: "profile", Signature: `profile "name" { ... }`, Description: "Defines environment-specific settings and scoped overrides."},
	{Name: "override", Signature: `override target { ... }`, Description: "Overrides fields on an existing block, often inside a profile."},
	{Name: "module", Signature: `module "name" { source "./module" }`, Description: "Loads reusable BCL content from a module source directory."},
	{Name: "namespace", Signature: `namespace name { ... }`, Description: "Groups declarations and blocks under a named namespace."},
	{Name: "runtime", Signature: `runtime { ... }`, Description: "Configures runtime behavior and sandbox settings."},
	{Name: "evaluation", Signature: `evaluation { ... }`, Description: "Configures policy/rule evaluation behavior."},
	{Name: "audit", Signature: `audit { ... }`, Description: "Configures audit output and redaction fields."},
	{Name: "context", Signature: `context { ... }`, Description: "Declares values read from host-provided context."},
	{Name: "session", Signature: `session { ... }`, Description: "Declares values read from host-provided session data."},
	{Name: "policy", Signature: `policy "name" { effect allow }`, Description: "Defines a policy with effect, priority, conditions, and actions."},
	{Name: "rule", Signature: `rule "name" { when { ... } then { ... } }`, Description: "Defines reusable rule logic with conditions and outcomes."},
	{Name: "set", Signature: `set "name" { item }`, Description: "Defines a reusable collection consumed by `set(\"name\")`."},
	{Name: "predicate", Signature: `predicate "name" { all { ... } }`, Description: "Defines a reusable boolean condition."},
	{Name: "test", Signature: `test "name" { input { ... } expect { ... } }`, Description: "Defines an executable BCL test fixture."},
	{Name: "pipeline", Signature: `pipeline "name" { step "id" { ... } }`, Description: "Defines a workflow graph made of steps and connections."},
	{Name: "step", Signature: `step "id" { kind task }`, Description: "Defines a workflow state such as task, decision, action, or terminal."},
	{Name: "connection", Signature: `connection "id" { from step.a to step.b }`, Description: "Connects workflow steps with transition metadata."},
	{Name: "http", Signature: `http "name" { base_url "https://..." }`, Description: "Defines an HTTP integration configuration."},
	{Name: "connector", Signature: `connector "name" { type http }`, Description: "Defines an external connector surface."},
	{Name: "source", Signature: `source "name" { type http }`, Description: "Defines an external data source integration."},
	{Name: "action", Signature: `action "name" { type http }`, Description: "Defines an executable integration action."},
	{Name: "file", Signature: `file "name" { path "./out" mode write }`, Description: "Defines file access configuration."},
	{Name: "command", Signature: `command "name" { exec [...] }`, Description: "Defines command execution configuration."},
	{Name: "output", Signature: `output { fields [...] }`, Description: "Defines export shape, field filtering, and redaction."},
	{Name: "when", Signature: `when { condition }`, Description: "Introduces a condition block or expression."},
	{Name: "then", Signature: `then { emit "event" }`, Description: "Introduces outcomes for matched rules or workflow steps."},
	{Name: "all", Signature: `all { ... }`, Description: "Requires every child condition to match."},
	{Name: "any", Signature: `any { ... }`, Description: "Requires at least one child condition to match."},
	{Name: "not", Signature: `not { ... }`, Description: "Negates a child condition."},
	{Name: "none", Signature: `none`, Description: "Represents no diagnostics or no matches, depending on context."},
}

var decisionBlockNames = []string{
	"decision_schema", "decision_table", "rule_set", "ranking", "dataset", "reason_code_catalog",
	"decision_bundle", "decision_release", "gate", "test_matrix", "rule_template", "row", "record",
	"case", "outcome", "obligation", "advice", "event", "approval", "governance",
}

var decisionFieldNames = []string{
	"effects", "hit_policy", "strategy", "phase", "status", "effective_from", "effective_until",
	"owner", "rationale", "reason", "reason_code", "tags", "actions", "resources", "score",
	"attributes", "metadata", "bundle", "bundles", "decision", "decisions", "dataset", "datasets",
	"tests", "release", "stage", "min_pass_rate", "max_diagnostics", "no_default_only", "required_rules",
	"approved_by", "approved_at", "approved", "allowed", "matched_rules", "selected_rules",
}

var patternHelperNames = []string{"match", "case", "MISSING", "NULL", "EXISTS", "ANY"}

var builtinFunctionHints = []hintInfo{
	{Name: "env", Signature: `env(name, default?)`, Description: "Reads an environment variable. Returns the optional default when the variable is absent.", InsertText: "env($1)", Examples: []string{`env("APP_ENV", "dev")`}},
	{Name: "env.required", Signature: `env.required(name)`, Description: "Reads an environment variable and fails validation/evaluation when it is missing.", InsertText: "env.required($1)", Examples: []string{`env.required("DATABASE_URL")`}},
	{Name: "env.int", Signature: `env.int(name, default?)`, Description: "Reads an environment variable and converts it to an integer.", InsertText: "env.int($1)", Examples: []string{`env.int("WORKERS", 8)`}},
	{Name: "env.bool", Signature: `env.bool(name, default?)`, Description: "Reads an environment variable and converts it to a boolean.", InsertText: "env.bool($1)", Examples: []string{`env.bool("DEBUG", false)`}},
	{Name: "env.duration", Signature: `env.duration(name, default?)`, Description: "Reads an environment variable and converts it to a duration.", InsertText: "env.duration($1)", Examples: []string{`env.duration("CACHE_TTL", 5m)`}},
	{Name: "context", Signature: `context(path, default?)`, Description: "Reads a value from host-provided context using a dotted path.", InsertText: "context($1)", Examples: []string{`context("request.id", "unknown")`}},
	{Name: "context.required", Signature: `context.required(path)`, Description: "Reads a required context value and fails when absent.", InsertText: "context.required($1)", Examples: []string{`context.required("request.id")`}},
	{Name: "context.float", Signature: `context.float(path, default?)`, Description: "Reads a context value as a floating-point number.", InsertText: "context.float($1)", Examples: []string{`context.float("request.score", 0)`}},
	{Name: "context.list", Signature: `context.list(path, separator?)`, Description: "Reads a context value as a list, optionally splitting a string value.", InsertText: "context.list($1)", Examples: []string{`context.list("request.flags", ",")`}},
	{Name: "session", Signature: `session(path, default?)`, Description: "Reads a value from host-provided session data.", InsertText: "session($1)", Examples: []string{`session("id", "anonymous")`}},
	{Name: "session.required", Signature: `session.required(path)`, Description: "Reads a required session value and fails when absent.", InsertText: "session.required($1)", Examples: []string{`session.required("subject.id")`}},
	{Name: "session.bool", Signature: `session.bool(path, default?)`, Description: "Reads a session value as a boolean.", InsertText: "session.bool($1)", Examples: []string{`session.bool("attrs.mfa", false)`}},
	{Name: "session.duration", Signature: `session.duration(path, default?)`, Description: "Reads a session value as a duration.", InsertText: "session.duration($1)", Examples: []string{`session.duration("expires_in", 30m)`}},
	{Name: "ref", Signature: `ref(target)`, Description: "Creates an explicit reference to another BCL declaration or block.", InsertText: "ref($1)"},
	{Name: "set", Signature: `set(name)`, Description: "Loads values from a named `set` block.", InsertText: "set($1)", Examples: []string{`set("admin-roles")`}},
	{Name: "sensitive", Signature: `sensitive(value)`, Description: "Marks a value for redaction in exported output and hover summaries.", InsertText: "sensitive($1)"},
	{Name: "concat", Signature: `concat(values...)`, Description: "Concatenates values into a string.", InsertText: "concat($1)"},
	{Name: "upper", Signature: `upper(value)`, Description: "Converts a string to uppercase.", InsertText: "upper($1)"},
	{Name: "lower", Signature: `lower(value)`, Description: "Converts a string to lowercase.", InsertText: "lower($1)"},
	{Name: "contains", Signature: `contains(collection, value)`, Description: "Checks whether a string or collection contains a value.", InsertText: "contains($1)"},
	{Name: "exists", Signature: `exists(value)`, Description: "Checks whether a runtime value is present.", InsertText: "exists($1)"},
	{Name: "hash", Signature: `hash(value)`, Description: "Computes a hash for a value when hash support is enabled.", InsertText: "hash($1)"},
	{Name: "base64", Signature: `base64(value)`, Description: "Encodes a value as Base64 when encoding support is enabled.", InsertText: "base64($1)"},
	{Name: "cidr", Signature: `cidr(value)`, Description: "Treats a string as a CIDR/network value.", InsertText: "cidr($1)"},
	{Name: "email", Signature: `email(value)`, Description: "Treats a string as an email value.", InsertText: "email($1)"},
	{Name: "url", Signature: `url(value)`, Description: "Treats a string as a URL value.", InsertText: "url($1)"},
	{Name: "regex", Signature: `regex(pattern)`, Description: "Compiles a regular expression pattern for matching.", InsertText: "regex($1)"},
	{Name: "match", Signature: `match(value, cases..., default)`, Description: "Matches a value against typed BCL patterns.", InsertText: "match($1)"},
	{Name: "case", Signature: `case(pattern, result)`, Description: "Defines one branch inside a pattern match expression.", InsertText: "case($1)"},
	{Name: "MISSING", Signature: `MISSING`, Description: "Pattern helper that matches a missing object field.", InsertText: "MISSING"},
	{Name: "NULL", Signature: `NULL`, Description: "Pattern helper that matches an explicit null value.", InsertText: "NULL"},
	{Name: "EXISTS", Signature: `EXISTS(pattern?)`, Description: "Pattern helper that requires a value or collection item to exist.", InsertText: "EXISTS($1)"},
	{Name: "ANY", Signature: `ANY(pattern?)`, Description: "Pattern helper that matches any compatible value.", InsertText: "ANY($1)"},
}

var runtimeValueHints = []hintInfo{
	{Name: "time.now", Signature: "timestamp", Description: "Current evaluation timestamp supplied by the host runtime. Use it for audit fields, generated metadata, and time-based decisions.", Examples: []string{"time time.now"}},
	{Name: "decision.effect", Signature: "string", Description: "Final decision effect produced by policy or rule evaluation, commonly `allow` or `deny`.", Examples: []string{"decision decision.effect"}},
	{Name: "decision.reason", Signature: "string", Description: "Human-readable decision reason or denial explanation supplied by the evaluator."},
	{Name: "decision.matched_rule", Signature: "string", Description: "Identifier of the rule or policy that determined the current decision."},
	{Name: "request.id", Signature: "string", Description: "Stable request identifier supplied by the host application."},
	{Name: "request.path", Signature: "string", Description: "Request path used in routing, policy, and integration conditions."},
	{Name: "request.method", Signature: "string", Description: "HTTP method or operation name for the current request."},
	{Name: "request.ip", Signature: "string", Description: "Client or caller IP address supplied by the host application."},
	{Name: "request.tenant_id", Signature: "string", Description: "Tenant identifier attached to the current request."},
	{Name: "request.amount", Signature: "number", Description: "Request amount or transaction value supplied by domain-specific input."},
	{Name: "request.risk_score", Signature: "number", Description: "Risk score supplied by prior evaluation or external risk systems."},
	{Name: "request.geo.country", Signature: "string", Description: "Country code inferred or supplied for the request location."},
	{Name: "subject.id", Signature: "string", Description: "Identifier for the user, principal, or entity being evaluated."},
	{Name: "subject.tenant", Signature: "string", Description: "Tenant associated with the current subject or principal."},
	{Name: "subject.roles", Signature: "list<string>", Description: "Roles assigned to the current subject."},
	{Name: "subject.email", Signature: "string", Description: "Email address for the current subject."},
	{Name: "subject.status", Signature: "string", Description: "Lifecycle/status value for the subject, such as active or blocked."},
	{Name: "subject.allowed_countries", Signature: "list<string>", Description: "Countries the subject is allowed to operate from."},
	{Name: "subject.clearance", Signature: "string", Description: "Subject clearance level used by policy checks."},
	{Name: "resource.id", Signature: "string", Description: "Identifier for the resource being accessed or modified."},
	{Name: "resource.owner_id", Signature: "string", Description: "Owner identifier for the target resource."},
	{Name: "context.request.id", Signature: "string", Description: "Request identifier nested under generic host context."},
	{Name: "context.request.path", Signature: "string", Description: "Request path nested under generic host context."},
	{Name: "context.request.score", Signature: "number", Description: "Numeric score nested under generic host context."},
	{Name: "context.request.flags", Signature: "list<string>", Description: "Flags attached to the request in host context."},
	{Name: "session.id", Signature: "string", Description: "Current session identifier."},
	{Name: "session.attrs.mfa", Signature: "bool", Description: "Whether the current session has satisfied MFA."},
	{Name: "session.subject.id", Signature: "string", Description: "Subject identifier stored inside session data."},
	{Name: "body.id", Signature: "any", Description: "Field read from an integration response body."},
	{Name: "body.email", Signature: "string", Description: "Email field read from an integration response body."},
	{Name: "body.roles", Signature: "list<string>", Description: "Roles field read from an integration response body."},
	{Name: "body.attributes", Signature: "map", Description: "Attributes object read from an integration response body."},
	{Name: "app.name", Signature: "string", Description: "Application name supplied by host context or config."},
	{Name: "applicant.name", Signature: "string", Description: "Applicant display name supplied by workflow input or host data."},
}

func runtimeHint(name string) (hintInfo, bool) {
	for _, item := range runtimeValueHints {
		if item.Name == name {
			return item, true
		}
	}
	root, _, dotted := strings.Cut(name, ".")
	if !dotted {
		return hintInfo{}, false
	}
	switch root {
	case "time":
		return hintInfo{Name: name, Signature: "runtime time value", Description: "Time value supplied by the host runtime."}, true
	case "decision":
		return hintInfo{Name: name, Signature: "runtime decision value", Description: "Decision metadata supplied by policy or rule evaluation."}, true
	case "request":
		return hintInfo{Name: name, Signature: "runtime request value", Description: "Request-scoped value supplied by the host application."}, true
	case "subject":
		return hintInfo{Name: name, Signature: "runtime subject value", Description: "Subject/principal value supplied by the host application."}, true
	case "resource":
		return hintInfo{Name: name, Signature: "runtime resource value", Description: "Resource-scoped value supplied by the host application."}, true
	case "context":
		return hintInfo{Name: name, Signature: "runtime context value", Description: "Context value supplied by the host application."}, true
	case "session":
		return hintInfo{Name: name, Signature: "runtime session value", Description: "Session value supplied by the host application."}, true
	case "body":
		return hintInfo{Name: name, Signature: "integration body value", Description: "Value read from an integration response body."}, true
	case "app", "applicant":
		return hintInfo{Name: name, Signature: "domain input value", Description: "Domain-specific value supplied by input, context, or the host application."}, true
	default:
		return hintInfo{}, false
	}
}

func builtinFunctionHint(name string) (hintInfo, bool) {
	for _, item := range builtinFunctionHints {
		if item.Name == name {
			return item, true
		}
	}
	return hintInfo{}, false
}

func builtinFunctionSymbol(name string, sp Span) (LanguageSymbol, bool) {
	info, ok := builtinFunctionHint(name)
	if !ok {
		return LanguageSymbol{}, false
	}
	return LanguageSymbol{Name: name, Detail: info.Signature, Kind: SymbolFunction, Span: sp, SelectionSpan: sp, Value: info.Signature, ValueKind: "function"}, true
}

func runtimeReferenceSymbol(name string, sp Span) (LanguageSymbol, bool) {
	info, ok := runtimeHint(name)
	if !ok {
		return LanguageSymbol{}, false
	}
	return LanguageSymbol{Name: name, Detail: info.Signature, Kind: SymbolRuntime, Span: sp, SelectionSpan: sp, Value: name, ValueKind: "runtime", ReferencedTargets: []string{name}}, true
}

func hasDeclaredBlockType(a *Analysis, typ string) bool {
	prefix := typ + "."
	for name, sym := range a.Declarations {
		if strings.HasPrefix(name, prefix) && (sym.Kind == SymbolBlock || sym.Kind == SymbolSet) {
			return true
		}
	}
	return false
}

func symbolBehavior(s LanguageSymbol) string {
	switch s.Kind {
	case SymbolRuntime:
		if info, ok := runtimeHint(s.Name); ok {
			return info.Description
		}
		return "Runtime value supplied by the host application."
	case SymbolFunction:
		if info, ok := builtinFunctionHint(s.Name); ok {
			return info.Description
		}
		return "BCL built-in function."
	case SymbolConst:
		return "Defines a reusable constant. References to this name are resolved during validation and compilation."
	case SymbolSchema:
		return "Defines the expected fields and types for blocks with the same name."
	case SymbolSet:
		return "Defines a reusable set. Calls such as `set(\"name\")` use this block as a collection value."
	case SymbolBlock:
		switch s.Detail {
		case "pipeline":
			return "Defines a workflow graph. `entrypoint` selects the first step, and `connection` blocks describe step transitions."
		case "step":
			return "Defines a workflow step. `when` controls matching, and `then` describes emitted actions or events."
		case "connection":
			return "Connects workflow steps with `from`, `to`, and `on` fields."
		case "http":
			return "Defines an HTTP integration. Request and response child blocks describe method, payload, headers, and expected status."
		}
		return "Defines a named BCL block. The compiler normalizes its body into the JSON-compatible AST."
	case SymbolAssignment:
		return "Assigns a value inside the current block or object. The parser preserves its source span for diagnostics and navigation."
	case SymbolField:
		return "Declares a schema field. Validation uses this to check required values, type shape, enums, and constraints."
	case SymbolImport:
		return "Imports another BCL file or module into the current document."
	default:
		return "Represents a BCL language symbol at the current cursor position."
	}
}

func symbolLiveStructure(s LanguageSymbol) string {
	if len(s.Children) == 0 {
		return ""
	}
	var assignments, blocks, steps, connections []string
	for _, child := range s.Children {
		switch child.Kind {
		case SymbolAssignment:
			if child.Value != "" {
				assignments = append(assignments, fmt.Sprintf("`%s = %s` (%s)", child.Name, child.Value, emptyDefault(child.ValueKind, child.Detail)))
			} else {
				assignments = append(assignments, fmt.Sprintf("`%s` (%s)", child.Name, emptyDefault(child.Detail, "value")))
			}
		case SymbolBlock, SymbolSet:
			switch child.Detail {
			case "step":
				steps = append(steps, "`"+strings.TrimPrefix(child.Name, "step.")+"`")
			case "connection":
				connections = append(connections, "`"+strings.TrimPrefix(child.Name, "connection.")+"`")
			default:
				blocks = append(blocks, fmt.Sprintf("`%s` (%s)", child.Name, emptyDefault(child.Detail, "block")))
			}
		}
	}
	var lines []string
	if len(assignments) > 0 {
		lines = append(lines, "- Fields: "+strings.Join(assignments, ", "))
	}
	if len(steps) > 0 {
		lines = append(lines, "- Workflow steps: "+strings.Join(steps, ", "))
	}
	if len(connections) > 0 {
		lines = append(lines, "- Workflow connections: "+strings.Join(connections, ", "))
	}
	if len(blocks) > 0 {
		lines = append(lines, "- Child blocks: "+strings.Join(blocks, ", "))
	}
	return strings.Join(lines, "\n")
}

func symbolEvaluation(a *Analysis, s LanguageSymbol) string {
	switch s.Kind {
	case SymbolRuntime:
		return "- Resolved at evaluation time from host-provided runtime data.\n- Not required to have a matching BCL declaration.\n- Included in hover and completion metadata from the runtime hint catalog."
	case SymbolFunction:
		return "- Parsed as a built-in function call.\n- Arguments are validated and evaluated according to function semantics.\n- Included in hover and completion metadata from the function hint catalog."
	case SymbolConst:
		return "- Parsed as a constant declaration.\n- Available as a reference completion.\n- Checked for unused/duplicate declarations by lint."
	case SymbolSchema:
		return "- Collected before block validation.\n- Blocks whose type matches this schema are checked field-by-field.\n- Unknown fields are reported in strict mode."
	case SymbolSet:
		return "- Collected as a reusable set.\n- `set(\"" + strings.TrimPrefix(s.Name, "set.") + "\")` records a set use and validates the set exists."
	case SymbolBlock:
		if s.Detail == "pipeline" {
			return "- Builds a step graph from nested `step` and `connection` blocks.\n- Validates the `entrypoint` exists.\n- Reports unreachable workflow steps."
		}
		if s.Detail == "connection" {
			return "- Resolves `from` and `to` references to step IDs.\n- Adds an edge to the workflow graph.\n- Validates both endpoint steps exist."
		}
		return "- Parsed into a block node.\n- Nested assignments and blocks are walked for validation, references, and symbols."
	case SymbolAssignment:
		if s.Detail == "expression" {
			return "- Parsed as an expression.\n- Compiled by the expression VM during validation.\n- Evaluated at runtime with request/context variables."
		}
		return "- Parsed as a literal, reference, object, list, call, or expression.\n- References are resolved against known constants, sets, and blocks."
	case SymbolField:
		return "- Used by schema validation.\n- Required fields must exist.\n- Type, enum, min/max, pattern, and format constraints are checked when present."
	default:
		_ = a
		return "- Indexed for hover, completion, symbols, and navigation."
	}
}

func symbolInputs(a *Analysis, s LanguageSymbol) string {
	var out []string
	switch s.Detail {
	case "pipeline":
		out = append(out, "- `entrypoint`: first workflow step ID")
		out = append(out, "- nested `step`: executable workflow states")
		out = append(out, "- nested `connection`: graph transitions")
	case "step":
		out = append(out, "- optional `when`: match condition using runtime variables")
		out = append(out, "- optional `then`: emitted events/actions")
	case "connection":
		out = append(out, "- `from`: source step reference")
		out = append(out, "- `to`: target step reference")
		out = append(out, "- `on`: transition event")
	case "http":
		out = append(out, "- `base_url`: integration base URL")
		out = append(out, "- `request`: method, path, headers, auth, body")
		out = append(out, "- `response`: expected status/body shape")
	}
	if len(out) == 0 && s.Kind == SymbolAssignment {
		out = append(out, "- Value is read from this assignment during normalization.")
	}
	if len(out) == 0 {
		_ = a
		return ""
	}
	return strings.Join(out, "\n")
}

func symbolOutput(a *Analysis, s LanguageSymbol) string {
	switch s.Detail {
	case "pipeline":
		return "- Normalized workflow block with step graph metadata.\n- Validation diagnostics for missing entrypoint, broken connections, or unreachable steps."
	case "step":
		return "- Step body in normalized output.\n- Runtime match/action result when evaluated by the host application."
	case "connection":
		return "- Directed edge in the workflow graph."
	case "http":
		return "- Structured integration config only. BCL validates the shape but does not execute network side effects."
	}
	if s.Kind == SymbolConst {
		return "- Constant value available to references and normalized output."
	}
	if s.Kind == SymbolSchema {
		return fmt.Sprintf("- Validation contract for `%s` blocks.", s.Name)
	}
	_ = a
	return ""
}

type workflowConnectionFact struct {
	Name       string
	From       string
	To         string
	On         string
	FromExists bool
	ToExists   bool
}

type workflowFacts struct {
	Pipeline    LanguageSymbol
	Entrypoint  string
	Steps       map[string]LanguageSymbol
	Connections []workflowConnectionFact
}

func richPipelineHover(a *Analysis, s LanguageSymbol, src []byte) string {
	facts := workflowFactsForPipeline(s)
	var b strings.Builder
	writeHoverHeader(&b, "pipeline", s, src)
	fmt.Fprintf(&b, "**What it does**\n\nPipeline `%s` defines a workflow graph. It starts at `%s`, evaluates step conditions, and follows connection edges based on transition events.\n\n", trimBlockName(s.Name), emptyDefault(facts.Entrypoint, "(missing entrypoint)"))
	b.WriteString("**Live structure**\n\n")
	if live := symbolLiveStructure(s); live != "" {
		b.WriteString(live)
		b.WriteString("\n\n")
	}
	b.WriteString("**Realtime workflow graph**\n\n")
	if facts.Entrypoint != "" {
		fmt.Fprintf(&b, "- Entrypoint: `%s`\n", facts.Entrypoint)
	}
	if len(facts.Steps) > 0 {
		var steps []string
		for id := range facts.Steps {
			steps = append(steps, "`"+id+"`")
		}
		sort.Strings(steps)
		fmt.Fprintf(&b, "- Steps: %s\n", strings.Join(steps, ", "))
	}
	if len(facts.Connections) > 0 {
		b.WriteString("- Connections:\n")
		for _, c := range facts.Connections {
			fmt.Fprintf(&b, "  - `%s`: `%s -> %s` on `%s`\n", c.Name, c.From, c.To, emptyDefault(c.On, "(default)"))
		}
	}
	b.WriteString("\n**How BCL evaluates it**\n\n")
	b.WriteString("- Parses nested `step` blocks into workflow states.\n- Parses nested `connection` blocks into directed graph edges.\n- Validates the entrypoint, edge endpoints, and reachability in real time from the current editor text.\n")
	b.WriteString("\n**Request / input parameters**\n\n")
	b.WriteString("- `entrypoint`: first step ID for workflow evaluation.\n- nested `step` blocks: workflow states and optional conditions/actions.\n- nested `connection` blocks: transitions with `from`, `to`, and `on` fields.\n")
	b.WriteString("\n**Validation**\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "Pipeline graph is valid for the current document.")
	b.WriteString("\n**Output / result**\n\n- Normalizes into a workflow block consumed by validation/evaluation.\n- Produces a directed graph of steps and transition edges.\n\n")
	writeHoverCommands(&b)
	return b.String()
}

func richConnectionHover(a *Analysis, s LanguageSymbol, src []byte) string {
	parent, facts, _ := workflowFactsForContainer(a, s.Container)
	assigns := blockChildAssignments(s)
	from := valueOr(assigns["from"].Value, assigns["from"].ResolvedTarget)
	to := valueOr(assigns["to"].Value, assigns["to"].ResolvedTarget)
	on := assigns["on"].Value
	if facts.Pipeline.Name != "" {
		for _, c := range facts.Connections {
			if c.Name == trimBlockName(s.Name) {
				from, to, on = c.From, c.To, c.On
				break
			}
		}
	}
	fromID, toID := refTargetIDForHover(from), refTargetIDForHover(to)
	fromExists := facts.Steps == nil || facts.Steps[fromID].Name != ""
	toExists := facts.Steps == nil || facts.Steps[toID].Name != ""
	var b strings.Builder
	writeHoverHeader(&b, "connection", s, src)
	fmt.Fprintf(&b, "**What it does**\n\nConnects `%s` to `%s` when transition event is `%s`.", emptyDefault(from, "(missing from)"), emptyDefault(to, "(missing to)"), emptyDefault(on, "(missing on)"))
	if parent.Name != "" {
		fmt.Fprintf(&b, " This edge belongs to pipeline `%s`.", trimBlockName(parent.Name))
	}
	b.WriteString("\n\n**Parameters**\n\n")
	b.WriteString("| Field | Current value | Meaning |\n|---|---|---|\n")
	pipelineName := trimBlockName(parent.Name)
	fmt.Fprintf(&b, "| `from` | `%s` | %s |\n", emptyDefault(from, "(missing)"), workflowStepParameterMeaning("Source", pipelineName, fromID, facts.Steps[fromID]))
	fmt.Fprintf(&b, "| `to` | `%s` | %s |\n", emptyDefault(to, "(missing)"), workflowStepParameterMeaning("Target", pipelineName, toID, facts.Steps[toID]))
	fmt.Fprintf(&b, "| `on` | `%s` | Transition event required to take this edge. |\n\n", emptyDefault(on, "(missing)"))
	b.WriteString("**Validation**\n\n")
	fmt.Fprintf(&b, "- Source step `%s`: %s\n", emptyDefault(fromID, "(missing)"), existsText(fromExists))
	fmt.Fprintf(&b, "- Target step `%s`: %s\n", emptyDefault(toID, "(missing)"), existsText(toExists))
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this connection.")
	if facts.Steps != nil {
		b.WriteString("\n**Connected step behavior**\n\n")
		if fromStep := facts.Steps[fromID]; fromStep.Name != "" {
			b.WriteString("- Source step:\n")
			writeStepBehaviorBullets(&b, fromStep, "  ")
		} else {
			fmt.Fprintf(&b, "- Source step `%s` is missing, so BCL cannot explain its behavior.\n", emptyDefault(fromID, "(missing)"))
		}
		if toStep := facts.Steps[toID]; toStep.Name != "" {
			b.WriteString("- Target step:\n")
			writeStepBehaviorBullets(&b, toStep, "  ")
		} else {
			fmt.Fprintf(&b, "- Target step `%s` is missing, so BCL cannot explain its behavior.\n", emptyDefault(toID, "(missing)"))
		}
	}
	b.WriteString("\n**How BCL evaluates it**\n\n- Reads `from`, `to`, and `on` from this connection block.\n- Resolves `from` and `to` against sibling `step` IDs in the containing pipeline.\n- Adds the edge only as configuration; no external side effects are executed by hover.\n")
	fmt.Fprintf(&b, "\n**Graph effect**\n\n- Adds directed edge `%s -> %s`", emptyDefault(fromID, "?"), emptyDefault(toID, "?"))
	if on != "" {
		fmt.Fprintf(&b, " for `%s` transitions", on)
	}
	b.WriteString(".\n\n**Output / result**\n\n- Normalizes into a workflow connection record.\n- Used by pipeline validation to detect broken edges and unreachable steps.\n- Used by host evaluation to choose the next workflow step after the source step completes.\n\n")
	writeHoverCommands(&b)
	return b.String()
}

func richStepHover(a *Analysis, s LanguageSymbol, src []byte) string {
	parent, facts, _ := workflowFactsForContainer(a, s.Container)
	assigns := blockChildAssignments(s)
	id := trimBlockName(s.Name)
	kind := assigns["kind"].Value
	then := childBlockNames(s, "then")
	var incoming, outgoing []workflowConnectionFact
	for _, c := range facts.Connections {
		if refTargetIDForHover(c.To) == id {
			incoming = append(incoming, c)
		}
		if refTargetIDForHover(c.From) == id {
			outgoing = append(outgoing, c)
		}
	}
	var b strings.Builder
	writeHoverHeader(&b, "step", s, src)
	fmt.Fprintf(&b, "**What it does**\n\nStep `%s`", id)
	if kind != "" {
		fmt.Fprintf(&b, " is a `%s` step", kind)
	}
	if parent.Name != "" {
		fmt.Fprintf(&b, " inside pipeline `%s`", trimBlockName(parent.Name))
	}
	b.WriteString(".\n\n**Realtime workflow role**\n\n")
	writeConnectionList(&b, "Incoming", incoming)
	writeConnectionList(&b, "Outgoing", outgoing)
	if len(then) > 0 {
		fmt.Fprintf(&b, "- Then blocks/actions: %s\n", strings.Join(then, ", "))
	}
	b.WriteString("\n**Validation**\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this step.")
	b.WriteString("\n**Output / result**\n\n- Normalizes into a workflow step record.\n- Evaluator can use its `when` and `then` body to decide and emit workflow outcomes.\n\n")
	writeHoverCommands(&b)
	return b.String()
}

func richHTTPHover(a *Analysis, s LanguageSymbol, src []byte) string {
	assigns := blockChildAssignments(s)
	var b strings.Builder
	writeHoverHeader(&b, "http integration", s, src)
	fmt.Fprintf(&b, "**What it does**\n\nDefines HTTP integration `%s` with base URL `%s`.\n\n", trimBlockName(s.Name), emptyDefault(assigns["base_url"].Value, "(missing)"))
	b.WriteString("**Parameters**\n\n")
	for _, key := range []string{"base_url", "auth", "proxy", "redirect"} {
		if v := assigns[key].Value; v != "" {
			fmt.Fprintf(&b, "- `%s`: `%s`\n", key, v)
		}
	}
	b.WriteString("\n**Output / result**\n\n- BCL validates the integration shape but does not execute network side effects.\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this HTTP integration.")
	b.WriteString("\n")
	writeHoverCommands(&b)
	return b.String()
}

func richGenericBlockHover(a *Analysis, s LanguageSymbol, src []byte) string {
	assigns := blockChildAssignments(s)
	var b strings.Builder
	writeHoverHeader(&b, string(s.Detail), s, src)
	fmt.Fprintf(&b, "**What it does**\n\n`%s` is a `%s` block. BCL parses it as structured configuration, validates its fields and references, and normalizes it into the compiled JSON-compatible AST.\n\n", s.Name, emptyDefault(s.Detail, "block"))
	if len(assigns) > 0 {
		b.WriteString("**Realtime fields**\n\n")
		b.WriteString("| Field | Current value | Value kind |\n|---|---|---|\n")
		for _, child := range sortedAssignmentSymbols(assigns) {
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s` |\n", child.Name, emptyDefault(child.Value, "(empty)"), emptyDefault(child.ValueKind, child.Detail))
		}
		b.WriteString("\n")
	}
	if children := childBlockSummary(s); children != "" {
		b.WriteString("**Child blocks**\n\n")
		b.WriteString(children)
		b.WriteString("\n\n")
	}
	if refs := symbolReferenceSummary(a, s); refs != "" {
		b.WriteString("**References used here**\n\n")
		b.WriteString(refs)
		b.WriteString("\n\n")
	}
	b.WriteString("**How BCL evaluates it**\n\n")
	b.WriteString("- Reads this block from the current in-memory editor text.\n- Walks assignments and child blocks recursively.\n- Applies schema, reference, workflow, integration, and strict-mode validation when relevant.\n")
	b.WriteString("\n**Validation**\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this block.")
	b.WriteString("\n**Output / result**\n\n- Emits a normalized block entry with its type, ID, fields, and nested body.\n\n")
	writeHoverCommands(&b)
	return b.String()
}

func richAssignmentHover(a *Analysis, s LanguageSymbol, src []byte) string {
	var b strings.Builder
	writeHoverHeader(&b, "assignment", s, src)
	fmt.Fprintf(&b, "**Current value**\n\n- `%s = %s`\n- Value kind: `%s`\n", s.Name, emptyDefault(s.Value, "(empty)"), emptyDefault(s.ValueKind, s.Detail))
	if refs := referenceTargetsForSymbol(s); len(refs) > 0 {
		b.WriteString("\n**Referenced items**\n\n")
		writeReferenceTargetSummaries(&b, a, s, refs)
	}
	b.WriteString("\n**How BCL evaluates it**\n\n- The assignment is parsed from the current in-memory document.\n- Its value is used by the containing block during normalization and validation.\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this assignment.")
	b.WriteString("\n")
	writeHoverCommands(&b)
	return b.String()
}

func richRuntimeHover(a *Analysis, s LanguageSymbol, src []byte) string {
	var b strings.Builder
	info, _ := runtimeHint(s.Name)
	writeHoverHeader(&b, "runtime value", s, src)
	if info.Signature != "" {
		fmt.Fprintf(&b, "**Signature**\n\n`%s`\n\n", info.Signature)
	}
	if info.Description != "" {
		b.WriteString("**What it does**\n\n")
		b.WriteString(info.Description)
		b.WriteString("\n\n")
	}
	if len(info.Examples) > 0 {
		b.WriteString("**Examples**\n\n")
		for _, ex := range info.Examples {
			fmt.Fprintf(&b, "- `%s`\n", ex)
		}
		b.WriteString("\n")
	}
	b.WriteString("**How BCL evaluates it**\n\n- BCL records this as a runtime reference.\n- The host application supplies the value during evaluation or simulation.\n- Validation does not require a matching BCL block declaration for this namespace.\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this runtime value.")
	b.WriteString("\n")
	writeHoverCommands(&b)
	return b.String()
}

func richFunctionHover(a *Analysis, s LanguageSymbol, src []byte) string {
	var b strings.Builder
	info, _ := builtinFunctionHint(s.Name)
	writeHoverHeader(&b, "function", s, src)
	if info.Signature != "" {
		fmt.Fprintf(&b, "**Signature**\n\n`%s`\n\n", info.Signature)
	}
	if info.Description != "" {
		b.WriteString("**What it does**\n\n")
		b.WriteString(info.Description)
		b.WriteString("\n\n")
	}
	if len(info.Examples) > 0 {
		b.WriteString("**Examples**\n\n")
		for _, ex := range info.Examples {
			fmt.Fprintf(&b, "- `%s`\n", ex)
		}
		b.WriteString("\n")
	}
	b.WriteString("**How BCL evaluates it**\n\n- Parses the call and its arguments from the current document.\n- Evaluates the function only when the host enables the required capability.\n- Preserves the call span for diagnostics, hover, and completion help.\n\n")
	writeDiagnosticsOrOK(&b, a, s.Span, "No diagnostics for this function call.")
	b.WriteString("\n")
	writeHoverCommands(&b)
	return b.String()
}

func writeHoverHeader(b *strings.Builder, label string, s LanguageSymbol, src []byte) {
	fmt.Fprintf(b, "### BCL %s `%s`\n\n", label, s.Name)
	if s.Detail != "" {
		fmt.Fprintf(b, "**Type:** `%s`\n\n", s.Detail)
	}
	if s.Container != "" {
		fmt.Fprintf(b, "**Scope:** `%s`\n\n", s.Container)
	}
	if snippet := sourceSnippet(src, s.Span); snippet != "" {
		b.WriteString("**Definition**\n\n")
		fmt.Fprintf(b, "```bcl\n%s\n```\n\n", snippet)
	}
}

func writeHoverCommands(b *strings.Builder) {
	b.WriteString("[Compile current file](command:bcl.compileCurrentFile) | [Explain current file](command:bcl.explainCurrentFile) | [Restart BCL LSP](command:bcl.restartLanguageServer)")
}

func writeDiagnosticsOrOK(b *strings.Builder, a *Analysis, sp Span, okMessage string) {
	diags := diagnosticsForSpan(a.Diagnostics, sp)
	if len(diags) == 0 {
		fmt.Fprintf(b, "- %s\n", okMessage)
		return
	}
	for _, d := range diags {
		fmt.Fprintf(b, "- `%s`: %s\n", d.Severity, d.Message)
	}
}

func blockChildAssignments(s LanguageSymbol) map[string]LanguageSymbol {
	out := map[string]LanguageSymbol{}
	for _, child := range s.Children {
		if child.Kind == SymbolAssignment {
			out[child.Name] = child
		}
	}
	return out
}

func sortedAssignmentSymbols(assigns map[string]LanguageSymbol) []LanguageSymbol {
	out := make([]LanguageSymbol, 0, len(assigns))
	for _, s := range assigns {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func writeStepBehaviorBullets(b *strings.Builder, step LanguageSymbol, indent string) {
	id := trimBlockName(step.Name)
	assigns := blockChildAssignments(step)
	kind := assigns["kind"].Value
	if kind == "" {
		kind = assigns["type"].Value
	}
	fmt.Fprintf(b, "%s- `%s`: %s\n", indent, id, stepPurpose(kind))
	if kind != "" {
		fmt.Fprintf(b, "%s- Kind: `%s`\n", indent, kind)
	}
	if when := stepConditionSummary(step); when != "" {
		fmt.Fprintf(b, "%s- Condition: %s\n", indent, when)
	}
	if then := stepThenSummary(step); then != "" {
		fmt.Fprintf(b, "%s- Then: %s\n", indent, then)
	}
	if fields := stepFieldSummary(step); fields != "" {
		fmt.Fprintf(b, "%s- Fields: %s\n", indent, fields)
	}
}

func workflowStepParameterMeaning(role, pipelineName, id string, step LanguageSymbol) string {
	var parts []string
	if pipelineName != "" {
		parts = append(parts, fmt.Sprintf("%s step in pipeline `%s`", role, pipelineName))
	} else {
		parts = append(parts, role+" workflow step")
	}
	if step.Name == "" {
		parts = append(parts, fmt.Sprintf("step `%s` is missing or unresolved", emptyDefault(id, "(missing)")))
		return markdownTableCell(strings.Join(parts, "; ") + ".")
	}
	assigns := blockChildAssignments(step)
	kind := assigns["kind"].Value
	if kind == "" {
		kind = assigns["type"].Value
	}
	if kind != "" {
		parts = append(parts, fmt.Sprintf("`%s` step that %s", kind, trimTrailingPeriod(stepPurpose(kind))))
	} else {
		parts = append(parts, trimTrailingPeriod(stepPurpose(kind)))
	}
	if when := stepConditionSummary(step); when != "" {
		parts = append(parts, "when "+when)
	}
	if then := stepThenSummary(step); then != "" {
		parts = append(parts, "then "+then)
	}
	if fields := stepFieldSummary(step); fields != "" {
		parts = append(parts, "fields "+fields)
	}
	return markdownTableCell(strings.Join(parts, "; ") + ".")
}

func workflowReferencedStepForAssignment(a *Analysis, s LanguageSymbol) (LanguageSymbol, workflowFacts, string, LanguageSymbol, bool) {
	parent, facts, ok := workflowFactsForContainer(a, s.Container)
	if !ok || facts.Steps == nil {
		return LanguageSymbol{}, workflowFacts{}, "", LanguageSymbol{}, false
	}
	var id string
	switch s.Name {
	case "from", "to":
		if !strings.HasPrefix(s.Container, "connection.") {
			return LanguageSymbol{}, workflowFacts{}, "", LanguageSymbol{}, false
		}
		id = refTargetIDForHover(valueOr(s.Value, s.ResolvedTarget))
	case "entrypoint":
		if !strings.HasPrefix(s.Container, "pipeline.") {
			return LanguageSymbol{}, workflowFacts{}, "", LanguageSymbol{}, false
		}
		id = refTargetIDForHover(s.Value)
	default:
		return LanguageSymbol{}, workflowFacts{}, "", LanguageSymbol{}, false
	}
	if id == "" {
		return parent, facts, id, LanguageSymbol{}, true
	}
	return parent, facts, id, facts.Steps[id], true
}

func trimTrailingPeriod(s string) string {
	return strings.TrimSuffix(s, ".")
}

func markdownTableCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}

func stepPurpose(kind string) string {
	switch kind {
	case "task":
		return "runs or records a unit of workflow work before moving to the next transition."
	case "decision":
		return "evaluates conditions and chooses a matched or unmatched transition."
	case "action":
		return "performs or records an action requested by the workflow definition."
	case "terminal":
		return "ends the workflow path and records a final reason or outcome."
	case "":
		return "participates as a workflow step."
	default:
		return "acts as a `" + kind + "` workflow step."
	}
}

func stepConditionSummary(step LanguageSymbol) string {
	for _, child := range step.Children {
		if child.Name == "when" {
			if child.Value != "" && child.Value != "object" {
				return "`" + child.Value + "`"
			}
			if nested := childBlockNames(child, ""); len(nested) > 0 {
				return strings.Join(nested, ", ")
			}
			return "configured in `when` block"
		}
		if child.Kind == SymbolBlock && child.Detail == "when" {
			return "configured in `when` block"
		}
	}
	return ""
}

func stepThenSummary(step LanguageSymbol) string {
	for _, child := range step.Children {
		if child.Name == "then" || (child.Kind == SymbolBlock && child.Detail == "then") {
			parts := childBlockNames(child, "")
			if len(parts) > 0 {
				return strings.Join(parts, ", ")
			}
			return "configured in `then` block"
		}
	}
	return ""
}

func stepFieldSummary(step LanguageSymbol) string {
	var fields []string
	for _, child := range step.Children {
		if child.Kind != SymbolAssignment {
			continue
		}
		switch child.Name {
		case "kind", "type", "when", "then":
			continue
		}
		fields = append(fields, fmt.Sprintf("`%s = %s`", child.Name, emptyDefault(child.Value, child.Detail)))
	}
	return strings.Join(fields, ", ")
}

func childBlockSummary(s LanguageSymbol) string {
	var lines []string
	for _, child := range s.Children {
		if child.Kind == SymbolBlock || child.Kind == SymbolSet {
			lines = append(lines, fmt.Sprintf("- `%s` (%s)", child.Name, emptyDefault(child.Detail, string(child.Kind))))
		}
		if child.Kind == SymbolAssignment && len(child.Children) > 0 {
			lines = append(lines, fmt.Sprintf("- `%s` object with %d nested item(s)", child.Name, len(child.Children)))
		}
	}
	return strings.Join(lines, "\n")
}

func symbolReferenceSummary(a *Analysis, s LanguageSymbol) string {
	targets := symbolReferencedTargets(s)
	if len(targets) == 0 {
		return ""
	}
	var b strings.Builder
	writeReferenceTargetSummaries(&b, a, s, targets)
	return b.String()
}

func writeReferenceTargetSummaries(b *strings.Builder, a *Analysis, context LanguageSymbol, targets []string) {
	for _, target := range uniqueStrings(targets) {
		fmt.Fprintf(b, "- `%s`: %s\n", target, referenceTargetSummary(a, context, target))
	}
}

func referenceTargetSummary(a *Analysis, context LanguageSymbol, target string) string {
	if target == "" {
		return "empty reference."
	}
	if info, ok := runtimeHint(target); ok {
		return info.Description
	}
	decl, ok := a.Declarations[target]
	if !ok {
		root, _, dotted := strings.Cut(target, ".")
		if dotted && !hasDeclaredBlockType(a, root) {
			return "runtime or host-provided value. Add it to the runtime hint catalog for a more specific description."
		}
		return "not declared in the current analysis index."
	}
	if decl.Detail == "step" {
		if parent, facts, found := workflowFactsForContainer(a, context.Container); found {
			id := refTargetIDForHover(target)
			if step := facts.Steps[id]; step.Name != "" {
				return workflowStepParameterMeaning("Referenced", trimBlockName(parent.Name), id, step)
			}
		}
		return symbolBriefSummary(decl)
	}
	return symbolBriefSummary(decl)
}

func symbolBriefSummary(s LanguageSymbol) string {
	switch s.Kind {
	case SymbolRuntime:
		if info, ok := runtimeHint(s.Name); ok {
			return info.Description
		}
		return "runtime value supplied by the host application."
	case SymbolConst:
		if s.Value != "" {
			return fmt.Sprintf("constant declaration; value `%s`; value kind `%s`.", s.Value, emptyDefault(s.ValueKind, s.Detail))
		}
		return fmt.Sprintf("constant declaration; value kind `%s`.", emptyDefault(s.Detail, "value"))
	case SymbolSchema:
		return "schema declaration used to validate blocks with the same name."
	case SymbolType:
		return fmt.Sprintf("type alias declaration for `%s`.", emptyDefault(s.Detail, "type"))
	case SymbolSet:
		return "set block consumed by `set(...)`; " + symbolFieldChildBrief(s)
	case SymbolBlock:
		return fmt.Sprintf("`%s` block; %s", emptyDefault(s.Detail, "block"), symbolFieldChildBrief(s))
	case SymbolImport:
		return fmt.Sprintf("import declaration for `%s`.", emptyDefault(s.Detail, s.Name))
	default:
		if s.Value != "" {
			return fmt.Sprintf("%s `%s`; current value `%s`.", s.Kind, emptyDefault(s.Detail, "value"), s.Value)
		}
		return fmt.Sprintf("%s `%s`.", s.Kind, emptyDefault(s.Detail, "symbol"))
	}
}

func symbolFieldChildBrief(s LanguageSymbol) string {
	var fields []string
	var children []string
	for _, child := range s.Children {
		if child.Kind == SymbolAssignment {
			if child.Value != "" {
				fields = append(fields, fmt.Sprintf("`%s = %s`", child.Name, child.Value))
			} else if len(child.Children) > 0 {
				children = append(children, fmt.Sprintf("`%s` object", child.Name))
			}
			continue
		}
		if child.Kind == SymbolBlock || child.Kind == SymbolSet {
			children = append(children, fmt.Sprintf("`%s` %s", child.Name, emptyDefault(child.Detail, "block")))
		}
	}
	var parts []string
	if len(fields) > 0 {
		parts = append(parts, "fields "+strings.Join(fields, ", "))
	}
	if len(children) > 0 {
		parts = append(parts, "children "+strings.Join(children, ", "))
	}
	if len(parts) == 0 {
		return "no visible fields."
	}
	return strings.Join(parts, "; ") + "."
}

func referenceTargetsForSymbol(s LanguageSymbol) []string {
	targets := append([]string{}, s.ReferencedTargets...)
	if s.ResolvedTarget != "" {
		targets = append(targets, s.ResolvedTarget)
	}
	return uniqueStrings(targets)
}

func symbolReferencedTargets(s LanguageSymbol) []string {
	targets := referenceTargetsForSymbol(s)
	for _, child := range s.Children {
		targets = mergeReferenceTargets(targets, symbolReferencedTargets(child))
	}
	return uniqueStrings(targets)
}

func mergeReferenceTargets(a, b []string) []string {
	out := append([]string{}, a...)
	out = append(out, b...)
	return uniqueStrings(out)
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func workflowFactsForContainer(a *Analysis, container string) (LanguageSymbol, workflowFacts, bool) {
	for _, s := range flattenSymbols(a.Symbols) {
		if s.Kind == SymbolBlock && s.Detail == "pipeline" && (s.Name == container || containsWorkflowSymbol(s, container)) {
			return s, workflowFactsForPipeline(s), true
		}
	}
	return LanguageSymbol{}, workflowFacts{}, false
}

func containsWorkflowSymbol(parent LanguageSymbol, name string) bool {
	for _, child := range parent.Children {
		if child.Name == name || containsWorkflowSymbol(child, name) {
			return true
		}
	}
	return false
}

func workflowFactsForPipeline(p LanguageSymbol) workflowFacts {
	f := workflowFacts{Pipeline: p, Steps: map[string]LanguageSymbol{}}
	assigns := blockChildAssignments(p)
	f.Entrypoint = assigns["entrypoint"].Value
	for _, child := range p.Children {
		if child.Kind != SymbolBlock {
			continue
		}
		id := trimBlockName(child.Name)
		switch child.Detail {
		case "step":
			f.Steps[id] = child
		case "connection":
			assigns := blockChildAssignments(child)
			from := valueOr(assigns["from"].Value, assigns["from"].ResolvedTarget)
			to := valueOr(assigns["to"].Value, assigns["to"].ResolvedTarget)
			on := assigns["on"].Value
			f.Connections = append(f.Connections, workflowConnectionFact{
				Name:       id,
				From:       from,
				To:         to,
				On:         on,
				FromExists: f.Steps[refTargetIDForHover(from)].Name != "",
				ToExists:   f.Steps[refTargetIDForHover(to)].Name != "",
			})
		}
	}
	for i := range f.Connections {
		f.Connections[i].FromExists = f.Steps[refTargetIDForHover(f.Connections[i].From)].Name != ""
		f.Connections[i].ToExists = f.Steps[refTargetIDForHover(f.Connections[i].To)].Name != ""
	}
	return f
}

func childBlockNames(s LanguageSymbol, typ string) []string {
	var out []string
	for _, child := range s.Children {
		if child.Kind == SymbolBlock && (typ == "" || child.Detail == typ) {
			if typ == "then" {
				out = append(out, childBlockNames(child, "")...)
			} else {
				out = append(out, "`"+child.Name+"`")
			}
		}
		if child.Kind == SymbolAssignment {
			if typ != "" && child.Name != typ {
				continue
			}
			if len(child.Children) > 0 {
				out = append(out, childBlockNames(child, "")...)
				continue
			}
			out = append(out, fmt.Sprintf("`%s = %s`", child.Name, emptyDefault(child.Value, child.Detail)))
		}
	}
	return out
}

func writeConnectionList(b *strings.Builder, label string, conns []workflowConnectionFact) {
	if len(conns) == 0 {
		fmt.Fprintf(b, "- %s: none\n", label)
		return
	}
	parts := make([]string, 0, len(conns))
	for _, c := range conns {
		parts = append(parts, fmt.Sprintf("`%s` (`%s -> %s` on `%s`)", c.Name, refTargetIDForHover(c.From), refTargetIDForHover(c.To), emptyDefault(c.On, "(default)")))
	}
	fmt.Fprintf(b, "- %s: %s\n", label, strings.Join(parts, ", "))
}

func trimBlockName(name string) string {
	if i := strings.IndexByte(name, '.'); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func refTargetIDForHover(ref string) string {
	if i := strings.LastIndexByte(ref, '.'); i >= 0 && i+1 < len(ref) {
		return ref[i+1:]
	}
	return ref
}

func valueOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func existsText(ok bool) string {
	if ok {
		return "exists"
	}
	return "missing"
}

func diagnosticsForSpan(diags []Diagnostic, sp Span) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if spansOverlap(d.Span, sp) {
			out = append(out, d)
		}
	}
	return out
}

func spansOverlap(a, b Span) bool {
	if a.Start.Line == 0 || b.Start.Line == 0 {
		return false
	}
	return containsPosition(b, a.Start.Line, a.Start.Column) || containsPosition(b, a.End.Line, a.End.Column) || containsPosition(a, b.Start.Line, b.Start.Column)
}

func sourceSnippet(src []byte, sp Span) string {
	if sp.Start.Offset < 0 || sp.End.Offset <= sp.Start.Offset || sp.End.Offset > len(src) {
		return ""
	}
	raw := strings.TrimSpace(string(src[sp.Start.Offset:sp.End.Offset]))
	lines := strings.Split(raw, "\n")
	if len(lines) > 14 {
		lines = append(lines[:14], "...")
	}
	return strings.Join(lines, "\n")
}

func isKeyword(s string) bool {
	switch s {
	case "import", "as", "const", "type", "schema", "required", "optional", "enum", "default", "description", "doc", "deprecated", "sensitive", "generated", "min", "max", "pattern", "format", "examples", "true", "false", "null", "when", "override":
		return true
	default:
		return isKnownBlock(s)
	}
}

func containsPosition(sp Span, line, column int) bool {
	if sp.Start.Line == 0 {
		return false
	}
	if line < sp.Start.Line || line > sp.End.Line {
		return false
	}
	if line == sp.Start.Line && column < sp.Start.Column {
		return false
	}
	if line == sp.End.Line && column > sp.End.Column {
		return false
	}
	return true
}

func spanSize(sp Span) int {
	return (sp.End.Line-sp.Start.Line)*10000 + (sp.End.Column - sp.Start.Column)
}
