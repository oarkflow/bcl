package bcl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/oarkflow/convert"
)

func LoadFile(path string, opts *Options) (*Document, error) {
	return ParsePath(path)
}

func CompileDetailed(doc *Document, opts *Options) (*CompileResult, error) {
	n, err := Compile(doc, opts)
	result := &CompileResult{
		Normalized:    n,
		Diagnostics:   append([]Diagnostic(nil), n.Diagnostics...),
		Sensitive:     collectSensitivePaths(doc),
		Dependencies:  collectDependencies(n),
		SourceSpans:   collectSourceSpans(doc),
		Capabilities:  collectCapabilities(n),
		Params:        n.Params,
		Predicates:    n.Predicates,
		Tests:         n.Tests,
		Strict:        opts != nil && opts.Strict,
		ActiveProfile: "",
	}
	if opts != nil {
		result.ActiveProfile = opts.Profile
		if opts.LockfilePath != "" {
			result.Lockfile, _ = ReadLockfile(opts.LockfilePath)
		}
	}
	result.Explain = append(result.Explain,
		ExplainStep{Phase: "load", Message: "parsed BCL document", Span: doc.Span},
		ExplainStep{Phase: "compile", Message: "normalized document to JSON-compatible AST", Span: doc.Span},
	)
	if err != nil {
		if e, ok := err.(ErrorList); ok {
			result.Diagnostics = append(result.Diagnostics, e...)
		} else {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: doc.Span})
		}
	}
	return result, err
}

func CompileFileWithLock(path, lockPath string, opts *Options) (*CompileResult, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.BaseDir = filepath.Dir(path)
	opts.LockfilePath = lockPath
	opts.ResolveImports = true
	opts.ResolveModules = true
	return CompileDetailed(doc, opts)
}

func ExplainFile(path string, opts *Options) (*CompileResult, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.BaseDir = filepath.Dir(path)
	opts.ResolveImports = true
	opts.ResolveModules = true
	return CompileDetailed(doc, opts)
}

func ValidateFile(path string, opts *Options) []Diagnostic {
	doc, err := ParsePath(path)
	if err != nil {
		return []Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	diags := Validate(doc, opts)
	if opts != nil && opts.Strict {
		diags = append(diags, strictDiagnostics(doc, opts)...)
	}
	return diags
}

type SimulationResult struct {
	Matched     []map[string]any `json:"matched,omitempty"`
	Unmatched   []map[string]any `json:"unmatched,omitempty"`
	Decision    map[string]any   `json:"decision,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
	Trace       []ExplainStep    `json:"trace,omitempty"`
}

func SimulateFile(path string, input map[string]any, opts *Options) (*SimulationResult, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.BaseDir = filepath.Dir(path)
	n, err := Compile(doc, opts)
	if err != nil {
		return &SimulationResult{Diagnostics: n.Diagnostics}, err
	}
	return Simulate(n, input, opts), nil
}

func Simulate(n *Normalized, input map[string]any, opts *Options) *SimulationResult {
	result := &SimulationResult{}
	vars := map[string]any{}
	for k, v := range input {
		vars[k] = v
	}
	for k, v := range n.Constants {
		vars[k] = v
	}
	for k, v := range n.Sets {
		vars[k] = v
	}
	addRuntimeScopeVars(vars, n)
	for _, block := range n.Blocks {
		body, _ := block["body"].(map[string]any)
		when, hasWhen := body["when"].(map[string]any)
		if !hasWhen {
			continue
		}
		ok, err := evalNormalizedConditionTrace(when, vars, opts, &result.Trace, blockID(block))
		step := ExplainStep{Phase: "evaluate", Message: blockID(block)}
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
			result.Trace = append(result.Trace, step)
			continue
		}
		if ok {
			result.Matched = append(result.Matched, block)
		} else {
			result.Unmatched = append(result.Unmatched, block)
		}
		result.Trace = append(result.Trace, step)
	}
	result.Decision = evaluateStrategy(n, result.Matched)
	result.Trace = append(result.Trace, ExplainStep{Phase: "decision", Message: fmt.Sprint(result.Decision["effect"]), Details: result.Decision})
	return result
}

func addRuntimeScopeVars(vars map[string]any, n *Normalized) {
	for _, name := range []string{"context", "session"} {
		if v, ok := n.Body[name]; ok {
			vars[name] = v
		}
	}
	for _, block := range n.Blocks {
		t, _ := block["type"].(string)
		if t != "context" && t != "session" {
			continue
		}
		body, _ := block["body"].(map[string]any)
		id, _ := block["id"].(string)
		if id == "" {
			vars[t] = body
			continue
		}
		group, _ := vars[t].(map[string]any)
		if group == nil {
			group = map[string]any{}
			vars[t] = group
		}
		group[id] = body
	}
}

func evaluateStrategy(n *Normalized, matched []map[string]any) map[string]any {
	strategy := "deny_overrides"
	defaultEffect := "deny"
	if eval, ok := n.Body["evaluation"].(map[string]any); ok {
		if s, ok := eval["strategy"].(string); ok && s != "" {
			strategy = s
		}
		if s, ok := eval["default"].(string); ok && s != "" {
			defaultEffect = s
		}
	}
	decision := map[string]any{"effect": defaultEffect, "strategy": strategy}
	if len(matched) == 0 {
		return decision
	}
	chosen := chooseMatched(strategy, matched)
	if chosen == nil {
		return decision
	}
	body, _ := chosen["body"].(map[string]any)
	if effect, ok := body["effect"].(string); ok {
		decision["effect"] = effect
	}
	decision["matched"] = blockID(chosen)
	if reason, ok := body["reason_code"].(string); ok {
		decision["reason"] = reason
	}
	return decision
}

func chooseMatched(strategy string, matched []map[string]any) map[string]any {
	switch strategy {
	case "first_match":
		return matched[0]
	case "highest_priority", "weighted_score":
		var best map[string]any
		bestPriority := int64(-1 << 62)
		for _, block := range matched {
			body, _ := block["body"].(map[string]any)
			p := intValue(body["priority"])
			if best == nil || p > bestPriority {
				best = block
				bestPriority = p
			}
		}
		return best
	case "allow_overrides":
		return firstEffect(matched, "allow", matched[0])
	case "all_must_pass":
		for _, block := range matched {
			body, _ := block["body"].(map[string]any)
			if body["effect"] == "deny" {
				return block
			}
		}
		return matched[len(matched)-1]
	case "deny_overrides":
		fallthrough
	default:
		return firstEffect(matched, "deny", matched[0])
	}
}

func firstEffect(blocks []map[string]any, effect string, fallback map[string]any) map[string]any {
	for _, block := range blocks {
		body, _ := block["body"].(map[string]any)
		if body["effect"] == effect {
			return block
		}
	}
	return fallback
}

func intValue(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

func WatchFiles(paths []string, opts *Options, onChange func(WatchEvent)) chan struct{} {
	return WatchFilesWithDependencies(paths, opts, onChange)
}

func WatchFilesWithDependencies(paths []string, opts *Options, onChange func(WatchEvent)) chan struct{} {
	stop := make(chan struct{})
	watchSet := map[string]bool{}
	for _, path := range paths {
		watchSet[path] = true
		for _, dep := range discoverDependencyFiles(path) {
			watchSet[dep] = true
		}
	}
	for path := range watchSet {
		w := &Watcher{Path: path, Options: opts, Interval: time.Second}
		watched := path
		child := w.Start(func(ev WatchEvent) {
			ev.Dependency = watched
			onChange(ev)
		})
		go func(ch chan struct{}) {
			<-stop
			close(ch)
		}(child)
	}
	return stop
}

func discoverDependencyFiles(path string) []string {
	doc, err := ParsePath(path)
	if err != nil {
		return nil
	}
	base := filepath.Dir(path)
	var out []string
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ImportDecl:
				files, _ := resolveSourceFiles(x.Path, base)
				out = append(out, files...)
			case *Block:
				if x.Type == "module" {
					src := blockString(x, "source")
					files, _ := resolveModuleOrSource(src, base)
					out = append(out, files...)
				}
				walk(x.Body)
			}
		}
	}
	walk(doc.Items)
	return out
}

func ReadJSONFile(path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func collectSensitivePaths(doc *Document) []string {
	var out []string
	var walk func(prefix string, nodes []Node)
	walk = func(prefix string, nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *Assignment:
				path := joinPath(prefix, x.Name)
				if x.Sensitive || isSensitiveValue(x.Value) {
					out = append(out, path)
				}
				if o, ok := x.Value.(*Object); ok {
					walk(path, o.Fields)
				}
			case *Block:
				walk(joinPath(prefix, x.Type+"."+x.ID), x.Body)
			}
		}
	}
	walk("", doc.Items)
	return out
}

func isSensitiveValue(v Value) bool {
	if c, ok := v.(*Call); ok && c.Name == "sensitive" {
		return true
	}
	return false
}

func collectSourceSpans(doc *Document) map[string]Span {
	out := map[string]Span{}
	var walk func(prefix string, nodes []Node)
	walk = func(prefix string, nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *Assignment:
				out[joinPath(prefix, x.Name)] = x.Span
			case *ParamDecl:
				out[joinPath(prefix, "param."+x.Name)] = x.Span
			case *Block:
				key := joinPath(prefix, x.Type+"."+x.ID)
				out[key] = x.Span
				walk(key, x.Body)
			}
		}
	}
	walk("", doc.Items)
	return out
}

func collectDependencies(n *Normalized) []Dependency {
	var out []Dependency
	for _, imp := range n.Imports {
		out = append(out, Dependency{Kind: "import", Source: imp["path"]})
	}
	for _, mod := range n.Modules {
		out = append(out, Dependency{Kind: "module", Source: stringValue(mod["source"]), Resolved: stringValue(mod["resolved"])})
	}
	return out
}

func collectCapabilities(n *Normalized) map[string]any {
	if n == nil || n.Body == nil {
		return nil
	}
	if caps, ok := n.Body["capabilities"].(map[string]any); ok {
		return caps
	}
	return nil
}

func evalNormalizedCondition(cond map[string]any, input map[string]any, opts *Options) (bool, error) {
	op, _ := cond["op"].(string)
	if expr, ok := cond["expr"].(string); ok {
		return evalExprBool(expr, input, opts)
	}
	children, _ := cond["children"].([]any)
	switch op {
	case "all", "":
		for _, raw := range children {
			ok, err := evalNormalizedCondition(raw.(map[string]any), input, opts)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	case "any":
		for _, raw := range children {
			ok, err := evalNormalizedCondition(raw.(map[string]any), input, opts)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		if len(children) == 0 {
			return true, nil
		}
		ok, err := evalNormalizedCondition(children[0].(map[string]any), input, opts)
		return !ok, err
	case "none":
		for _, raw := range children {
			ok, err := evalNormalizedCondition(raw.(map[string]any), input, opts)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

func evalNormalizedConditionTrace(cond map[string]any, input map[string]any, opts *Options, trace *[]ExplainStep, owner string) (bool, error) {
	if expr, ok := cond["expr"].(string); ok {
		v, err := EvalExpr(expr, evalOptionsFrom(opts, input))
		detail := map[string]any{"owner": owner, "expr": expr, "value": v}
		if err != nil {
			detail["error"] = err.Error()
			*trace = append(*trace, ExplainStep{Phase: "expression", Message: expr, Details: detail})
			return false, err
		}
		ok := truthy(v)
		detail["result"] = ok
		*trace = append(*trace, ExplainStep{Phase: "expression", Message: expr, Details: detail})
		return ok, nil
	}
	op, _ := cond["op"].(string)
	children, _ := cond["children"].([]any)
	switch op {
	case "all", "":
		for _, raw := range children {
			ok, err := evalNormalizedConditionTrace(raw.(map[string]any), input, opts, trace, owner)
			if err != nil || !ok {
				*trace = append(*trace, ExplainStep{Phase: "condition", Message: "all short-circuit", Details: map[string]any{"result": ok}})
				return ok, err
			}
		}
		return true, nil
	case "any":
		for _, raw := range children {
			ok, err := evalNormalizedConditionTrace(raw.(map[string]any), input, opts, trace, owner)
			if err != nil {
				return false, err
			}
			if ok {
				*trace = append(*trace, ExplainStep{Phase: "condition", Message: "any short-circuit", Details: map[string]any{"result": true}})
				return true, nil
			}
		}
		return false, nil
	case "not":
		if len(children) == 0 {
			return true, nil
		}
		ok, err := evalNormalizedConditionTrace(children[0].(map[string]any), input, opts, trace, owner)
		return !ok, err
	case "none":
		for _, raw := range children {
			ok, err := evalNormalizedConditionTrace(raw.(map[string]any), input, opts, trace, owner)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

func evalOptionsFrom(opts *Options, vars map[string]any) *EvalOptions {
	eopts := &EvalOptions{Variables: vars}
	if opts != nil {
		eopts.AllowHash = opts.AllowHash
		eopts.AllowEncoding = opts.AllowEncoding
		eopts.AllowTime = opts.AllowTime
		eopts.Functions = opts.EvalFunctions
		eopts.Now = opts.Now
	}
	return eopts
}

func evalExprBool(expr string, input map[string]any, opts *Options) (bool, error) {
	if ok, handled, err := evalSimpleExprBool(expr, input); handled {
		return ok, err
	}
	eopts := EvalOptions{Variables: input}
	if opts != nil {
		eopts.AllowHash = opts.AllowHash
		eopts.AllowEncoding = opts.AllowEncoding
		eopts.AllowTime = opts.AllowTime
		eopts.Functions = opts.EvalFunctions
		eopts.Now = opts.Now
	}
	v, err := EvalExpr(expr, &eopts)
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

func evalSimpleExprBool(expr string, input map[string]any) (bool, bool, error) {
	if strings.HasPrefix(expr, "match ") || strings.HasPrefix(expr, "match(") {
		return false, false, nil
	}
	for _, op := range []string{" not_in ", " starts_with ", " ends_with ", " contains ", " has_any ", " has_all ", " == ", " != ", " >= ", " <= ", " > ", " < ", " in ", " has "} {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		left, ok := simpleExprOperand(strings.TrimSpace(expr[:idx]), input)
		if !ok {
			return false, false, nil
		}
		right, ok := simpleExprOperand(strings.TrimSpace(expr[idx+len(op):]), input)
		if !ok {
			return false, false, nil
		}
		v, err := evalOp(strings.TrimSpace(op), left, right)
		if err != nil {
			return false, true, err
		}
		return truthy(v), true, nil
	}
	return false, false, nil
}

func simpleExprOperand(raw string, input map[string]any) (any, bool) {
	if raw == "" || strings.ContainsAny(raw, " ()[]{}+-*/%,") {
		return nil, false
	}
	if v, ok := parseSimpleOperandLiteral(raw); ok {
		return v, true
	}
	return lookup(input, raw), true
}

func parseSimpleOperandLiteral(raw string) (any, bool) {
	switch raw {
	case "true":
		return true, true
	case "false":
		return false, true
	case "null":
		return nil, true
	}
	if len(raw) >= 2 {
		q := raw[0]
		if (q == '"' || q == '\'') && raw[len(raw)-1] == q {
			if q == '"' {
				s, err := strconv.Unquote(raw)
				if err == nil {
					return s, true
				}
			}
			return raw[1 : len(raw)-1], true
		}
	}
	if strings.Contains(raw, ".") {
		f, err := convert.ToFloat64(raw)
		return f, err == nil
	}
	i, err := convert.ToInt64(raw)
	return i, err == nil
}

func blockID(block map[string]any) string {
	return stringValue(block["type"]) + "." + stringValue(block["id"])
}

func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return prefix + "." + name
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
