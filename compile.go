package bcl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Options struct {
	Profile                 string
	Env                     func(string) (string, bool)
	EnvFiles                []string
	Context                 map[string]any
	Session                 map[string]any
	AllowEnv                bool
	AllowTime               bool
	AllowHash               bool
	AllowEncoding           bool
	ResolveImports          bool
	ResolveModules          bool
	Interpolate             bool
	DisableInterpolation    bool
	Partial                 bool
	Strict                  bool
	Verbose                 bool
	LockfilePath            string
	BaseDir                 string
	Redact                  bool
	EvalFunctions           map[string]EvalFunction
	DecisionActions         map[string]DecisionActionHandler
	DecisionRankers         map[string]DecisionRankingScorer
	DecisionDatasetAdapters map[string]DecisionDatasetAdapter
	HTTPClient              *http.Client
	DecisionInputValidator  DecisionInputValidator
	Now                     func() time.Time
	AllowedDatasetAdapters  []string
	AllowedHTTPHosts        []string
	AllowedHTTPMethods      []string
	ExternalTimeout         time.Duration
}

func Compile(doc *Document, opts *Options) (*Normalized, error) {
	if opts == nil {
		opts = &Options{}
	}
	if opts.Env == nil {
		opts.Env = os.LookupEnv
	}
	opts.Interpolate = !opts.DisableInterpolation
	if opts.BaseDir == "" && doc.File != "" && doc.File != "<input>" {
		opts.BaseDir = filepath.Dir(doc.File)
	}
	if len(opts.EnvFiles) == 0 {
		baseDir := opts.BaseDir
		if baseDir == "" {
			baseDir = "."
		}
		if absDir, err := filepath.Abs(baseDir); err == nil {
			baseDir = absDir
		}
		candidate := filepath.Join(baseDir, ".env")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			opts.EnvFiles = append(opts.EnvFiles, candidate)
		}
	}
	topCap := len(doc.Items)
	c := &compiler{
		opts:        opts,
		out:         &Normalized{Body: make(map[string]any, topCap), Constants: map[string]any{}, Params: map[string]any{}, Predicates: map[string]any{}, Sets: map[string][]any{}, Types: map[string]string{}, Schemas: map[string]any{}, Namespaces: map[string]any{}},
		consts:      map[string]Value{},
		sets:        map[string][]Value{},
		types:       map[string]string{},
		schemaDecls: map[string]*SchemaDecl{},
		blockIndex:  map[string]*Block{},
		spreadStack: map[string]bool{},
	}
	c.loadEnvFiles(doc.Span, nil)
	items := doc.Items
	if opts.LockfilePath != "" {
		if lock, err := ReadLockfile(opts.LockfilePath); err == nil {
			c.lock = lock
		} else if opts.Strict {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: doc.Span})
		}
	}
	if opts.ResolveImports {
		items = c.resolveImports(items, opts.BaseDir, map[string]bool{})
	}
	if opts.ResolveModules {
		items = c.resolveModules(items, opts.BaseDir, map[string]bool{})
	}
	c.indexBlocks(items)
	c.loadEnvFiles(doc.Span, envFileDecls(items))
	c.collect(items)
	c.emit(items, c.out.Body)
	if opts.Profile == "" {
		if p, ok := c.out.Body["active_profile"].(string); ok {
			opts.Profile = p
		}
	}
	c.applyProfile()
	c.applyOverrides()
	if len(c.errs) > 0 {
		c.out.Diagnostics = append(c.out.Diagnostics, c.errs...)
		return c.out, c.errs
	}
	return c.out, nil
}

type compiler struct {
	opts        *Options
	out         *Normalized
	consts      map[string]Value
	sets        map[string][]Value
	types       map[string]string
	schemaDecls map[string]*SchemaDecl
	blockIndex  map[string]*Block
	spreadStack map[string]bool
	lock        *Lockfile
	result      *CompileResult
	errs        ErrorList
}

func (c *compiler) indexBlocks(nodes []Node) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *Block:
			if x.ID != "" {
				key := x.Type + "." + x.ID
				if c.blockIndex[key] == nil {
					c.blockIndex[key] = x
				}
			}
			c.indexBlocks(x.Body)
		case *Assignment:
			if o, ok := x.Value.(*Object); ok {
				c.indexBlocks(o.Fields)
			}
		case *Spread:
			c.indexBlocks(x.Body)
		}
	}
}

func (c *compiler) loadEnvFiles(sp Span, declared []string) {
	files := append([]string(nil), c.opts.EnvFiles...)
	files = append(files, declared...)
	if len(files) == 0 {
		return
	}
	paths := make([]string, 0, len(files))
	for _, path := range files {
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) && c.opts.BaseDir != "" {
			path = filepath.Join(c.opts.BaseDir, path)
		}
		paths = append(paths, path)
	}
	values, err := LoadEnvFiles(paths...)
	if err != nil {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: sp})
		return
	}
	parent := c.opts.Env
	c.opts.Env = func(key string) (string, bool) {
		if v, ok := parent(key); ok {
			return v, true
		}
		v, ok := values[key]
		return v, ok
	}
}

func envFileDecls(nodes []Node) []string {
	var files []string
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *Assignment:
				switch x.Name {
				case "env_file":
					if s, ok := literalEnvPath(x.Value); ok {
						files = append(files, s)
					}
				case "env_files":
					if list, ok := x.Value.(*List); ok {
						for _, item := range list.Items {
							if s, ok := literalEnvPath(item); ok {
								files = append(files, s)
							}
						}
					}
				}
				if o, ok := x.Value.(*Object); ok {
					walk(o.Fields)
				}
			case *Block:
				walk(x.Body)
			}
		}
	}
	walk(nodes)
	return files
}

func literalEnvPath(v Value) (string, bool) {
	if lit, ok := v.(*Literal); ok {
		if s, ok := lit.Data.(string); ok {
			return s, true
		}
	}
	return "", false
}

func (c *compiler) resolveImports(nodes []Node, baseDir string, seen map[string]bool) []Node {
	var out []Node
	for _, n := range nodes {
		imp, ok := n.(*ImportDecl)
		if !ok {
			out = append(out, n)
			continue
		}
		c.out.Imports = append(c.out.Imports, map[string]string{"path": imp.Path, "alias": imp.Alias})
		if err := c.checkLock(imp.Path, baseDir, imp.Span); err != nil {
			c.errs = append(c.errs, *err)
			if c.opts.Strict {
				continue
			}
		}
		matches, err := resolveSourceFiles(imp.Path, baseDir)
		if err != nil {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: imp.Span})
			continue
		}
		var imported []Node
		for _, path := range matches {
			if seen[path] {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("cyclic import %q", path), Span: imp.Span})
				continue
			}
			seen[path] = true
			doc, err := ParsePath(path)
			if err != nil {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: imp.Span})
				continue
			}
			imported = append(imported, c.resolveImports(doc.Items, filepath.Dir(path), seen)...)
			delete(seen, path)
		}
		if imp.Alias != "" {
			out = append(out, &Block{Type: "namespace", ID: imp.Alias, Body: imported, Span: imp.Span})
		} else {
			out = append(out, imported...)
		}
	}
	return out
}

func (c *compiler) resolveModules(nodes []Node, baseDir string, seen map[string]bool) []Node {
	var out []Node
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok || b.Type != "module" {
			out = append(out, n)
			continue
		}
		src := blockString(b, "source")
		inputs := blockObject(b, "inputs", c)
		mod := map[string]any{"id": b.ID, "source": src, "inputs": inputs}
		c.out.Modules = append(c.out.Modules, mod)
		if err := c.checkLock(src, baseDir, b.Span); err != nil {
			c.errs = append(c.errs, *err)
			if c.opts.Strict {
				out = append(out, n)
				continue
			}
		}
		if src == "" || isRemoteSource(src) {
			out = append(out, n)
			continue
		}
		matches, err := resolveModuleFiles(src, baseDir)
		if err != nil {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: b.Span})
			continue
		}
		var imported []Node
		var moduleParams []*ParamDecl
		for _, path := range matches {
			if seen[path] {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("cyclic module %q", path), Span: b.Span})
				continue
			}
			seen[path] = true
			doc, err := ParsePath(path)
			if err != nil {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: b.Span})
				continue
			}
			moduleParams = append(moduleParams, collectParamDecls(doc.Items)...)
			imported = append(imported, c.resolveImports(doc.Items, filepath.Dir(path), seen)...)
			delete(seen, path)
		}
		c.validateModuleInputs(b, moduleParams, inputs)
		out = append(out, &Block{Type: "namespace", ID: b.ID, Body: imported, Span: b.Span})
	}
	return out
}

func collectParamDecls(nodes []Node) []*ParamDecl {
	var out []*ParamDecl
	for _, n := range nodes {
		if p, ok := n.(*ParamDecl); ok {
			out = append(out, p)
		}
	}
	return out
}

func (c *compiler) validateModuleInputs(module *Block, params []*ParamDecl, inputs map[string]any) {
	if len(params) == 0 {
		return
	}
	known := map[string]*ParamDecl{}
	for _, p := range params {
		known[p.Name] = p
		if p.Required {
			if inputs == nil {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q missing required input %q", module.ID, p.Name), Span: module.Span})
				continue
			}
			if _, ok := inputs[p.Name]; !ok {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q missing required input %q", module.ID, p.Name), Span: module.Span})
			}
		}
	}
	for name, value := range inputs {
		p := known[name]
		if p == nil {
			c.errs = append(c.errs, Diagnostic{Severity: "warning", Message: fmt.Sprintf("module %q input %q is not declared by module params", module.ID, name), Span: module.Span})
			continue
		}
		if !valueMatchesParamType(value, p.Type) {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q input %q must be %s", module.ID, name, p.Type), Span: module.Span})
		}
	}
}

func valueMatchesParamType(v any, typ string) bool {
	switch typ {
	case "", "any":
		return true
	case "string":
		_, ok := v.(string)
		return ok
	case "int", "integer":
		switch v.(type) {
		case int, int64, float64:
			return true
		default:
			return false
		}
	case "float", "number":
		switch v.(type) {
		case int, int64, float64:
			return true
		default:
			return false
		}
	case "bool", "boolean":
		_, ok := v.(bool)
		return ok
	case "list":
		_, ok := v.([]any)
		return ok
	case "map", "object":
		_, ok := v.(map[string]any)
		return ok
	default:
		return true
	}
}

func (c *compiler) checkLock(source, baseDir string, sp Span) *Diagnostic {
	if source == "" || c.lock == nil {
		if isRemoteSource(source) || c.opts.Strict && c.opts.LockfilePath != "" {
			return &Diagnostic{Severity: "error", Message: fmt.Sprintf("missing lock entry for %q", source), Span: sp}
		}
		return nil
	}
	for _, entry := range lockEntriesForSource(source, baseDir) {
		locked := c.lock.Find(entry.Source)
		if locked == nil {
			locked = c.lock.Find(entry.Resolved)
		}
		if locked == nil {
			return &Diagnostic{Severity: "error", Message: fmt.Sprintf("missing lock entry for %q", entry.Source), Span: sp}
		}
		if err := VerifyLockEntry(*locked); err != nil {
			return &Diagnostic{Severity: "error", Message: err.Error(), Span: sp}
		}
	}
	return nil
}

func (c *compiler) collect(nodes []Node) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *ConstDecl:
			c.consts[x.Name] = x.Value
			c.out.Constants[x.Name] = c.value(x.Value)
		case *ImportDecl:
			if !c.opts.ResolveImports {
				c.out.Imports = append(c.out.Imports, map[string]string{"path": x.Path, "alias": x.Alias})
			}
		case *ParamDecl:
			c.out.Params[x.Name] = paramToMap(x, c)
		case *SchemaDecl:
			c.schemaDecls[x.Name] = x
			c.out.Schemas[x.Name] = schemaToMap(x, c)
		case *TypeDecl:
			c.types[x.Name] = x.Type
			c.out.Types[x.Name] = x.Type
		case *Block:
			if x.Type == "namespace" && x.ID != "" {
				ns := &compiler{opts: c.opts, out: &Normalized{Body: map[string]any{}, Constants: map[string]any{}, Params: map[string]any{}, Predicates: map[string]any{}, Sets: map[string][]any{}, Types: map[string]string{}, Schemas: map[string]any{}}, consts: c.consts, sets: c.sets, types: c.types, schemaDecls: c.schemaDecls, blockIndex: c.blockIndex, spreadStack: map[string]bool{}}
				ns.collect(x.Body)
				nsBody := map[string]any{}
				ns.emit(x.Body, nsBody)
				c.out.Namespaces[x.ID] = map[string]any{"body": nsBody, "blocks": ns.out.Blocks, "constants": ns.out.Constants, "params": ns.out.Params, "predicates": ns.out.Predicates, "sets": ns.out.Sets, "types": ns.out.Types, "schemas": ns.out.Schemas}
				continue
			}
			if x.Type == "predicate" && x.ID != "" {
				if cond, ok := buildCondition("all", x.Body, x.Span).(*Condition); ok {
					c.out.Predicates[x.ID] = c.conditionToInterface(cond)
				} else {
					c.out.Predicates[x.ID] = c.block(x)["body"]
				}
			}
			if x.Type == "test" && x.ID != "" {
				c.out.Tests = append(c.out.Tests, c.block(x))
			}
			if x.Type == "set" && x.ID != "" {
				var vals []Value
				for _, item := range x.Body {
					if a, ok := item.(*Assignment); ok && a.Value != nil {
						if ref, ok := a.Value.(*Reference); ok && ref.Path == "" {
							vals = append(vals, &Literal{Type: "identifier", Data: a.Name, Span: a.Span})
						} else {
							vals = append(vals, a.Value)
						}
					}
				}
				c.sets[x.ID] = vals
				for _, v := range vals {
					c.out.Sets[x.ID] = append(c.out.Sets[x.ID], c.value(v))
				}
			}
			if x.Type == "bcl" {
				for _, item := range x.Body {
					if a, ok := item.(*Assignment); ok {
						if a.Name == "version" {
							if s, ok := c.value(a.Value).(string); ok {
								c.out.Version = s
							}
						}
						if a.Name == "strict" {
							if strict, ok := c.value(a.Value).(bool); ok && strict {
								c.opts.Strict = true
							}
						}
					}
				}
			}
			c.collect(x.Body)
		case *Assignment:
			if o, ok := x.Value.(*Object); ok {
				c.collect(o.Fields)
			}
		}
	}
}

func (c *compiler) emit(nodes []Node, body map[string]any) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *Assignment:
			if x.Name == "env_file" || x.Name == "env_files" {
				continue
			}
			setNormalized(body, x.Name, c.assignmentValue(x))
		case *Block:
			switch x.Type {
			case "set", "bcl", "schema", "predicate", "test":
				continue
			case "namespace":
				continue
			default:
				c.out.Blocks = append(c.out.Blocks, c.block(x))
			}
		case *Spread:
			if merged := c.spreadBody("", x); merged != nil {
				mergeMap(body, merged)
			}
		}
	}
}

func paramToMap(p *ParamDecl, c *compiler) map[string]any {
	out := map[string]any{"type": p.Type}
	if p.Required {
		out["required"] = true
	}
	if p.Default != nil {
		out["default"] = c.value(p.Default)
	}
	if p.Description != "" {
		out["description"] = p.Description
	}
	return out
}

func (c *compiler) block(b *Block) map[string]any {
	out := make(map[string]any, 3)
	out["type"] = b.Type
	if b.ID != "" {
		out["id"] = b.ID
	}
	body := make(map[string]any, len(b.Body))
	for _, n := range b.Body {
		switch x := n.(type) {
		case *Assignment:
			setNormalized(body, x.Name, c.assignmentValue(x))
		case *Block:
			key := c.blockCollectionKey(b.Type, x.Type)
			body[key] = appendBlock(body[key], c.block(x))
		case *Spread:
			if merged := c.spreadBody(b.Type, x); merged != nil {
				mergeMap(body, merged)
			}
		}
	}
	out["body"] = body
	c.applySchemaDefaults(b.Type, body)
	return out
}

func (c *compiler) blockCollectionKey(parentType, childType string) string {
	if parentType == "" || childType == "" {
		return childType
	}
	s := c.schemaDecls[parentType]
	if s == nil {
		return childType
	}
	for _, f := range s.Fields {
		ft := resolveBuiltinAlias(resolveAlias(f.Type, c.types))
		if !strings.HasPrefix(ft, "list") && !strings.HasPrefix(ft, "array") {
			continue
		}
		if sameCollectionName(f.Name, childType) {
			return f.Name
		}
	}
	return childType
}

func (c *compiler) applySchemaDefaults(blockType string, body map[string]any) {
	s := c.schemaDecls[blockType]
	if s == nil {
		return
	}
	for _, f := range s.Fields {
		if _, ok := body[f.Name]; !ok && f.Default != nil {
			body[f.Name] = c.value(f.Default)
		}
	}
}

func (c *compiler) spreadBody(currentType string, s *Spread) map[string]any {
	target := s.Target
	if target == "" {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: "missing spread target", Span: s.Span})
		return nil
	}
	if !strings.Contains(target, ".") && currentType != "" {
		target = currentType + "." + target
	}
	if c.spreadStack[target] {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("cyclic spread %q", target), Span: s.Span})
		return nil
	}
	base := c.blockIndex[target]
	if base == nil {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown spread target %q", s.Target), Span: s.Span})
		return nil
	}
	c.spreadStack[target] = true
	compiled := c.block(base)
	delete(c.spreadStack, target)
	body, _ := compiled["body"].(map[string]any)
	merged, _ := cloneAny(body).(map[string]any)
	if merged == nil {
		merged = map[string]any{}
	}
	if len(s.Body) > 0 {
		overrides := c.nodesToBody(s.Body, currentType)
		mergeMap(merged, overrides)
	}
	return merged
}

func (c *compiler) nodesToBody(nodes []Node, currentType string) map[string]any {
	body := make(map[string]any, len(nodes))
	for _, n := range nodes {
		switch x := n.(type) {
		case *Assignment:
			setNormalized(body, x.Name, c.assignmentValue(x))
		case *Block:
			key := c.blockCollectionKey(currentType, x.Type)
			body[key] = appendBlock(body[key], c.block(x))
		case *Spread:
			if merged := c.spreadBody(currentType, x); merged != nil {
				mergeMap(body, merged)
			}
		}
	}
	return body
}

func (c *compiler) value(v Value) any {
	return c.valueWithRedact(v, false)
}

func (c *compiler) assignmentValue(a *Assignment) any {
	if a.Name == "$expr" {
		if expr, ok := a.Value.(*Expr); ok {
			return map[string]any{"$expr": expr.Raw}
		}
	}
	return c.valueWithRedact(a.Value, a.Sensitive)
}

func (c *compiler) valueWithRedact(v Value, sensitive bool) any {
	switch x := v.(type) {
	case *Literal:
		if sensitive || x.Sensitive {
			return "****"
		}
		if x.Type == "string" {
			if s, ok := x.Data.(string); ok && c.opts.Interpolate {
				return c.interpolate(s)
			}
		}
		switch x.Type {
		case "duration", "bytes", "date", "datetime":
			return map[string]any{"$" + x.Type: x.Data}
		}
		return x.Data
	case *Reference:
		if x.Path == "" {
			return nil
		}
		switch x.Path {
		case "CURRENT_TIMESTAMP":
			if !c.opts.AllowTime {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: "CURRENT_TIMESTAMP requires time capability", Span: x.Span})
				return nil
			}
			return optionsNow(c.opts).Format(time.RFC3339)
		case "CURRENT_DATE":
			if !c.opts.AllowTime {
				c.errs = append(c.errs, Diagnostic{Severity: "error", Message: "CURRENT_DATE requires time capability", Span: x.Span})
				return nil
			}
			return optionsNow(c.opts).Format("2006-01-02")
		}
		if cv, ok := c.consts[x.Path]; ok {
			return c.value(cv)
		}
		return map[string]any{"$ref": x.Path}
	case *List:
		out := make([]any, 0, len(x.Items))
		for _, item := range x.Items {
			out = append(out, c.value(item))
		}
		return out
	case *Object:
		m := make(map[string]any, len(x.Fields))
		for _, n := range x.Fields {
			switch y := n.(type) {
			case *Assignment:
				setNormalized(m, y.Name, c.assignmentValue(y))
			case *Block:
				m[y.Type] = appendBlock(m[y.Type], c.block(y))
			case *Spread:
				if merged := c.spreadBody("", y); merged != nil {
					mergeMap(m, merged)
				}
			}
		}
		return m
	case *Expr:
		v, err := EvalExpr(x.Raw, &EvalOptions{Variables: c.evalVars(), AllowEncoding: c.opts.AllowEncoding, AllowHash: c.opts.AllowHash, AllowTime: c.opts.AllowTime, Functions: c.opts.EvalFunctions, Now: c.opts.Now})
		if err != nil {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: x.Span})
			return map[string]any{"$expr": x.Raw}
		}
		return v
	case *Call:
		return c.call(x)
	case *Condition:
		return c.conditionToInterface(x)
	default:
		return nil
	}
}

func (c *compiler) conditionToInterface(cond *Condition) any {
	if cond.Expr != nil {
		if name, ok := predicateRefName(cond.Expr.Raw); ok {
			if pred, ok := c.out.Predicates[name]; ok {
				return pred
			}
		}
		return map[string]any{"op": cond.Op, "expr": cond.Expr.Raw}
	}
	children := make([]any, 0, len(cond.Children))
	for _, child := range cond.Children {
		children = append(children, c.conditionToInterface(child))
	}
	return map[string]any{"op": cond.Op, "children": children}
}

func predicateRefName(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "predicate.") {
		name := strings.TrimPrefix(raw, "predicate.")
		if name != "" && !strings.ContainsAny(name, " \t\n\r()[]{}") {
			return name, true
		}
	}
	return "", false
}

func (c *compiler) call(x *Call) any {
	switch x.Name {
	case "env", "env.required", "env.int", "env.bool", "env.float", "env.duration", "env.bytes", "env.list":
		if !c.opts.AllowEnv {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: "env function requires AllowEnv capability", Span: x.Span})
			return nil
		}
		return c.envCall(x)
	case "context", "context.required", "context.int", "context.bool", "context.float", "context.duration", "context.bytes", "context.list":
		return c.scopeCall("context", c.opts.Context, x)
	case "session", "session.required", "session.int", "session.bool", "session.float", "session.duration", "session.bytes", "session.list":
		return c.scopeCall("session", c.opts.Session, x)
	case "set":
		if len(x.Args) == 1 {
			if name, ok := c.value(x.Args[0]).(string); ok {
				vals := c.sets[name]
				out := make([]any, 0, len(vals))
				for _, v := range vals {
					out = append(out, c.value(v))
				}
				return out
			}
		}
	case "sensitive":
		if len(x.Args) == 1 {
			return "****"
		}
	case "regex", "cidr", "duration", "ip", "url", "email", "bytes":
		if len(x.Args) == 1 {
			return map[string]any{"$" + x.Name: c.value(x.Args[0])}
		}
	case "json":
		if len(x.Args) == 1 {
			return map[string]any{"$json": c.value(x.Args[0])}
		}
	case "now", "current_timestamp", "current_date", "current_time", "unix_timestamp", "unix_millis", "today", "uuid", "uuid_v4", "random_uuid", "uid", "unique_id", "date", "time", "datetime", "timestamp":
		v, err := c.generatedCall(x)
		if err != nil {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: err.Error(), Span: x.Span})
			return nil
		}
		return v
	case "concat", "lower", "upper", "trim", "coalesce":
		args := make([]any, 0, len(x.Args))
		for _, a := range x.Args {
			args = append(args, c.value(a))
		}
		v, err := EvalExpr(callToExpr(x.Name, args), &EvalOptions{Variables: c.evalVars(), AllowEncoding: c.opts.AllowEncoding, AllowHash: c.opts.AllowHash, AllowTime: c.opts.AllowTime, Functions: c.opts.EvalFunctions, Now: c.opts.Now})
		if err == nil {
			return v
		}
	}
	args := make([]any, 0, len(x.Args))
	for _, a := range x.Args {
		args = append(args, c.value(a))
	}
	return map[string]any{"$call": x.Name, "args": args}
}

func (c *compiler) generatedCall(x *Call) (any, error) {
	switch x.Name {
	case "now", "current_timestamp":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("%s requires 0 arguments", x.Name)
		}
		if !c.opts.AllowTime {
			return nil, fmt.Errorf("%s requires time capability", x.Name)
		}
		return optionsNow(c.opts).Format(time.RFC3339), nil
	case "today", "current_date":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("%s requires 0 arguments", x.Name)
		}
		if !c.opts.AllowTime {
			return nil, fmt.Errorf("%s requires time capability", x.Name)
		}
		return optionsNow(c.opts).Format("2006-01-02"), nil
	case "current_time":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("current_time requires 0 arguments")
		}
		if !c.opts.AllowTime {
			return nil, fmt.Errorf("current_time requires time capability")
		}
		return optionsNow(c.opts).Format("15:04:05"), nil
	case "unix_timestamp":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("unix_timestamp requires 0 arguments")
		}
		if !c.opts.AllowTime {
			return nil, fmt.Errorf("unix_timestamp requires time capability")
		}
		return optionsNow(c.opts).Unix(), nil
	case "unix_millis":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("unix_millis requires 0 arguments")
		}
		if !c.opts.AllowTime {
			return nil, fmt.Errorf("unix_millis requires time capability")
		}
		return optionsNow(c.opts).UnixMilli(), nil
	case "date":
		if len(x.Args) == 0 {
			if !c.opts.AllowTime {
				return nil, fmt.Errorf("date requires time capability")
			}
			return map[string]any{"$date": optionsNow(c.opts).Format("2006-01-02")}, nil
		}
		if len(x.Args) == 1 {
			return map[string]any{"$date": c.value(x.Args[0])}, nil
		}
		return nil, fmt.Errorf("date requires 0 or 1 arguments")
	case "time":
		if len(x.Args) == 0 {
			if !c.opts.AllowTime {
				return nil, fmt.Errorf("time requires time capability")
			}
			return map[string]any{"$time": optionsNow(c.opts).Format("15:04:05")}, nil
		}
		if len(x.Args) == 1 {
			return map[string]any{"$time": c.value(x.Args[0])}, nil
		}
		return nil, fmt.Errorf("time requires 0 or 1 arguments")
	case "datetime", "timestamp":
		name := x.Name
		if name == "timestamp" {
			name = "datetime"
		}
		if len(x.Args) == 0 {
			if !c.opts.AllowTime {
				return nil, fmt.Errorf("%s requires time capability", x.Name)
			}
			return map[string]any{"$" + name: optionsNow(c.opts).Format(time.RFC3339)}, nil
		}
		if len(x.Args) == 1 {
			return map[string]any{"$" + name: c.value(x.Args[0])}, nil
		}
		return nil, fmt.Errorf("%s requires 0 or 1 arguments", x.Name)
	case "uuid", "uuid_v4", "random_uuid":
		if len(x.Args) != 0 {
			return nil, fmt.Errorf("%s requires 0 arguments", x.Name)
		}
		return randomUUID()
	case "unique_id", "uid":
		prefix := "id"
		if len(x.Args) > 0 {
			if s := stringValue(c.value(x.Args[0])); s != "" {
				prefix = s
			}
		}
		id, err := randomHex(12)
		if err != nil {
			return nil, err
		}
		return prefix + "_" + id, nil
	default:
		return nil, fmt.Errorf("unsupported generated function %q", x.Name)
	}
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:]), nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var interpolationPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func (c *compiler) interpolate(s string) string {
	return interpolationPattern.ReplaceAllStringFunc(s, func(match string) string {
		expr := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		v, err := EvalExpr(expr, &EvalOptions{Variables: c.evalVars(), AllowEncoding: c.opts.AllowEncoding, AllowHash: c.opts.AllowHash, AllowTime: c.opts.AllowTime, Functions: c.opts.EvalFunctions})
		if err != nil || v == nil {
			return match
		}
		return fmt.Sprint(v)
	})
}

func (c *compiler) evalVars() map[string]any {
	vars := map[string]any{
		"config":  map[string]any{"app": c.out.Body},
		"app":     c.out.Body,
		"const":   c.out.Constants,
		"sets":    c.out.Sets,
		"context": c.opts.Context,
		"session": c.opts.Session,
	}
	for k, v := range c.out.Body {
		vars[k] = v
	}
	for k, v := range c.out.Constants {
		vars[k] = v
	}
	return vars
}

func callToExpr(name string, args []any) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('(')
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		if s, ok := a.(string); ok {
			b.WriteString(strconvQuote(s))
		} else {
			b.WriteString(fmt.Sprint(a))
		}
	}
	b.WriteByte(')')
	return b.String()
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (c *compiler) envCall(x *Call) any {
	if len(x.Args) == 0 {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: "env call requires a key", Span: x.Span})
		return nil
	}
	key, _ := c.value(x.Args[0]).(string)
	val, ok := c.opts.Env(key)
	if !ok {
		if x.Name == "env.required" {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("required env %q is not set", key), Span: x.Span})
			return nil
		}
		if len(x.Args) > 1 {
			return c.value(x.Args[1])
		}
		return ""
	}
	switch x.Name {
	case "env.int":
		var i int64
		fmt.Sscan(val, &i)
		return i
	case "env.bool":
		return val == "true" || val == "1" || val == "yes"
	case "env.float":
		var f float64
		fmt.Sscan(val, &f)
		return f
	case "env.list":
		sep := ","
		if len(x.Args) > 1 {
			if s, ok := c.value(x.Args[1]).(string); ok {
				sep = s
			}
		}
		return strings.Split(val, sep)
	case "env.duration":
		return map[string]any{"$duration": val}
	case "env.bytes":
		return map[string]any{"$bytes": val}
	default:
		return val
	}
}

func (c *compiler) scopeCall(scopeName string, scope map[string]any, x *Call) any {
	if len(x.Args) == 0 {
		c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("%s call requires a key", scopeName), Span: x.Span})
		return nil
	}
	key, _ := c.value(x.Args[0]).(string)
	val := lookup(scope, key)
	if val == nil {
		if x.Name == scopeName+".required" {
			c.errs = append(c.errs, Diagnostic{Severity: "error", Message: fmt.Sprintf("required %s %q is not set", scopeName, key), Span: x.Span})
			return nil
		}
		if len(x.Args) > 1 {
			return c.value(x.Args[1])
		}
		return nil
	}
	switch x.Name {
	case scopeName + ".int":
		if i, ok := numericInt(val); ok {
			return i
		}
		var i int64
		fmt.Sscan(fmt.Sprint(val), &i)
		return i
	case scopeName + ".bool":
		if b, ok := val.(bool); ok {
			return b
		}
		s := strings.ToLower(fmt.Sprint(val))
		return s == "true" || s == "1" || s == "yes"
	case scopeName + ".float":
		if f, ok := numericFloat(val); ok {
			return f
		}
		var f float64
		fmt.Sscan(fmt.Sprint(val), &f)
		return f
	case scopeName + ".list":
		switch xs := val.(type) {
		case []any:
			return xs
		case []string:
			out := make([]any, 0, len(xs))
			for _, item := range xs {
				out = append(out, item)
			}
			return out
		}
		sep := ","
		if len(x.Args) > 1 {
			if s, ok := c.value(x.Args[1]).(string); ok {
				sep = s
			}
		}
		return strings.Split(fmt.Sprint(val), sep)
	case scopeName + ".duration":
		return map[string]any{"$duration": fmt.Sprint(val)}
	case scopeName + ".bytes":
		return map[string]any{"$bytes": fmt.Sprint(val)}
	default:
		return val
	}
}

func (c *compiler) applyProfile() {
	if c.opts.Profile == "" {
		return
	}
	for _, b := range c.out.Blocks {
		if b["type"] == "profile" && b["id"] == c.opts.Profile {
			if body, ok := b["body"].(map[string]any); ok {
				for k, v := range body {
					if k == "override" {
						continue
					}
					c.out.Body[k] = v
				}
			}
		}
	}
}

func (c *compiler) applyOverrides() {
	var applyBlockOverride func(target string, body map[string]any)
	applyBlockOverride = func(target string, body map[string]any) {
		parts := strings.SplitN(target, ".", 2)
		if len(parts) != 2 {
			if existing, ok := c.out.Body[target].(map[string]any); ok {
				mergeMap(existing, body)
			} else {
				c.out.Body[target] = body
			}
			return
		}
		for _, block := range c.out.Blocks {
			if block["type"] == parts[0] && block["id"] == parts[1] {
				if dst, ok := block["body"].(map[string]any); ok {
					mergeMap(dst, body)
				}
			}
		}
	}
	for _, block := range c.out.Blocks {
		if block["type"] == "override" {
			if body, ok := block["body"].(map[string]any); ok {
				if id, ok := block["id"].(string); ok {
					applyBlockOverride(id, body)
				} else {
					mergeMap(c.out.Body, body)
				}
			}
		}
	}
	if c.opts.Profile != "" {
		for _, block := range c.out.Blocks {
			if block["type"] == "profile" && block["id"] == c.opts.Profile {
				if body, ok := block["body"].(map[string]any); ok {
					if overrides, ok := body["override"].([]any); ok {
						for _, raw := range overrides {
							if ov, ok := raw.(map[string]any); ok {
								id, _ := ov["id"].(string)
								b, _ := ov["body"].(map[string]any)
								applyBlockOverride(id, b)
							}
						}
					}
				}
			}
		}
	}
	var filtered []map[string]any
	for _, block := range c.out.Blocks {
		if block["type"] != "override" {
			filtered = append(filtered, block)
		}
	}
	c.out.Blocks = filtered
}

func schemaToMap(s *SchemaDecl, c *compiler) any {
	m := map[string]any{"fields": schemaFieldsToMaps(s.Fields, c)}
	if len(s.Options) > 0 {
		options := map[string]any{}
		for _, key := range sortedValueKeys(s.Options) {
			options[key] = c.value(s.Options[key])
		}
		m["options"] = options
	}
	if len(s.Sections) > 0 {
		sections := map[string]any{}
		for _, key := range sortedValueKeys(s.Sections) {
			sections[key] = c.value(s.Sections[key])
		}
		m["sections"] = sections
	}
	return m
}

func schemaFieldsToMaps(schemaFields []SchemaField, c *compiler) []map[string]any {
	fields := make([]map[string]any, 0, len(schemaFields))
	for _, f := range schemaFields {
		fields = append(fields, schemaFieldToMap(f, c))
	}
	return fields
}

func schemaFieldToMap(f SchemaField, c *compiler) map[string]any {
	m := map[string]any{"name": f.Name, "type": f.Type, "required": f.Required}
	if f.Ref != "" {
		m["ref"] = f.Ref
	}
	if f.Const != nil {
		m["const"] = c.value(f.Const)
	}
	if f.Default != nil {
		m["default"] = c.value(f.Default)
	}
	if len(f.Enum) > 0 {
		var vals []any
		for _, v := range f.Enum {
			vals = append(vals, c.value(v))
		}
		m["enum"] = vals
	}
	if len(f.Fields) > 0 {
		m["fields"] = schemaFieldsToMaps(f.Fields, c)
	}
	if f.Items != "" {
		m["items"] = f.Items
	}
	if len(f.PrefixItems) > 0 {
		m["prefix_items"] = append([]string(nil), f.PrefixItems...)
	}
	if f.Contains != "" {
		m["contains"] = f.Contains
	}
	if f.Description != "" {
		m["description"] = f.Description
	}
	if f.Title != "" {
		m["title"] = f.Title
	}
	if f.Deprecated != "" {
		m["deprecated"] = f.Deprecated
	}
	if f.Sensitive {
		m["sensitive"] = true
	}
	if f.Generated {
		m["generated"] = true
	}
	if f.Derived {
		m["derived"] = true
	}
	if f.ReadOnly {
		m["read_only"] = true
	}
	if f.WriteOnly {
		m["write_only"] = true
	}
	if f.Nullable {
		m["nullable"] = true
	}
	if f.UniqueItems {
		m["unique_items"] = true
	}
	if f.ClosedSet {
		m["closed"] = f.Closed
	}
	if f.AdditionalProperties != nil {
		m["additional_properties"] = *f.AdditionalProperties
	}
	if f.Min != nil {
		m["min"] = c.value(f.Min)
	}
	if f.Max != nil {
		m["max"] = c.value(f.Max)
	}
	if f.ExclusiveMin != nil {
		m["exclusive_min"] = c.value(f.ExclusiveMin)
	}
	if f.ExclusiveMax != nil {
		m["exclusive_max"] = c.value(f.ExclusiveMax)
	}
	if f.MultipleOf != nil {
		m["multiple_of"] = c.value(f.MultipleOf)
	}
	if f.MinLen != nil {
		m["min_len"] = c.value(f.MinLen)
	}
	if f.MaxLen != nil {
		m["max_len"] = c.value(f.MaxLen)
	}
	if f.MinItems != nil {
		m["min_items"] = c.value(f.MinItems)
	}
	if f.MaxItems != nil {
		m["max_items"] = c.value(f.MaxItems)
	}
	if f.MinProps != nil {
		m["min_props"] = c.value(f.MinProps)
	}
	if f.MaxProps != nil {
		m["max_props"] = c.value(f.MaxProps)
	}
	if f.Pattern != "" {
		m["pattern"] = f.Pattern
	}
	if f.Format != "" {
		m["format"] = f.Format
	}
	if f.ContentEncoding != "" {
		m["content_encoding"] = f.ContentEncoding
	}
	if f.ContentMediaType != "" {
		m["content_media_type"] = f.ContentMediaType
	}
	if len(f.Examples) > 0 {
		vals := make([]any, 0, len(f.Examples))
		for _, v := range f.Examples {
			vals = append(vals, c.value(v))
		}
		m["examples"] = vals
	}
	if f.PatternProperties != nil {
		m["pattern_properties"] = c.value(f.PatternProperties)
	}
	if f.DependentRequired != nil {
		m["dependent_required"] = c.value(f.DependentRequired)
	}
	if f.LTField != "" {
		m["lt_field"] = f.LTField
	}
	if f.LTEField != "" {
		m["lte_field"] = f.LTEField
	}
	if f.GTField != "" {
		m["gt_field"] = f.GTField
	}
	if f.GTEField != "" {
		m["gte_field"] = f.GTEField
	}
	if f.EqField != "" {
		m["eq_field"] = f.EqField
	}
	if len(f.AllOf) > 0 {
		m["all_of"] = append([]string(nil), f.AllOf...)
	}
	if len(f.AnyOf) > 0 {
		m["any_of"] = append([]string(nil), f.AnyOf...)
	}
	if len(f.OneOf) > 0 {
		m["one_of"] = append([]string(nil), f.OneOf...)
	}
	if f.Not != "" {
		m["not"] = f.Not
	}
	if f.If != "" {
		m["if"] = f.If
	}
	if f.Then != "" {
		m["then"] = f.Then
	}
	if f.Else != "" {
		m["else"] = f.Else
	}
	if f.Classification != "" {
		m["classification"] = f.Classification
	}
	if f.Audit != "" {
		m["audit"] = f.Audit
	}
	if f.Explain != "" {
		m["explain"] = f.Explain
	}
	if f.PII != "" {
		m["pii"] = f.PII
	}
	if f.PolicyTag != "" {
		m["policy_tag"] = f.PolicyTag
	}
	if f.Owner != "" {
		m["owner"] = f.Owner
	}
	if f.Severity != "" {
		m["severity"] = f.Severity
	}
	for k, v := range f.Extensions {
		m[k] = c.value(v)
	}
	return m
}

func schemaToMapOld(s *SchemaDecl, c *compiler) any {
	fields := make([]map[string]any, 0, len(s.Fields))
	for _, f := range s.Fields {
		m := map[string]any{"name": f.Name, "type": f.Type, "required": f.Required}
		if f.Default != nil {
			m["default"] = c.value(f.Default)
		}
		if len(f.Enum) > 0 {
			var vals []any
			for _, v := range f.Enum {
				vals = append(vals, c.value(v))
			}
			m["enum"] = vals
		}
		fields = append(fields, m)
	}
	return map[string]any{"fields": fields}
}

func CompileBytes(src []byte, opts *Options) (*Normalized, error) {
	doc, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return Compile(doc, opts)
}

func CompileFile(path string, opts *Options) (*Normalized, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	opts.BaseDir = filepath.Dir(path)
	return Compile(doc, opts)
}

func ResolveDocument(doc *Document, opts *Options) (*Document, []Diagnostic) {
	if opts == nil {
		opts = &Options{}
	}
	if opts.BaseDir == "" && doc.File != "" && doc.File != "<input>" {
		opts.BaseDir = filepath.Dir(doc.File)
	}
	c := &compiler{
		opts:        opts,
		out:         &Normalized{},
		consts:      map[string]Value{},
		sets:        map[string][]Value{},
		types:       map[string]string{},
		schemaDecls: map[string]*SchemaDecl{},
	}
	items := doc.Items
	if opts.ResolveImports {
		items = c.resolveImports(items, opts.BaseDir, map[string]bool{})
	}
	if opts.ResolveModules {
		items = c.resolveModules(items, opts.BaseDir, map[string]bool{})
	}
	resolved := &Document{File: doc.File, Items: items, Span: doc.Span}
	return resolved, c.errs
}

func ToJSON(src []byte, opts *Options) ([]byte, error) {
	n, err := CompileBytes(src, opts)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(n, "", "  ")
}

func resolveSourceFiles(pattern, baseDir string) ([]string, error) {
	if isRemoteSource(pattern) {
		return nil, fmt.Errorf("remote source %q requires module lock/fetch integration", pattern)
	}
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(baseDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		if _, err := os.Stat(pattern); err != nil {
			return nil, err
		}
		matches = []string{pattern}
	}
	sort.Strings(matches)
	return matches, nil
}

func resolveModuleFiles(source, baseDir string) ([]string, error) {
	if !filepath.IsAbs(source) {
		source = filepath.Join(baseDir, source)
	}
	st, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{source}, nil
	}
	var matches []string
	for _, ext := range []string{"*.bcl", "*.schema"} {
		files, err := filepath.Glob(filepath.Join(source, ext))
		if err != nil {
			return nil, err
		}
		matches = append(matches, files...)
	}
	sort.Strings(matches)
	return matches, nil
}

func isRemoteSource(source string) bool {
	return strings.HasPrefix(source, "git::") || strings.HasSuffix(source, ".git") || strings.Contains(source, "://")
}

func blockString(b *Block, name string) string {
	for _, n := range b.Body {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if lit, ok := a.Value.(*Literal); ok {
			if s, ok := lit.Data.(string); ok {
				return s
			}
		}
	}
	return ""
}

func blockObject(b *Block, name string, c *compiler) map[string]any {
	for _, n := range b.Body {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if obj, ok := c.value(a.Value).(map[string]any); ok {
			return obj
		}
	}
	return nil
}

func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if v == nil {
			delete(dst, k)
			continue
		}
		if dm, ok := dst[k].(map[string]any); ok {
			if sm, ok := v.(map[string]any); ok {
				mergeMap(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func cloneAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = cloneAny(v)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = cloneAny(v)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(x))
		for i, v := range x {
			out[i], _ = cloneAny(v).(map[string]any)
		}
		return out
	default:
		return v
	}
}

func setNormalized(dst map[string]any, key string, value any) {
	if strings.Contains(key, ".") {
		parts := strings.Split(key, ".")
		cur := dst
		for _, part := range parts[:len(parts)-1] {
			next, _ := cur[part].(map[string]any)
			if next == nil {
				next = map[string]any{}
				cur[part] = next
			}
			cur = next
		}
		key = parts[len(parts)-1]
		dst = cur
	}
	if existing, ok := dst[key]; ok {
		dst[key] = appendBlock(existing, value)
		return
	}
	dst[key] = value
}
