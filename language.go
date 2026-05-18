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

type Completion struct {
	Label         string `json:"label"`
	Kind          string `json:"kind"`
	Detail        string `json:"detail,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	InsertText    string `json:"insert_text,omitempty"`
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
	Completions  []Completion              `json:"completions"`
	Diagnostics  []Diagnostic              `json:"diagnostics,omitempty"`
}

func TokenizeFile(name string, src []byte) ([]SyntaxToken, []Diagnostic) {
	toks, errs := lex(name, src)
	out := make([]SyntaxToken, 0, len(toks))
	for _, tok := range toks {
		if tok.kind == tokEOF || tok.kind == tokNewline {
			continue
		}
		out = append(out, SyntaxToken{Type: tokenEditorType(tok), Text: tok.text, Span: tok.span})
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
	a.Symbols = analyzeNodes(doc.Items, "", a)
	a.Diagnostics = append(a.Diagnostics, Lint(doc, opts)...)
	a.Completions = analysisCompletions(a)
	return a, a.Diagnostics
}

func DefaultCompletions() []Completion {
	var out []Completion
	for _, kw := range []string{"import", "param", "predicate", "test", "const", "type", "schema", "bcl", "namespace", "profile", "module", "override", "runtime", "evaluation", "audit", "context", "session", "policy", "rule", "set", "pipeline", "step", "connection", "http", "connector", "source", "action", "file", "command", "output", "when", "all", "any", "not", "none"} {
		out = append(out, Completion{Label: kw, Kind: "keyword", Detail: "BCL keyword/block"})
	}
	for _, fn := range []string{"env", "ref", "set", "http", "concat", "upper", "lower", "contains", "exists", "hash", "base64"} {
		out = append(out, Completion{Label: fn, Kind: "function", Detail: "BCL function", InsertText: fn + "($1)"})
	}
	for _, snip := range []Completion{
		{Label: "schema block", Kind: "snippet", Detail: "Schema declaration", InsertText: "schema ${1:name} {\n  required ${2:field} ${3:string}\n}"},
		{Label: "param block", Kind: "snippet", Detail: "Module input parameter", InsertText: "param ${1:name} ${2:string} {\n  required true\n}"},
		{Label: "predicate block", Kind: "snippet", Detail: "Reusable condition predicate", InsertText: "predicate \"${1:name}\" {\n  all {\n    ${2:condition}\n  }\n}"},
		{Label: "test block", Kind: "snippet", Detail: "Executable BCL test", InsertText: "test \"${1:name}\" {\n  input {\n    ${2:key} ${3:value}\n  }\n\n  expect {\n    diagnostics none\n  }\n}"},
		{Label: "policy block", Kind: "snippet", Detail: "Policy block", InsertText: "policy \"${1:name}\" {\n  effect ${2:allow}\n}"},
		{Label: "set block", Kind: "snippet", Detail: "Reusable set", InsertText: "set \"${1:name}\" {\n  ${2:item}\n}"},
	} {
		out = append(out, snip)
	}
	return out
}

func SymbolAt(a *Analysis, line, column int) (LanguageSymbol, bool) {
	for _, r := range a.References {
		if containsPosition(r.Span, line, column) {
			if decl, found := a.Declarations[r.Name]; found {
				return decl, true
			}
		}
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
	switch tok.kind {
	case tokIdent:
		if isKeyword(tok.text) {
			return "keyword"
		}
		if isExprOperator(tok.text) {
			return "operator"
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
	return out
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

func symbolBehavior(s LanguageSymbol) string {
	switch s.Kind {
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
	decl, ok := a.Declarations[target]
	if !ok {
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
