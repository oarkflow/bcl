package bcl

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// ValuePhase tells the host when a value can be resolved.
type ValuePhase string

const (
	PhaseStatic  ValuePhase = "static"
	PhaseCompile ValuePhase = "compile"
	PhaseRuntime ValuePhase = "runtime"
	PhaseSecret  ValuePhase = "secret"
)

// MissingPolicy controls what runtime resolution does with missing optional values.
type MissingPolicy string

const (
	MissingError MissingPolicy = "error"
	MissingKeep  MissingPolicy = "keep"
	MissingNull  MissingPolicy = "null"
	MissingEmpty MissingPolicy = "empty"
)

// RefDep is a dependency discovered from a reference, expression, or template.
type RefDep struct {
	Scope string   `json:"scope"`
	Path  []string `json:"path,omitempty"`
}

// PrepareOptions configures the parse/prepare phase. It intentionally does not
// contain request/task/session data; those belong to RuntimeContext.
type PrepareOptions struct {
	Env                  func(string) (string, bool)
	AllowEnv             bool
	AllowTime            bool
	AllowHash            bool
	AllowEncoding        bool
	Interpolate          bool
	DisableInterpolation bool
	Strict               bool
	Partial              bool
	Redact               bool
	Missing              MissingPolicy
	EvalFunctions        map[string]EvalFunction
	Now                  func() time.Time
}

// RuntimeContext is supplied by the host when a prepared BCL plan is executed.
type RuntimeContext struct {
	Input   map[string]any
	Request map[string]any
	Session map[string]any
	Context map[string]any
	Runtime map[string]any
	Task    map[string]any
	Node    map[string]any
	Result  map[string]any
	Results map[string]any
	Vars    map[string]any
	Event   map[string]any

	Secrets SecretProvider
	Store   RuntimeStore

	Now           func() time.Time
	Functions     map[string]EvalFunction
	Missing       MissingPolicy
	Strict        bool
	AllowTime     bool
	AllowHash     bool
	AllowEncoding bool
}

type SecretProvider interface {
	GetSecret(name string) (string, bool, error)
}

type RuntimeStore interface {
	Get(path string) (any, bool, error)
}

type PreparedPlan struct {
	Body        map[string]PreparedValue `json:"body,omitempty"`
	Blocks      []map[string]any         `json:"blocks,omitempty"`
	Constants   map[string]PreparedValue `json:"constants,omitempty"`
	Params      map[string]RuntimeParam  `json:"params,omitempty"`
	Diagnostics []Diagnostic             `json:"diagnostics,omitempty"`
}

type RuntimeParam struct {
	Name        string        `json:"name"`
	Type        string        `json:"type,omitempty"`
	Required    bool          `json:"required,omitempty"`
	Default     PreparedValue `json:"default,omitempty"`
	Description string        `json:"description,omitempty"`
	Phase       ValuePhase    `json:"phase"`
	Deps        []RefDep      `json:"deps,omitempty"`
}

type PreparedValue interface {
	Phase() ValuePhase
	Deps() []RefDep
	Resolve(*RuntimeContext) (any, error)
	ToInterface(redact bool) any
}

type StaticValue struct {
	Value     any        `json:"value"`
	Sensitive bool       `json:"sensitive,omitempty"`
	phase     ValuePhase `json:"-"`
}

func (v *StaticValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	return PhaseStatic
}
func (v *StaticValue) Deps() []RefDep { return nil }
func (v *StaticValue) Resolve(*RuntimeContext) (any, error) {
	if v.Sensitive {
		return "****", nil
	}
	return cloneAny(v.Value), nil
}
func (v *StaticValue) ToInterface(redact bool) any {
	if v.Sensitive {
		return "****"
	}
	return cloneAny(v.Value)
}

type RefValue struct {
	Scope     string     `json:"scope"`
	Path      []string   `json:"path,omitempty"`
	Required  bool       `json:"required,omitempty"`
	Sensitive bool       `json:"sensitive,omitempty"`
	phase     ValuePhase `json:"-"`
}

func (v *RefValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	if isRuntimeScope(v.Scope) {
		return PhaseRuntime
	}
	return PhaseCompile
}
func (v *RefValue) Deps() []RefDep {
	return []RefDep{{Scope: v.Scope, Path: append([]string(nil), v.Path...)}}
}
func (v *RefValue) Resolve(ctx *RuntimeContext) (any, error) {
	if v.Sensitive {
		return "****", nil
	}
	val, ok, err := lookupRuntimeRef(ctx, v.Scope, v.Path)
	if err != nil {
		return nil, err
	}
	if !ok {
		return resolveMissingValue(ctx, v.Required, strings.Join(append([]string{v.Scope}, v.Path...), "."))
	}
	return val, nil
}
func (v *RefValue) ToInterface(redact bool) any {
	if v.Sensitive {
		return "****"
	}
	return map[string]any{"$ref": strings.Join(append([]string{v.Scope}, v.Path...), "."), "$phase": v.Phase(), "$deps": v.Deps()}
}

type ExprValue struct {
	Raw       string             `json:"raw"`
	Program   *ExpressionProgram `json:"-"`
	Required  bool               `json:"required,omitempty"`
	Sensitive bool               `json:"sensitive,omitempty"`
	deps      []RefDep           `json:"-"`
	phase     ValuePhase         `json:"-"`
}

func (v *ExprValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	return phaseFromDeps(v.deps)
}
func (v *ExprValue) Deps() []RefDep { return cloneDeps(v.deps) }
func (v *ExprValue) Resolve(ctx *RuntimeContext) (any, error) {
	if v.Sensitive {
		return "****", nil
	}
	prog := v.Program
	if prog == nil {
		p, err := CompileExpression(v.Raw)
		if err != nil {
			return nil, err
		}
		prog = p
	}
	val, err := prog.Eval(ctx.evalVars(), &EvalOptions{Variables: ctx.evalVars(), AllowEncoding: ctx.AllowEncoding, AllowHash: ctx.AllowHash, AllowTime: ctx.AllowTime, Functions: ctx.Functions, Now: ctx.nowFunc()})
	if err != nil {
		return nil, err
	}
	if val == nil && v.Required {
		return nil, fmt.Errorf("missing required expression %q", v.Raw)
	}
	return val, nil
}
func (v *ExprValue) ToInterface(redact bool) any {
	if v.Sensitive {
		return "****"
	}
	return map[string]any{"$expr": v.Raw, "$phase": v.Phase(), "$deps": v.Deps()}
}

type TemplateValue struct {
	Parts     []PreparedValue `json:"parts"`
	Sensitive bool            `json:"sensitive,omitempty"`
	phase     ValuePhase      `json:"-"`
}

func (v *TemplateValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	return phaseFromPrepared(v.Parts)
}
func (v *TemplateValue) Deps() []RefDep {
	var deps []RefDep
	for _, p := range v.Parts {
		deps = append(deps, p.Deps()...)
	}
	return uniqueDeps(deps)
}
func (v *TemplateValue) Resolve(ctx *RuntimeContext) (any, error) {
	if v.Sensitive {
		return "****", nil
	}
	var b strings.Builder
	for _, p := range v.Parts {
		val, err := p.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		if val != nil {
			b.WriteString(fmt.Sprint(val))
		}
	}
	return b.String(), nil
}
func (v *TemplateValue) ToInterface(redact bool) any {
	if v.Sensitive {
		return "****"
	}
	parts := make([]any, 0, len(v.Parts))
	for _, p := range v.Parts {
		parts = append(parts, p.ToInterface(redact))
	}
	return map[string]any{"$template": map[string]any{"phase": v.Phase(), "parts": parts, "deps": v.Deps()}}
}

type ObjectValue struct {
	Fields map[string]PreparedValue `json:"fields"`
	phase  ValuePhase
}

func (v *ObjectValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	xs := make([]PreparedValue, 0, len(v.Fields))
	for _, f := range v.Fields {
		xs = append(xs, f)
	}
	return phaseFromPrepared(xs)
}
func (v *ObjectValue) Deps() []RefDep {
	var deps []RefDep
	for _, f := range v.Fields {
		deps = append(deps, f.Deps()...)
	}
	return uniqueDeps(deps)
}
func (v *ObjectValue) Resolve(ctx *RuntimeContext) (any, error) {
	m := make(map[string]any, len(v.Fields))
	keys := make([]string, 0, len(v.Fields))
	if varsValue, ok := v.Fields["vars"]; ok {
		val, err := varsValue.Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("vars: %w", err)
		}
		setNormalized(m, "vars", val)
		if ctx != nil {
			if ctx.Vars == nil {
				ctx.Vars = map[string]any{}
			}
			if mm, ok := val.(map[string]any); ok {
				for k, item := range mm {
					ctx.Vars[k] = item
				}
			}
		}
	}
	for k := range v.Fields {
		if k != "vars" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		val, err := v.Fields[k].Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		setNormalized(m, k, val)
	}
	return m, nil
}
func (v *ObjectValue) ToInterface(redact bool) any {
	m := make(map[string]any, len(v.Fields))
	for k, f := range v.Fields {
		setNormalized(m, k, f.ToInterface(redact))
	}
	return m
}

type ListValue struct {
	Items []PreparedValue `json:"items"`
	phase ValuePhase
}

func (v *ListValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	return phaseFromPrepared(v.Items)
}
func (v *ListValue) Deps() []RefDep {
	var deps []RefDep
	for _, f := range v.Items {
		deps = append(deps, f.Deps()...)
	}
	return uniqueDeps(deps)
}
func (v *ListValue) Resolve(ctx *RuntimeContext) (any, error) {
	out := make([]any, 0, len(v.Items))
	for i, it := range v.Items {
		val, err := it.Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
		out = append(out, val)
	}
	return out, nil
}
func (v *ListValue) ToInterface(redact bool) any {
	out := make([]any, 0, len(v.Items))
	for _, it := range v.Items {
		out = append(out, it.ToInterface(redact))
	}
	return out
}

type SecretValue struct {
	Name     string `json:"name"`
	Required bool   `json:"required,omitempty"`
}

func (v *SecretValue) Phase() ValuePhase { return PhaseSecret }
func (v *SecretValue) Deps() []RefDep    { return []RefDep{{Scope: "secrets", Path: []string{v.Name}}} }
func (v *SecretValue) Resolve(ctx *RuntimeContext) (any, error) {
	if ctx == nil || ctx.Secrets == nil {
		return resolveMissingValue(ctx, v.Required, "secrets."+v.Name)
	}
	s, ok, err := ctx.Secrets.GetSecret(v.Name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return resolveMissingValue(ctx, v.Required, "secrets."+v.Name)
	}
	return s, nil
}
func (v *SecretValue) ToInterface(bool) any {
	return map[string]any{"$secret": v.Name, "$phase": PhaseSecret, "$redact": true}
}

// Prepare parses AST into a partially-evaluated execution plan. Compile-time values
// are resolved, runtime references remain typed deferred values.
func Prepare(doc *Document, opts *PrepareOptions) (*PreparedPlan, error) {
	if opts == nil {
		opts = &PrepareOptions{}
	}
	if opts.Env == nil {
		opts.Env = os.LookupEnv
	}
	if opts.Missing == "" {
		opts.Missing = MissingError
	}
	opts.Interpolate = !opts.DisableInterpolation
	p := &planCompiler{opts: opts, constants: map[string]PreparedValue{}, constResolved: map[string]any{}}
	p.collectConstants(doc.Items)
	plan := &PreparedPlan{Body: map[string]PreparedValue{}, Constants: map[string]PreparedValue{}, Params: map[string]RuntimeParam{}}
	for name, val := range p.constants {
		plan.Constants[name] = val
	}
	for _, n := range doc.Items {
		p.emitNode(plan, plan.Body, n)
	}
	plan.Diagnostics = append(plan.Diagnostics, p.errs...)
	if len(p.errs) > 0 && opts.Strict {
		return plan, p.errs
	}
	return plan, nil
}

func (p *PreparedPlan) Resolve(ctx *RuntimeContext) (map[string]any, error) {
	if ctx == nil {
		ctx = &RuntimeContext{}
	}
	ctx.ensureDefaults()
	consts := map[string]any{}
	for k, v := range p.Constants {
		rv, err := v.Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("const.%s: %w", k, err)
		}
		consts[k] = rv
	}
	if ctx.Context == nil {
		ctx.Context = map[string]any{}
	}
	ctx.Context["const"] = consts
	if ctx.Vars == nil {
		ctx.Vars = map[string]any{}
	}
	if vv, ok := p.Body["vars"]; ok {
		if m, err := vv.Resolve(ctx); err != nil {
			return nil, fmt.Errorf("vars: %w", err)
		} else if mm, ok := m.(map[string]any); ok {
			for k, v := range mm {
				ctx.Vars[k] = v
			}
		}
	}
	out := map[string]any{}
	keys := make([]string, 0, len(p.Body))
	for k := range p.Body {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val, err := p.Body[k].Resolve(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		setNormalized(out, k, val)
	}
	return out, nil
}

func (p *PreparedPlan) ToInterface(redact bool) map[string]any {
	out := map[string]any{"body": map[string]any{}, "constants": map[string]any{}, "params": p.Params}
	body := out["body"].(map[string]any)
	for k, v := range p.Body {
		setNormalized(body, k, v.ToInterface(redact))
	}
	consts := out["constants"].(map[string]any)
	for k, v := range p.Constants {
		consts[k] = v.ToInterface(redact)
	}
	if len(p.Diagnostics) > 0 {
		out["diagnostics"] = p.Diagnostics
	}
	return out
}

func (p *PreparedPlan) MarshalJSON() ([]byte, error) { return json.Marshal(p.ToInterface(false)) }

type planCompiler struct {
	opts          *PrepareOptions
	constants     map[string]PreparedValue
	constResolved map[string]any
	errs          ErrorList
}

func (p *planCompiler) collectConstants(nodes []Node) {
	for _, n := range nodes {
		if c, ok := n.(*ConstDecl); ok {
			pv := p.prepareValue(c.Value, false)
			p.constants[c.Name] = pv
			if pv.Phase() != PhaseRuntime && pv.Phase() != PhaseSecret {
				if val, err := pv.Resolve(p.compileContext()); err == nil {
					p.constResolved[c.Name] = val
				}
			}
		}
	}
}
func (p *planCompiler) emitNode(plan *PreparedPlan, body map[string]PreparedValue, n Node) {
	switch x := n.(type) {
	case *Assignment:
		setPrepared(body, x.Name, p.prepareValue(x.Value, x.Sensitive))
	case *Block:
		bv := p.prepareBlockValue(x)
		plan.Blocks = append(plan.Blocks, bv.ToInterface(false).(map[string]any))
		body[x.Type] = appendPreparedBlock(body[x.Type], bv)
	case *ParamDecl:
		rp := RuntimeParam{Name: x.Name, Type: x.Type, Required: x.Required, Description: x.Description, Phase: PhaseRuntime}
		if x.Default != nil {
			rp.Default = p.prepareValue(x.Default, false)
			rp.Deps = rp.Default.Deps()
			rp.Phase = rp.Default.Phase()
		}
		plan.Params[x.Name] = rp
	case *ConstDecl: /* collected */
	}
}

func (p *planCompiler) prepareBlockValue(x *Block) PreparedValue {
	fields := map[string]PreparedValue{
		"type": &StaticValue{Value: x.Type},
		"body": &ObjectValue{Fields: p.prepareNodes(x.Body)},
	}
	if x.ID != "" {
		fields["id"] = &StaticValue{Value: x.ID}
	}
	return &ObjectValue{Fields: fields}
}

func (p *planCompiler) prepareNodes(nodes []Node) map[string]PreparedValue {
	body := map[string]PreparedValue{}
	for _, n := range nodes {
		switch x := n.(type) {
		case *Assignment:
			setPrepared(body, x.Name, p.prepareValue(x.Value, x.Sensitive))
		case *Block:
			body[x.Type] = appendPreparedBlock(body[x.Type], p.prepareBlockValue(x))
		}
	}
	return body
}

func (p *planCompiler) prepareValue(v Value, sensitive bool) PreparedValue {
	switch x := v.(type) {
	case *Literal:
		if x.Type == "string" && p.opts.Interpolate {
			if s, ok := x.Data.(string); ok {
				return p.prepareTemplate(s, sensitive)
			}
		}
		if x.Type == "duration" || x.Type == "bytes" || x.Type == "date" || x.Type == "datetime" {
			return &StaticValue{Value: map[string]any{"$" + x.Type: x.Data}, Sensitive: sensitive || x.Sensitive}
		}
		return &StaticValue{Value: x.Data, Sensitive: sensitive || x.Sensitive}
	case *Reference:
		return p.prepareRef(x.Path, sensitive)
	case *Expr:
		return p.prepareExpr(x.Raw, sensitive)
	case *Call:
		return p.prepareCall(x, sensitive)
	case *List:
		items := make([]PreparedValue, 0, len(x.Items))
		for _, it := range x.Items {
			items = append(items, p.prepareValue(it, sensitive))
		}
		return &ListValue{Items: items}
	case *Object:
		return &ObjectValue{Fields: p.prepareNodes(x.Fields)}
	case *Condition:
		return &StaticValue{Value: x.ToInterface(false), Sensitive: sensitive}
	default:
		return &StaticValue{Value: nil, Sensitive: sensitive}
	}
}
func (p *planCompiler) prepareRef(path string, sensitive bool) PreparedValue {
	if path == "" {
		return &StaticValue{Value: nil}
	}
	parts := strings.Split(path, ".")
	scope := parts[0]
	rest := parts[1:]
	if scope == "const" && len(rest) > 0 {
		if v, ok := p.constants[strings.Join(rest, ".")]; ok {
			return v
		}
	}
	if v, ok := p.constants[path]; ok {
		return v
	}
	rv := &RefValue{Scope: scope, Path: rest, Sensitive: sensitive, Required: p.opts.Strict}
	if isRuntimeScope(scope) {
		rv.phase = PhaseRuntime
	} else {
		rv.phase = PhaseCompile
	}
	if rv.phase == PhaseCompile {
		if scope == "env" && len(rest) > 0 {
			if val, ok := p.opts.Env(strings.Join(rest, ".")); ok {
				return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
			}
		}
		if val, ok, err := lookupRuntimeRef(p.compileContext(), scope, rest); err == nil && ok {
			return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
		}
	}
	return rv
}
func (p *planCompiler) prepareExpr(raw string, sensitive bool) PreparedValue {
	deps := AnalyzeDeps(raw)
	phase := phaseFromDeps(deps)
	prog, err := CompileExpression(raw)
	if err != nil {
		p.errs = append(p.errs, Diagnostic{Severity: "error", Message: err.Error()})
		return &ExprValue{Raw: raw, deps: deps, phase: phase, Sensitive: sensitive}
	}
	ev := &ExprValue{Raw: raw, Program: prog, deps: deps, phase: phase, Sensitive: sensitive}
	if phase == PhaseCompile {
		if val, err := ev.Resolve(p.compileContext()); err == nil {
			return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
		} else if p.opts.Strict {
			p.errs = append(p.errs, Diagnostic{Severity: "error", Message: err.Error()})
		}
	}
	return ev
}
func (p *planCompiler) prepareCall(x *Call, sensitive bool) PreparedValue {
	name := x.Name
	if strings.HasPrefix(name, "env") {
		return p.prepareEnvCall(x, sensitive)
	}
	if strings.HasPrefix(name, "secrets.") || name == "secret" || name == "secret.required" {
		if len(x.Args) > 0 {
			if lit, ok := x.Args[0].(*Literal); ok {
				if s, ok := lit.Data.(string); ok {
					return &SecretValue{Name: s, Required: strings.Contains(name, "required")}
				}
			}
		}
		return &SecretValue{Name: name, Required: strings.Contains(name, "required")}
	}
	if strings.HasPrefix(name, "runtime.") || name == "defer" {
		return p.prepareExpr(callRaw(x), sensitive)
	}
	// Compile-capability calls remain eager unless their arguments have runtime deps.
	args := make([]PreparedValue, 0, len(x.Args))
	phase := PhaseCompile
	for _, a := range x.Args {
		pv := p.prepareValue(a, false)
		args = append(args, pv)
		if pv.Phase() == PhaseRuntime || pv.Phase() == PhaseSecret {
			phase = PhaseRuntime
		}
	}
	if phase == PhaseCompile {
		cv := &CallRuntimeValue{Name: name, Args: args, Sensitive: sensitive, phase: PhaseCompile, compiler: p}
		if val, err := cv.Resolve(p.compileContext()); err == nil {
			return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
		}
	}
	return &CallRuntimeValue{Name: name, Args: args, Sensitive: sensitive, phase: phase, compiler: p}
}
func (p *planCompiler) prepareTemplate(s string, sensitive bool) PreparedValue {
	matches := interpolationPattern.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return &StaticValue{Value: s, Sensitive: sensitive}
	}
	parts := make([]PreparedValue, 0, len(matches)*2+1)
	pos := 0
	for _, m := range matches {
		if m[0] > pos {
			parts = append(parts, &StaticValue{Value: s[pos:m[0]]})
		}
		raw := strings.TrimSpace(s[m[2]:m[3]])
		parts = append(parts, p.prepareExpr(raw, false))
		pos = m[1]
	}
	if pos < len(s) {
		parts = append(parts, &StaticValue{Value: s[pos:]})
	}
	tv := &TemplateValue{Parts: parts, Sensitive: sensitive}
	if tv.Phase() == PhaseCompile {
		if val, err := tv.Resolve(p.compileContext()); err == nil {
			return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
		}
	}
	return tv
}
func (p *planCompiler) compileContext() *RuntimeContext {
	return &RuntimeContext{Context: map[string]any{"const": p.constResolved}, Runtime: map[string]any{}, Now: p.opts.Now, Missing: p.opts.Missing, Strict: p.opts.Strict, AllowTime: p.opts.AllowTime, AllowHash: p.opts.AllowHash, AllowEncoding: p.opts.AllowEncoding, Functions: p.opts.EvalFunctions}
}

func (p *planCompiler) prepareEnvCall(x *Call, sensitive bool) PreparedValue {
	key := ""
	if len(x.Args) > 0 {
		if lit, ok := x.Args[0].(*Literal); ok {
			key = fmt.Sprint(lit.Data)
		}
	}
	if key == "" {
		return &StaticValue{Value: nil, Sensitive: sensitive, phase: PhaseCompile}
	}
	val, ok := p.opts.Env(key)
	if !ok {
		if strings.Contains(x.Name, "required") && p.opts.Strict {
			p.errs = append(p.errs, Diagnostic{Severity: "error", Message: "missing required env " + key})
		}
		return &StaticValue{Value: nil, Sensitive: sensitive, phase: PhaseCompile}
	}
	switch x.Name {
	case "env", "env.required":
		return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
	case "env.list":
		parts := strings.Split(val, ",")
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			out = append(out, strings.TrimSpace(part))
		}
		return &StaticValue{Value: out, Sensitive: sensitive, phase: PhaseCompile}
	default:
		return &StaticValue{Value: val, Sensitive: sensitive, phase: PhaseCompile}
	}
}

type CallRuntimeValue struct {
	Name      string
	Args      []PreparedValue
	Sensitive bool
	phase     ValuePhase
	compiler  *planCompiler
}

func (v *CallRuntimeValue) Phase() ValuePhase {
	if v.phase != "" {
		return v.phase
	}
	return phaseFromPrepared(v.Args)
}
func (v *CallRuntimeValue) Deps() []RefDep {
	var d []RefDep
	for _, a := range v.Args {
		d = append(d, a.Deps()...)
	}
	return uniqueDeps(d)
}
func (v *CallRuntimeValue) Resolve(ctx *RuntimeContext) (any, error) {
	if v.Sensitive {
		return "****", nil
	}
	args := make([]any, 0, len(v.Args))
	for _, a := range v.Args {
		rv, err := a.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		args = append(args, rv)
	}
	opts := &EvalOptions{Variables: ctx.evalVars(), AllowEncoding: ctx.AllowEncoding, AllowHash: ctx.AllowHash, AllowTime: ctx.AllowTime, Functions: ctx.Functions, Now: ctx.nowFunc()}
	return evalCall(v.Name, args, opts)
}
func (v *CallRuntimeValue) ToInterface(redact bool) any {
	if v.Sensitive {
		return "****"
	}
	args := make([]any, 0, len(v.Args))
	for _, a := range v.Args {
		args = append(args, a.ToInterface(redact))
	}
	return map[string]any{"$call": v.Name, "args": args, "$phase": v.Phase(), "$deps": v.Deps()}
}

func AnalyzeDeps(raw string) []RefDep {
	toks, err := exprTokens(raw)
	if err != nil {
		return nil
	}
	skip := map[string]bool{"true": true, "false": true, "null": true, "ANY": true, "NONE": true, "match": true, "case": true, "exists": true, "empty": true, "between": true, "in": true, "not": true, "and": true, "or": true}
	var deps []RefDep
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tokIdent || skip[t.text] {
			continue
		}
		if i+1 < len(toks) && toks[i+1].kind == tokLParen {
			continue
		}
		parts := []string{t.text}
		j := i + 1
		for j+1 < len(toks) && toks[j].kind == tokDot && toks[j+1].kind == tokIdent {
			parts = append(parts, toks[j+1].text)
			j += 2
		}
		i = j - 1
		if len(parts) > 0 {
			deps = append(deps, RefDep{Scope: parts[0], Path: parts[1:]})
		}
	}
	return uniqueDeps(deps)
}

func isRuntimeScope(scope string) bool {
	switch scope {
	case "input", "request", "session", "runtime", "task", "node", "result", "results", "vars", "secrets", "store", "event":
		return true
	}
	return false
}
func phaseFromDeps(deps []RefDep) ValuePhase {
	phase := PhaseStatic
	for _, d := range deps {
		if d.Scope == "secrets" {
			return PhaseSecret
		}
		if isRuntimeScope(d.Scope) {
			phase = PhaseRuntime
		} else if phase == PhaseStatic {
			phase = PhaseCompile
		}
	}
	return phase
}
func phaseFromPrepared(xs []PreparedValue) ValuePhase {
	phase := PhaseStatic
	for _, x := range xs {
		switch x.Phase() {
		case PhaseSecret:
			return PhaseSecret
		case PhaseRuntime:
			phase = PhaseRuntime
		case PhaseCompile:
			if phase == PhaseStatic {
				phase = PhaseCompile
			}
		}
	}
	return phase
}
func cloneDeps(in []RefDep) []RefDep {
	out := make([]RefDep, len(in))
	for i, d := range in {
		out[i] = RefDep{Scope: d.Scope, Path: append([]string(nil), d.Path...)}
	}
	return out
}
func uniqueDeps(in []RefDep) []RefDep {
	seen := map[string]bool{}
	out := make([]RefDep, 0, len(in))
	for _, d := range in {
		key := d.Scope + "." + strings.Join(d.Path, ".")
		if !seen[key] {
			seen[key] = true
			out = append(out, d)
		}
	}
	return out
}
func setPrepared(m map[string]PreparedValue, name string, val PreparedValue) {
	if !strings.Contains(name, ".") {
		m[name] = val
		return
	}
	parts := strings.Split(name, ".")
	cur, ok := m[parts[0]].(*ObjectValue)
	if !ok {
		cur = &ObjectValue{Fields: map[string]PreparedValue{}}
		m[parts[0]] = cur
	}
	for _, part := range parts[1 : len(parts)-1] {
		next, ok := cur.Fields[part].(*ObjectValue)
		if !ok {
			next = &ObjectValue{Fields: map[string]PreparedValue{}}
			cur.Fields[part] = next
		}
		cur = next
	}
	cur.Fields[parts[len(parts)-1]] = val
}
func appendPreparedBlock(existing PreparedValue, block PreparedValue) PreparedValue {
	if existing == nil {
		return &ListValue{Items: []PreparedValue{block}}
	}
	if l, ok := existing.(*ListValue); ok {
		l.Items = append(l.Items, block)
		return l
	}
	return &ListValue{Items: []PreparedValue{existing, block}}
}
func callRaw(x *Call) string {
	var b strings.Builder
	b.WriteString(x.Name)
	b.WriteByte('(')
	for i, a := range x.Args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(valueRaw(a))
	}
	b.WriteByte(')')
	return b.String()
}
func valueRaw(v Value) string {
	switch x := v.(type) {
	case *Literal:
		if s, ok := x.Data.(string); ok {
			return strconvQuote(s)
		}
		return fmt.Sprint(x.Data)
	case *Reference:
		return x.Path
	case *Expr:
		return x.Raw
	default:
		b, _ := json.Marshal(x.ToInterface(false))
		return string(b)
	}
}

func (ctx *RuntimeContext) ensureDefaults() {
	if ctx.Missing == "" {
		ctx.Missing = MissingError
	}
	if ctx.Now == nil {
		ctx.Now = func() time.Time { return time.Now().UTC() }
	}
	if ctx.Runtime == nil {
		ctx.Runtime = map[string]any{}
	}
	now := ctx.Now().UTC()
	if _, ok := ctx.Runtime["now"]; !ok {
		ctx.Runtime["now"] = now.Format(time.RFC3339)
	}
	if _, ok := ctx.Runtime["date"]; !ok {
		ctx.Runtime["date"] = now.Format("2006-01-02")
	}
	if _, ok := ctx.Runtime["timestamp"]; !ok {
		ctx.Runtime["timestamp"] = now.Format(time.RFC3339)
	}
	if _, ok := ctx.Runtime["uuid"]; !ok {
		if u, err := randomUUID(); err == nil {
			ctx.Runtime["uuid"] = u
		}
	}
	if _, ok := ctx.Runtime["run_id"]; !ok {
		ctx.Runtime["run_id"] = ctx.Runtime["uuid"]
	}
}
func (ctx *RuntimeContext) nowFunc() func() time.Time {
	if ctx != nil && ctx.Now != nil {
		return ctx.Now
	}
	return func() time.Time { return time.Now().UTC() }
}
func (ctx *RuntimeContext) evalVars() map[string]any {
	if ctx == nil {
		ctx = &RuntimeContext{}
	}
	ctx.ensureDefaults()
	vars := map[string]any{"input": ctx.Input, "request": ctx.Request, "session": ctx.Session, "context": ctx.Context, "runtime": ctx.Runtime, "task": ctx.Task, "node": ctx.Node, "result": ctx.Result, "results": ctx.Results, "vars": ctx.Vars, "event": ctx.Event}
	if ctx.Context != nil {
		if c, ok := ctx.Context["const"].(map[string]any); ok {
			vars["const"] = c
		}
	}
	return vars
}
func lookupRuntimeRef(ctx *RuntimeContext, scope string, path []string) (any, bool, error) {
	if ctx == nil {
		ctx = &RuntimeContext{}
	}
	ctx.ensureDefaults()
	var root any
	switch scope {
	case "input":
		root = ctx.Input
	case "request":
		root = ctx.Request
	case "session":
		root = ctx.Session
	case "context":
		root = ctx.Context
	case "runtime":
		root = ctx.Runtime
	case "task":
		root = ctx.Task
	case "node":
		root = ctx.Node
	case "result":
		root = ctx.Result
	case "results":
		root = ctx.Results
	case "vars":
		root = ctx.Vars
	case "event":
		root = ctx.Event
	case "store":
		if ctx.Store == nil {
			return nil, false, nil
		}
		v, ok, err := ctx.Store.Get(strings.Join(path, "."))
		return v, ok, err
	case "secrets":
		if ctx.Secrets == nil {
			return nil, false, nil
		}
		v, ok, err := ctx.Secrets.GetSecret(strings.Join(path, "."))
		return v, ok, err
	case "env":
		if len(path) == 0 {
			return nil, false, nil
		}
		v, ok := os.LookupEnv(strings.Join(path, "."))
		return v, ok, nil
	case "const":
		if ctx.Context != nil {
			if c, ok := ctx.Context["const"].(map[string]any); ok {
				root = c
			}
		}
	default:
		return nil, false, nil
	}
	if len(path) == 0 {
		return root, root != nil, nil
	}
	v := lookupParts(map[string]any{scope: root}, append([]string{scope}, path...))
	return v, v != nil, nil
}
func resolveMissingValue(ctx *RuntimeContext, required bool, name string) (any, error) {
	pol := MissingError
	if ctx != nil && ctx.Missing != "" {
		pol = ctx.Missing
	}
	if required || pol == MissingError {
		return nil, fmt.Errorf("missing runtime variable %s", name)
	}
	switch pol {
	case MissingKeep:
		return "${" + name + "}", nil
	case MissingEmpty:
		return "", nil
	default:
		return nil, nil
	}
}
