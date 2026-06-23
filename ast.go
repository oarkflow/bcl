package bcl

import (
	"encoding/json"
)

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

type Span struct {
	File  string   `json:"file,omitempty"`
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Span     Span   `json:"span,omitempty"`
}

type ErrorList []Diagnostic

func (e ErrorList) Error() string {
	if len(e) == 0 {
		return ""
	}
	return FormatDiagnostics(e)
}

type Node interface {
	node()
	GetSpan() Span
}

type Document struct {
	File  string `json:"file,omitempty"`
	Items []Node `json:"items"`
	Span  Span   `json:"span,omitempty"`
}

func (*Document) node()           {}
func (d *Document) GetSpan() Span { return d.Span }

type Assignment struct {
	Name      string `json:"name"`
	Value     Value  `json:"value"`
	Sensitive bool   `json:"sensitive,omitempty"`
	Span      Span   `json:"span,omitempty"`
}

func (*Assignment) node()           {}
func (a *Assignment) GetSpan() Span { return a.Span }

type Block struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Body []Node `json:"body"`
	Span Span   `json:"span,omitempty"`
}

func (*Block) node()           {}
func (b *Block) GetSpan() Span { return b.Span }

type Spread struct {
	Target string `json:"target"`
	Body   []Node `json:"body,omitempty"`
	Span   Span   `json:"span,omitempty"`
}

func (*Spread) node()           {}
func (s *Spread) GetSpan() Span { return s.Span }

type ConstDecl struct {
	Name  string `json:"name"`
	Value Value  `json:"value"`
	Span  Span   `json:"span,omitempty"`
}

func (*ConstDecl) node()           {}
func (c *ConstDecl) GetSpan() Span { return c.Span }

type ImportDecl struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
	Span  Span   `json:"span,omitempty"`
}

func (*ImportDecl) node()           {}
func (i *ImportDecl) GetSpan() Span { return i.Span }

type ParamDecl struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Default     Value  `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
	Span        Span   `json:"span,omitempty"`
}

func (*ParamDecl) node()           {}
func (p *ParamDecl) GetSpan() Span { return p.Span }

func (*TypeDecl) node()           {}
func (t *TypeDecl) GetSpan() Span { return t.Span }

type SchemaDecl struct {
	Name     string           `json:"name"`
	Options  map[string]Value `json:"options,omitempty"`
	Fields   []SchemaField    `json:"fields"`
	Sections map[string]Value `json:"sections,omitempty"`
	Span     Span             `json:"span,omitempty"`
}

func (*SchemaDecl) node()           {}
func (s *SchemaDecl) GetSpan() Span { return s.Span }

type SchemaField struct {
	Required             bool             `json:"required"`
	Name                 string           `json:"name"`
	Type                 string           `json:"type"`
	Ref                  string           `json:"ref,omitempty"`
	Const                Value            `json:"const,omitempty"`
	Default              Value            `json:"default,omitempty"`
	Enum                 []Value          `json:"enum,omitempty"`
	Fields               []SchemaField    `json:"fields,omitempty"`
	Items                string           `json:"items,omitempty"`
	PrefixItems          []string         `json:"prefix_items,omitempty"`
	Contains             string           `json:"contains,omitempty"`
	Description          string           `json:"description,omitempty"`
	Title                string           `json:"title,omitempty"`
	Deprecated           string           `json:"deprecated,omitempty"`
	Sensitive            bool             `json:"sensitive,omitempty"`
	Generated            bool             `json:"generated,omitempty"`
	Derived              bool             `json:"derived,omitempty"`
	ReadOnly             bool             `json:"read_only,omitempty"`
	WriteOnly            bool             `json:"write_only,omitempty"`
	Nullable             bool             `json:"nullable,omitempty"`
	UniqueItems          bool             `json:"unique_items,omitempty"`
	Closed               bool             `json:"closed,omitempty"`
	ClosedSet            bool             `json:"closed_set,omitempty"`
	AdditionalProperties *bool            `json:"additional_properties,omitempty"`
	Min                  Value            `json:"min,omitempty"`
	Max                  Value            `json:"max,omitempty"`
	ExclusiveMin         Value            `json:"exclusive_min,omitempty"`
	ExclusiveMax         Value            `json:"exclusive_max,omitempty"`
	MultipleOf           Value            `json:"multiple_of,omitempty"`
	MinLen               Value            `json:"min_len,omitempty"`
	MaxLen               Value            `json:"max_len,omitempty"`
	MinItems             Value            `json:"min_items,omitempty"`
	MaxItems             Value            `json:"max_items,omitempty"`
	MinProps             Value            `json:"min_props,omitempty"`
	MaxProps             Value            `json:"max_props,omitempty"`
	Pattern              string           `json:"pattern,omitempty"`
	Format               string           `json:"format,omitempty"`
	ContentEncoding      string           `json:"content_encoding,omitempty"`
	ContentMediaType     string           `json:"content_media_type,omitempty"`
	Examples             []Value          `json:"examples,omitempty"`
	PatternProperties    Value            `json:"pattern_properties,omitempty"`
	DependentRequired    Value            `json:"dependent_required,omitempty"`
	LTField              string           `json:"lt_field,omitempty"`
	LTEField             string           `json:"lte_field,omitempty"`
	GTField              string           `json:"gt_field,omitempty"`
	GTEField             string           `json:"gte_field,omitempty"`
	EqField              string           `json:"eq_field,omitempty"`
	AllOf                []string         `json:"all_of,omitempty"`
	AnyOf                []string         `json:"any_of,omitempty"`
	OneOf                []string         `json:"one_of,omitempty"`
	Not                  string           `json:"not,omitempty"`
	If                   string           `json:"if,omitempty"`
	Then                 string           `json:"then,omitempty"`
	Else                 string           `json:"else,omitempty"`
	Classification       string           `json:"classification,omitempty"`
	Audit                string           `json:"audit,omitempty"`
	Explain              string           `json:"explain,omitempty"`
	PII                  string           `json:"pii,omitempty"`
	PolicyTag            string           `json:"policy_tag,omitempty"`
	Owner                string           `json:"owner,omitempty"`
	Severity             string           `json:"severity,omitempty"`
	Extensions           map[string]Value `json:"extensions,omitempty"`
	Span                 Span             `json:"span,omitempty"`
}

type Value interface {
	value()
	Kind() string
	GetSpan() Span
	ToInterface(redact bool) any
}

type Literal struct {
	Type      string `json:"type"`
	Raw       string `json:"raw,omitempty"`
	Data      any    `json:"data"`
	Sensitive bool   `json:"sensitive,omitempty"`
	Span      Span   `json:"span,omitempty"`
}

func (*Literal) value()          {}
func (l *Literal) Kind() string  { return l.Type }
func (l *Literal) GetSpan() Span { return l.Span }
func (l *Literal) ToInterface(redact bool) any {
	if redact && l.Sensitive {
		return "****"
	}
	return l.Data
}

type List struct {
	Items []Value `json:"items"`
	Tuple bool    `json:"tuple,omitempty"`
	Span  Span    `json:"span,omitempty"`
}

func (*List) value()          {}
func (l *List) Kind() string  { return "list" }
func (l *List) GetSpan() Span { return l.Span }
func (l *List) ToInterface(redact bool) any {
	out := make([]any, 0, len(l.Items))
	for _, v := range l.Items {
		out = append(out, v.ToInterface(redact))
	}
	return out
}

type Object struct {
	Fields []Node `json:"fields"`
	Span   Span   `json:"span,omitempty"`
}

func (*Object) node()           {}
func (*Object) value()          {}
func (o *Object) Kind() string  { return "object" }
func (o *Object) GetSpan() Span { return o.Span }
func (o *Object) ToInterface(redact bool) any {
	m := make(map[string]any, len(o.Fields))
	for _, n := range o.Fields {
		switch x := n.(type) {
		case *Assignment:
			m[x.Name] = x.Value.ToInterface(redact || x.Sensitive)
		case *Block:
			m[x.Type] = appendBlock(m[x.Type], normalizeBlock(x, redact))
		case *Spread:
			// Spread resolution needs compiler context, so raw AST conversion keeps a marker.
			m["$spread"] = appendBlock(m["$spread"], map[string]any{"target": x.Target})
		}
	}
	return m
}

type Expr struct {
	Raw  string `json:"raw"`
	Span Span   `json:"span,omitempty"`
}

func (*Expr) value()          {}
func (e *Expr) Kind() string  { return "expression" }
func (e *Expr) GetSpan() Span { return e.Span }
func (e *Expr) ToInterface(bool) any {
	return map[string]any{"$expr": e.Raw}
}

type Condition struct {
	Op       string       `json:"op"`
	Expr     *Expr        `json:"expr,omitempty"`
	Children []*Condition `json:"children,omitempty"`
	Span     Span         `json:"span,omitempty"`
}

func (*Condition) value()          {}
func (c *Condition) Kind() string  { return "condition" }
func (c *Condition) GetSpan() Span { return c.Span }
func (c *Condition) ToInterface(bool) any {
	if c.Expr != nil {
		return map[string]any{"op": c.Op, "expr": c.Expr.Raw}
	}
	children := make([]any, 0, len(c.Children))
	for _, child := range c.Children {
		children = append(children, child.ToInterface(false))
	}
	return map[string]any{"op": c.Op, "children": children}
}

type Call struct {
	Name string  `json:"name"`
	Args []Value `json:"args,omitempty"`
	Span Span    `json:"span,omitempty"`
}

func (*Call) value()          {}
func (c *Call) Kind() string  { return "call" }
func (c *Call) GetSpan() Span { return c.Span }
func (c *Call) ToInterface(redact bool) any {
	args := make([]any, 0, len(c.Args))
	for _, a := range c.Args {
		args = append(args, a.ToInterface(redact))
	}
	return map[string]any{"$call": c.Name, "args": args}
}

type Reference struct {
	Path string `json:"path"`
	Span Span   `json:"span,omitempty"`
}

func (*Reference) value()          {}
func (r *Reference) Kind() string  { return "reference" }
func (r *Reference) GetSpan() Span { return r.Span }
func (r *Reference) ToInterface(bool) any {
	return map[string]any{"$ref": r.Path}
}

type Normalized struct {
	Version     string              `json:"version,omitempty"`
	Body        map[string]any      `json:"body,omitempty"`
	Blocks      []map[string]any    `json:"blocks,omitempty"`
	Constants   map[string]any      `json:"constants,omitempty"`
	Params      map[string]any      `json:"params,omitempty"`
	Predicates  map[string]any      `json:"predicates,omitempty"`
	Tests       []map[string]any    `json:"tests,omitempty"`
	Sets        map[string][]any    `json:"sets,omitempty"`
	Types       map[string]string   `json:"types,omitempty"`
	Imports     []map[string]string `json:"imports,omitempty"`
	Modules     []map[string]any    `json:"modules,omitempty"`
	Namespaces  map[string]any      `json:"namespaces,omitempty"`
	Schemas     map[string]any      `json:"schemas,omitempty"`
	Diagnostics []Diagnostic        `json:"diagnostics,omitempty"`
}

type CompileResult struct {
	Normalized    *Normalized      `json:"normalized"`
	Diagnostics   []Diagnostic     `json:"diagnostics,omitempty"`
	Sensitive     []string         `json:"sensitive,omitempty"`
	Dependencies  []Dependency     `json:"dependencies,omitempty"`
	SourceSpans   map[string]Span  `json:"source_spans,omitempty"`
	Lockfile      *Lockfile        `json:"lockfile,omitempty"`
	Explain       []ExplainStep    `json:"explain,omitempty"`
	Capabilities  map[string]any   `json:"capabilities,omitempty"`
	Params        map[string]any   `json:"params,omitempty"`
	Predicates    map[string]any   `json:"predicates,omitempty"`
	Tests         []map[string]any `json:"tests,omitempty"`
	Strict        bool             `json:"strict"`
	ActiveProfile string           `json:"active_profile,omitempty"`
}

type Dependency struct {
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Resolved string `json:"resolved,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

type ExplainStep struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Span    Span   `json:"span,omitempty"`
	Details any    `json:"details,omitempty"`
}

type TypeDecl struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Span Span   `json:"span,omitempty"`
}

func (n *Normalized) JSON(redact bool) ([]byte, error) {
	if redact {
		cp := *n
		return json.MarshalIndent(&cp, "", "  ")
	}
	return json.MarshalIndent(n, "", "  ")
}

func appendBlock(existing any, block any) any {
	if existing == nil {
		return []any{block}
	}
	if s, ok := existing.([]any); ok {
		return append(s, block)
	}
	return []any{existing, block}
}

func normalizeBlock(b *Block, redact bool) map[string]any {
	out := make(map[string]any, 3)
	out["type"] = b.Type
	if b.ID != "" {
		out["id"] = b.ID
	}
	body := (&Object{Fields: b.Body}).ToInterface(redact)
	if m, ok := body.(map[string]any); ok {
		out["body"] = m
	}
	return out
}
