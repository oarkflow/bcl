package bcl

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

func Validate(doc *Document, opts *Options) []Diagnostic {
	var diags []Diagnostic
	seen := map[string]Span{}
	blocks := map[string]*Block{}
	consts := map[string]bool{}
	constUses := map[string]int{}
	sets := map[string]bool{}
	setUses := map[string]int{}
	schemas := map[string]*SchemaDecl{}
	aliases := map[string]string{}
	predicates := map[string]*Block{}
	refs := map[string][]Span{}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ConstDecl:
				if consts[x.Name] {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate constant %q", x.Name), Span: x.Span})
				}
				consts[x.Name] = true
				validateValueAdvanced(x.Value, &diags, refs, constUses, setUses)
			case *SchemaDecl:
				if schemas[x.Name] != nil {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate schema %q", x.Name), Span: x.Span})
				}
				schemas[x.Name] = x
			case *ParamDecl:
				if x.Default != nil {
					validateValueAdvanced(x.Default, &diags, refs, constUses, setUses)
				}
			case *TypeDecl:
				aliases[x.Name] = x.Type
			case *Block:
				if x.ID != "" {
					key := x.Type + "." + x.ID
					if old, ok := seen[key]; ok {
						_ = old
						diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate block %s", key), Span: x.Span})
					}
					seen[key] = x.Span
					blocks[key] = x
				}
				if x.Type == "set" && x.ID != "" {
					if sets[x.ID] {
						diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate set %q", x.ID), Span: x.Span})
					}
					sets[x.ID] = true
				}
				if x.Type == "predicate" && x.ID != "" {
					if predicates[x.ID] != nil {
						diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate predicate %q", x.ID), Span: x.Span})
					}
					predicates[x.ID] = x
				}
				walk(x.Body)
			case *Assignment:
				validateValueAdvanced(x.Value, &diags, refs, constUses, setUses)
				if o, ok := x.Value.(*Object); ok {
					walk(o.Fields)
				}
			}
		}
	}
	walk(doc.Items)
	validateSchemas(doc.Items, schemas, aliases, &diags)
	validateReferences(blocks, refs, &diags)
	validateCycles(blocks, refs, &diags)
	validateWorkflowGraphs(doc.Items, &diags)
	validateEvaluationConflicts(doc.Items, &diags)
	validateIntegrations(doc.Items, &diags)
	validatePredicates(doc.Items, predicates, &diags)
	validateModuleParamContracts(doc.Items, opts, &diags)
	if opts != nil && opts.Strict || documentStrict(doc) {
		diags = append(diags, strictDiagnostics(doc, opts)...)
		validateUnknownSchemaFields(doc.Items, schemas, &diags)
	}
	return diags
}

func validatePredicates(nodes []Node, predicates map[string]*Block, diags *[]Diagnostic) {
	graph := map[string][]string{}
	var walk func([]Node, string)
	walk = func(nodes []Node, owner string) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *Assignment:
				collectPredicateRefs(x.Value, owner, predicates, graph, diags)
				if obj, ok := x.Value.(*Object); ok {
					walk(obj.Fields, owner)
				}
			case *Block:
				nextOwner := owner
				if x.Type == "predicate" {
					nextOwner = x.ID
				}
				walk(x.Body, nextOwner)
			}
		}
	}
	walk(nodes, "")
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(name string) bool {
		if visiting[name] {
			if p := predicates[name]; p != nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("cyclic predicate reference involving %q", name), Span: p.Span})
			}
			return true
		}
		if visited[name] {
			return false
		}
		visiting[name] = true
		for _, dep := range graph[name] {
			visit(dep)
		}
		visiting[name] = false
		visited[name] = true
		return false
	}
	for name := range predicates {
		visit(name)
	}
}

func collectPredicateRefs(v Value, owner string, predicates map[string]*Block, graph map[string][]string, diags *[]Diagnostic) {
	switch x := v.(type) {
	case *Expr:
		if name, ok := predicateRefName(x.Raw); ok {
			if predicates[name] == nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown predicate %q", name), Span: x.Span})
			}
			if owner != "" {
				graph[owner] = append(graph[owner], name)
			}
		}
	case *Condition:
		if x.Expr != nil {
			collectPredicateRefs(x.Expr, owner, predicates, graph, diags)
		}
		for _, child := range x.Children {
			collectPredicateRefs(child, owner, predicates, graph, diags)
		}
	case *Object:
		for _, n := range x.Fields {
			if a, ok := n.(*Assignment); ok {
				collectPredicateRefs(a.Value, owner, predicates, graph, diags)
			}
		}
	case *List:
		for _, item := range x.Items {
			collectPredicateRefs(item, owner, predicates, graph, diags)
		}
	case *Call:
		for _, arg := range x.Args {
			collectPredicateRefs(arg, owner, predicates, graph, diags)
		}
	}
}

func validateModuleParamContracts(nodes []Node, opts *Options, diags *[]Diagnostic) {
	if opts == nil || opts.BaseDir == "" {
		return
	}
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		if b.Type != "module" {
			validateModuleParamContracts(b.Body, opts, diags)
			continue
		}
		src := blockString(b, "source")
		if src == "" || isRemoteSource(src) {
			continue
		}
		files, err := resolveModuleFiles(src, opts.BaseDir)
		if err != nil {
			continue
		}
		inputs := blockInputValues(b)
		for _, path := range files {
			doc, err := ParsePath(path)
			if err != nil {
				continue
			}
			for _, p := range collectParamDecls(doc.Items) {
				if p.Required {
					if _, ok := inputs[p.Name]; !ok {
						*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q missing required input %q", b.ID, p.Name), Span: b.Span})
					}
				}
				if v, ok := inputs[p.Name]; ok && !astValueMatchesParamType(v, p.Type) {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("module %q input %q must be %s", b.ID, p.Name, p.Type), Span: b.Span})
				}
			}
		}
	}
}

func blockInputValues(b *Block) map[string]Value {
	out := map[string]Value{}
	for _, n := range b.Body {
		a, ok := n.(*Assignment)
		if !ok || a.Name != "inputs" {
			continue
		}
		if obj, ok := a.Value.(*Object); ok {
			for _, item := range obj.Fields {
				if ia, ok := item.(*Assignment); ok {
					out[ia.Name] = ia.Value
				}
			}
		}
	}
	return out
}

func astValueMatchesParamType(v Value, typ string) bool {
	switch typ {
	case "", "any":
		return true
	case "string":
		lit, ok := v.(*Literal)
		return ok && lit.Type == "string"
	case "int", "integer":
		lit, ok := v.(*Literal)
		return ok && lit.Type == "int"
	case "float", "number":
		lit, ok := v.(*Literal)
		return ok && (lit.Type == "int" || lit.Type == "float")
	case "bool", "boolean":
		lit, ok := v.(*Literal)
		return ok && lit.Type == "bool"
	case "list":
		_, ok := v.(*List)
		return ok
	case "map", "object":
		_, ok := v.(*Object)
		return ok
	default:
		return true
	}
}

func validateEvaluationConflicts(nodes []Node, diags *[]Diagnostic) {
	strategy := "deny_overrides"
	for _, n := range nodes {
		if b, ok := n.(*Block); ok && b.Type == "evaluation" {
			if s := blockString(b, "strategy"); s != "" {
				strategy = s
			}
		}
	}
	type policyKey struct {
		tenant   string
		priority string
	}
	seen := map[policyKey]string{}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			b, ok := n.(*Block)
			if !ok {
				continue
			}
			if b.Type == "policy" || b.Type == "rule" {
				body := blockAssignments(b)
				effect := literalString(body["effect"])
				tenant := literalString(body["tenant"])
				priority := literalScalar(body["priority"])
				key := policyKey{tenant: tenant, priority: priority}
				if prev, ok := seen[key]; ok && prev != "" && effect != "" && prev != effect {
					switch strategy {
					case "deny_overrides", "allow_overrides", "highest_priority":
						*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("conflicting %s/%s at priority %s for tenant %s", prev, effect, priority, tenant), Span: b.Span})
					}
				}
				if effect != "" {
					seen[key] = effect
				}
			}
			walk(b.Body)
		}
	}
	walk(nodes)
}

func blockAssignments(b *Block) map[string]Value {
	out := map[string]Value{}
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok {
			out[a.Name] = a.Value
		}
	}
	return out
}

func literalString(v Value) string {
	if lit, ok := v.(*Literal); ok {
		if s, ok := lit.Data.(string); ok {
			return s
		}
	}
	if ref, ok := v.(*Reference); ok {
		return ref.Path
	}
	return ""
}

func literalScalar(v Value) string {
	if v == nil {
		return ""
	}
	if lit, ok := v.(*Literal); ok {
		return fmt.Sprint(lit.Data)
	}
	return literalString(v)
}

func Lint(doc *Document, opts *Options) []Diagnostic {
	diags := Validate(doc, opts)
	var hasVersion bool
	for _, n := range doc.Items {
		if b, ok := n.(*Block); ok && b.Type == "bcl" {
			hasVersion = true
		}
	}
	if !hasVersion {
		diags = append(diags, Diagnostic{Severity: "warning", Message: "missing bcl version declaration", Span: doc.Span})
	}
	decls, uses := declarationUsage(doc)
	for name, sp := range decls.consts {
		if uses.consts[name] == 0 {
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("unused constant %q", name), Span: sp})
		}
	}
	for name, sp := range decls.sets {
		if uses.sets[name] == 0 {
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("unused set %q", name), Span: sp})
		}
	}
	return diags
}

func validateValue(v Value, diags *[]Diagnostic) {
	switch x := v.(type) {
	case *Expr:
		if x.Raw == "" {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: "empty expression", Span: x.Span})
		}
	case *List:
		for _, item := range x.Items {
			validateValue(item, diags)
		}
	case *Object:
		for _, item := range x.Fields {
			if a, ok := item.(*Assignment); ok {
				validateValue(a.Value, diags)
			}
		}
	case *Call:
		for _, arg := range x.Args {
			validateValue(arg, diags)
		}
	}
}

func validateValueAdvanced(v Value, diags *[]Diagnostic, refs map[string][]Span, constUses map[string]int, setUses map[string]int) {
	validateValue(v, diags)
	switch x := v.(type) {
	case *Reference:
		if x.Path != "" {
			refs[x.Path] = append(refs[x.Path], x.Span)
			constUses[x.Path]++
		}
	case *Expr:
		if _, err := CompileExpression(x.Raw); err != nil && !strings.Contains(err.Error(), "unexpected expression token") {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid expression: " + err.Error(), Span: x.Span})
		}
		validateMatchExpressionSyntax(x.Raw, x.Span, diags)
	case *List:
		for _, item := range x.Items {
			validateValueAdvanced(item, diags, refs, constUses, setUses)
		}
	case *Object:
		for _, item := range x.Fields {
			if a, ok := item.(*Assignment); ok {
				validateValueAdvanced(a.Value, diags, refs, constUses, setUses)
			}
		}
	case *Call:
		if x.Name == "set" && len(x.Args) == 1 {
			if lit, ok := x.Args[0].(*Literal); ok {
				if s, ok := lit.Data.(string); ok {
					setUses[s]++
				}
			}
		}
		if x.Name == "ref" && len(x.Args) == 1 {
			if ref, ok := x.Args[0].(*Reference); ok {
				refs[ref.Path] = append(refs[ref.Path], ref.Span)
			}
		}
		for _, arg := range x.Args {
			validateValueAdvanced(arg, diags, refs, constUses, setUses)
		}
	case *Condition:
		for _, child := range x.Children {
			validateValueAdvanced(child, diags, refs, constUses, setUses)
		}
		if x.Expr != nil {
			validateValueAdvanced(x.Expr, diags, refs, constUses, setUses)
		}
	}
}

func validateMatchExpressionSyntax(raw string, sp Span, diags *[]Diagnostic) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "match ") {
		open := strings.IndexByte(raw, '{')
		close := strings.LastIndexByte(raw, '}')
		if open < 0 || close < open {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match expression", Span: sp})
			return
		}
		for _, rawCase := range splitMatchCases(raw[open+1 : close]) {
			pat, guard, _, err := parseRawCase(rawCase)
			if err != nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match case: " + err.Error(), Span: sp})
				continue
			}
			validatePatternBindings(pat, sp, diags)
			if guard != "" {
				if _, err := CompileExpression(guard); err != nil {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match guard: " + err.Error(), Span: sp})
				}
			}
		}
		return
	}
	if strings.HasPrefix(raw, "match(") && strings.HasSuffix(raw, ")") {
		args := splitTopLevel(raw[len("match("):len(raw)-1], ',')
		for _, arg := range args[1:] {
			arg = strings.TrimSpace(arg)
			if !strings.HasPrefix(arg, "case(") || !strings.HasSuffix(arg, ")") {
				continue
			}
			parts := splitTopLevel(arg[len("case("):len(arg)-1], ',')
			if len(parts) < 2 {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match case: case requires pattern and result", Span: sp})
				continue
			}
			pat, guard := splitPatternGuard(parts[0])
			validatePatternBindings(pat, sp, diags)
			if guard != "" {
				if _, err := CompileExpression(guard); err != nil {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match guard: " + err.Error(), Span: sp})
				}
			}
		}
	}
}

func validatePatternBindings(pattern string, sp Span, diags *[]Diagnostic) {
	seen := map[string]bool{}
	for _, name := range patternBindingNames(pattern) {
		if seen[name] {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate pattern binding %q", name), Span: sp})
		}
		seen[name] = true
	}
}

func patternBindingNames(pattern string) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "_" || pattern == "ANY" || pattern == "NONE" {
		return nil
	}
	if strings.HasPrefix(pattern, "SOME(") && strings.HasSuffix(pattern, ")") {
		return patternBindingNames(pattern[len("SOME(") : len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "ALL(") && strings.HasSuffix(pattern, ")") {
		return patternBindingNames(pattern[len("ALL(") : len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "{") && strings.HasSuffix(pattern, "}") {
		var out []string
		for _, field := range splitTopLevel(pattern[1:len(pattern)-1], ',') {
			field = strings.TrimSpace(field)
			if field == "" || strings.HasPrefix(field, "...") {
				continue
			}
			parts := splitTopLevel(field, ':')
			if len(parts) >= 2 {
				out = append(out, patternBindingNames(strings.Join(parts[1:], ":"))...)
			}
		}
		return out
	}
	if strings.HasPrefix(pattern, "[") && strings.HasSuffix(pattern, "]") {
		var out []string
		for _, item := range splitTopLevel(pattern[1:len(pattern)-1], ',') {
			item = strings.TrimSpace(item)
			if item == "" || strings.HasPrefix(item, "...") {
				continue
			}
			out = append(out, patternBindingNames(item)...)
		}
		return out
	}
	if name, _, ok := splitTypedPattern(pattern); ok && name != "_" {
		return []string{name}
	}
	if isBindName(pattern) {
		return []string{pattern}
	}
	return nil
}

func validateSchemas(nodes []Node, schemas map[string]*SchemaDecl, aliases map[string]string, diags *[]Diagnostic) {
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		if schema := schemas[b.Type]; schema != nil {
			validateBlockAgainstSchema(b, schema, aliases, diags)
		}
		validateSchemas(b.Body, schemas, aliases, diags)
	}
}

func validateBlockAgainstSchema(b *Block, schema *SchemaDecl, aliases map[string]string, diags *[]Diagnostic) {
	fields := map[string]Value{}
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok {
			fields[a.Name] = a.Value
		}
	}
	for _, f := range schema.Fields {
		v, ok := fields[f.Name]
		if !ok {
			if f.Required && f.Default == nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("%s %q missing required field %q", b.Type, b.ID, f.Name), Span: b.Span})
			}
			continue
		}
		if !typeMatches(resolveAlias(f.Type, aliases), v) {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q should be %s, got %s", f.Name, f.Type, v.Kind()), Span: v.GetSpan()})
		}
		if len(f.Enum) > 0 {
			var found bool
			for _, ev := range f.Enum {
				if reflect.DeepEqual(ev.ToInterface(false), v.ToInterface(false)) {
					found = true
					break
				}
			}
			if !found {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q value is not in enum", f.Name), Span: v.GetSpan()})
			}
		}
		validateSchemaConstraints(f, v, diags)
		if len(f.Fields) > 0 {
			if obj, ok := v.(*Object); ok {
				validateObjectAgainstFields(obj, f.Fields, aliases, diags)
			}
		}
	}
}

func validateObjectAgainstFields(obj *Object, schemaFields []SchemaField, aliases map[string]string, diags *[]Diagnostic) {
	fields := map[string]Value{}
	for _, n := range obj.Fields {
		if a, ok := n.(*Assignment); ok {
			fields[a.Name] = a.Value
		}
	}
	for _, f := range schemaFields {
		v, ok := fields[f.Name]
		if !ok {
			if f.Required && f.Default == nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("nested field %q missing required field %q", obj.Kind(), f.Name), Span: obj.Span})
			}
			continue
		}
		if !typeMatches(resolveAlias(f.Type, aliases), v) {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q should be %s, got %s", f.Name, f.Type, v.Kind()), Span: v.GetSpan()})
		}
		validateSchemaConstraints(f, v, diags)
		if len(f.Fields) > 0 {
			if child, ok := v.(*Object); ok {
				validateObjectAgainstFields(child, f.Fields, aliases, diags)
			}
		}
	}
}

func validateSchemaConstraints(f SchemaField, v Value, diags *[]Diagnostic) {
	if f.Pattern != "" {
		re, err := regexp.Compile(f.Pattern)
		if err != nil {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("schema field %q has invalid pattern: %v", f.Name, err), Span: f.Span})
		} else if !re.MatchString(literalScalar(v)) {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q does not match pattern", f.Name), Span: v.GetSpan()})
		}
	}
	if f.Min != nil || f.Max != nil {
		got, ok := numericFloat(valueInterface(v))
		if !ok {
			return
		}
		if f.Min != nil {
			if min, ok := numericFloat(valueInterface(f.Min)); ok && got < min {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q is below minimum", f.Name), Span: v.GetSpan()})
			}
		}
		if f.Max != nil {
			if max, ok := numericFloat(valueInterface(f.Max)); ok && got > max {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("field %q is above maximum", f.Name), Span: v.GetSpan()})
			}
		}
	}
}

func valueInterface(v Value) any {
	if v == nil {
		return nil
	}
	return v.ToInterface(false)
}

func typeMatches(want string, v Value) bool {
	want = resolveBuiltinAlias(want)
	if strings.HasPrefix(want, "list") {
		return v.Kind() == "list"
	}
	if strings.HasPrefix(want, "map") {
		return v.Kind() == "object"
	}
	if strings.HasPrefix(want, "tuple") {
		l, ok := v.(*List)
		if !ok {
			return false
		}
		inner := genericInner(want)
		if inner == "" {
			return true
		}
		parts := splitTypeList(inner)
		if len(parts) != len(l.Items) {
			return false
		}
		for i, part := range parts {
			if !typeMatches(strings.TrimSpace(part), l.Items[i]) {
				return false
			}
		}
		return true
	}
	switch want {
	case "any", "expression", "enum":
		return true
	case "string":
		return v.Kind() == "string" || v.Kind() == "identifier" || v.Kind() == "reference"
	case "int", "float", "bool", "duration", "bytes", "date", "datetime", "regex", "cidr", "url", "email", "ip", "time", "null":
		if c, ok := v.(*Call); ok && c.Name == want {
			return true
		}
		return v.Kind() == want
	case "map", "object", "block":
		return v.Kind() == "object"
	default:
		return true
	}
}

func resolveAlias(s string, aliases map[string]string) string {
	if aliases == nil {
		return s
	}
	if v, ok := aliases[s]; ok {
		return v
	}
	return s
}

func resolveBuiltinAlias(s string) string {
	switch s {
	case "Action", "TenantID", "Permission":
		return "string"
	default:
		return s
	}
}

func genericInner(s string) string {
	start := strings.IndexByte(s, '<')
	end := strings.LastIndexByte(s, '>')
	if start < 0 || end <= start {
		return ""
	}
	return s[start+1 : end]
}

func splitTypeList(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

func validateReferences(blocks map[string]*Block, refs map[string][]Span, diags *[]Diagnostic) {
	for ref, spans := range refs {
		if ref == "" || strings.HasPrefix(ref, "config.") || strings.Contains(ref, ".") && blocks[ref] != nil {
			continue
		}
		if strings.Contains(ref, ".") && blocks[ref] == nil {
			for _, sp := range spans {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown reference %q", ref), Span: sp})
			}
		}
	}
}

func validateCycles(blocks map[string]*Block, refs map[string][]Span, diags *[]Diagnostic) {
	graph := map[string][]string{}
	for id, b := range blocks {
		localRefs := map[string][]Span{}
		var dummy []Diagnostic
		var walkVals func([]Node)
		walkVals = func(nodes []Node) {
			for _, n := range nodes {
				switch x := n.(type) {
				case *Assignment:
					validateValueAdvanced(x.Value, &dummy, localRefs, map[string]int{}, map[string]int{})
				case *Block:
					walkVals(x.Body)
				}
			}
		}
		walkVals(b.Body)
		for ref := range localRefs {
			if blocks[ref] != nil {
				graph[id] = append(graph[id], ref)
			}
		}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var dfs func(string) bool
	dfs = func(n string) bool {
		if visiting[n] {
			return true
		}
		if visited[n] {
			return false
		}
		visiting[n] = true
		for _, next := range graph[n] {
			if dfs(next) {
				return true
			}
		}
		visiting[n] = false
		visited[n] = true
		return false
	}
	for id, b := range blocks {
		if dfs(id) {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("cyclic reference involving %s", id), Span: b.Span})
			return
		}
	}
}

type usageSet struct {
	consts map[string]Span
	sets   map[string]Span
}

type usageCount struct {
	consts map[string]int
	sets   map[string]int
}

func declarationUsage(doc *Document) (usageSet, usageCount) {
	decls := usageSet{consts: map[string]Span{}, sets: map[string]Span{}}
	uses := usageCount{consts: map[string]int{}, sets: map[string]int{}}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ConstDecl:
				decls.consts[x.Name] = x.Span
			case *Block:
				if x.Type == "set" && x.ID != "" {
					decls.sets[x.ID] = x.Span
				}
				walk(x.Body)
			case *Assignment:
				validateValueAdvanced(x.Value, &[]Diagnostic{}, map[string][]Span{}, uses.consts, uses.sets)
			}
		}
	}
	walk(doc.Items)
	return decls, uses
}

func documentStrict(doc *Document) bool {
	for _, n := range doc.Items {
		b, ok := n.(*Block)
		if !ok || b.Type != "bcl" {
			continue
		}
		for _, item := range b.Body {
			a, ok := item.(*Assignment)
			if !ok || a.Name != "strict" {
				continue
			}
			if lit, ok := a.Value.(*Literal); ok {
				if v, ok := lit.Data.(bool); ok {
					return v
				}
			}
		}
	}
	return false
}

func strictDiagnostics(doc *Document, opts *Options) []Diagnostic {
	var diags []Diagnostic
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *Block:
				if x.Type == "module" && blockString(x, "source") == "" {
					diags = append(diags, Diagnostic{Severity: "error", Message: "module requires source in strict mode", Span: x.Span})
				}
				if hasBoolField(x, "deprecated", true) && blockString(x, "replaced_by") == "" {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("%s %q is deprecated without replaced_by", x.Type, x.ID), Span: x.Span})
				}
				walk(x.Body)
			}
		}
	}
	walk(doc.Items)
	return diags
}

func validateUnknownSchemaFields(nodes []Node, schemas map[string]*SchemaDecl, diags *[]Diagnostic) {
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		if schema := schemas[b.Type]; schema != nil {
			allowed := map[string]bool{}
			for _, f := range schema.Fields {
				allowed[f.Name] = true
			}
			for _, item := range b.Body {
				a, ok := item.(*Assignment)
				if ok && !allowed[a.Name] {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown field %q for schema %s", a.Name, schema.Name), Span: a.Span})
				}
			}
		}
		validateUnknownSchemaFields(b.Body, schemas, diags)
	}
}

func validateWorkflowGraphs(nodes []Node, diags *[]Diagnostic) {
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		if b.Type == "pipeline" {
			validatePipelineGraph(b, diags)
		}
		validateWorkflowGraphs(b.Body, diags)
	}
}

func validatePipelineGraph(p *Block, diags *[]Diagnostic) {
	steps := map[string]Span{}
	var connections []*Block
	entry := blockString(p, "entrypoint")
	for _, n := range p.Body {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		if b.Type == "step" {
			steps[b.ID] = b.Span
		}
		if b.Type == "connection" {
			connections = append(connections, b)
		}
	}
	if entry == "" {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("pipeline %q missing entrypoint", p.ID), Span: p.Span})
	} else if _, ok := steps[entry]; !ok {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("pipeline %q entrypoint %q does not exist", p.ID, entry), Span: p.Span})
	}
	graph := map[string][]string{}
	for _, c := range connections {
		from := refTargetID(blockRef(c, "from"))
		to := refTargetID(blockRef(c, "to"))
		if _, ok := steps[from]; !ok {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("connection %q references unknown from step %q", c.ID, from), Span: c.Span})
		}
		if _, ok := steps[to]; !ok {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("connection %q references unknown to step %q", c.ID, to), Span: c.Span})
		}
		if from != "" && to != "" {
			graph[from] = append(graph[from], to)
		}
	}
	if entry != "" {
		seen := map[string]bool{}
		var dfs func(string)
		dfs = func(id string) {
			if seen[id] {
				return
			}
			seen[id] = true
			for _, next := range graph[id] {
				dfs(next)
			}
		}
		dfs(entry)
		for id, sp := range steps {
			if !seen[id] {
				*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("workflow step %q is unreachable", id), Span: sp})
			}
		}
	}
}

func validateIntegrations(nodes []Node, diags *[]Diagnostic) {
	for _, n := range nodes {
		b, ok := n.(*Block)
		if !ok {
			continue
		}
		switch b.Type {
		case "http":
			if blockString(b, "base_url") == "" {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("http %q requires base_url", b.ID), Span: b.Span})
			}
			if isDeniedHost(blockString(b, "base_url")) {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("http %q uses denied host", b.ID), Span: b.Span})
			}
			validateHTTPBlock(b, diags)
			validateProxyRedirect(b, diags)
		case "connector", "source", "action":
			if blockString(b, "type") == "" {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("%s %q requires type", b.Type, b.ID), Span: b.Span})
			}
			validateRequestResponseBlocks(b, diags)
		case "file":
			if blockString(b, "path") == "" || blockString(b, "mode") == "" {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("file %q requires path and mode", b.ID), Span: b.Span})
			}
		case "command":
			if blockString(b, "exec") == "" {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("command %q requires structured exec", b.ID), Span: b.Span})
			}
		}
		validateIntegrations(b.Body, diags)
	}
}

func validateHTTPBlock(b *Block, diags *[]Diagnostic) {
	for _, n := range b.Body {
		name, fields, span, ok := namedFields(n)
		if !ok {
			continue
		}
		switch name {
		case "auth":
			validateAuthFields(b.ID, fields, span, diags)
		case "security":
			_ = fields
		}
	}
}

func validateRequestResponseBlocks(b *Block, diags *[]Diagnostic) {
	for _, n := range b.Body {
		name, fields, span, ok := namedFields(n)
		if !ok {
			continue
		}
		switch name {
		case "request":
			method := stringFromFields(fields, "method")
			if method != "" && !allowedHTTPMethod(method) {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unsupported HTTP method %q", method), Span: span})
			}
			for _, rn := range fields {
				body, ok := rn.(*Block)
				if ok && body.Type == "body" {
					switch body.ID {
					case "json", "form", "text", "raw", "":
					default:
						*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unsupported body type %q", body.ID), Span: body.Span})
					}
				}
			}
			validateHeaderSensitivity(fields, diags)
		case "response":
			if hasFieldInNodes(fields, "expect_status") && !statusValueValidNodes(fields, "expect_status") {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: "response expect_status must be int or list<int>", Span: span})
			}
		}
	}
}

func validateAuthFields(id string, fields []Node, span Span, diags *[]Diagnostic) {
	authType := stringFromFields(fields, "type")
	if authType == "" {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("http %q auth requires type", id), Span: span})
	}
	switch authType {
	case "none", "bearer", "basic", "api_key", "mtls", "oauth2", "":
	default:
		*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unsupported auth type %q", authType), Span: span})
	}
	if authType == "bearer" && !hasFieldInNodes(fields, "token") {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: "bearer auth requires token", Span: span})
	}
	if authType == "bearer" && hasFieldInNodes(fields, "token") && !fieldSensitive(fields, "token") {
		*diags = append(*diags, Diagnostic{Severity: "warning", Message: "bearer auth token should be sensitive", Span: span})
	}
	if authType == "basic" && (!hasFieldInNodes(fields, "username") || !hasFieldInNodes(fields, "password")) {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: "basic auth requires username and password", Span: span})
	}
	if authType == "basic" && hasFieldInNodes(fields, "password") && !fieldSensitive(fields, "password") {
		*diags = append(*diags, Diagnostic{Severity: "warning", Message: "basic auth password should be sensitive", Span: span})
	}
	if authType == "api_key" && (!hasFieldInNodes(fields, "location") || !hasFieldInNodes(fields, "name") || !hasFieldInNodes(fields, "value")) {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: "api_key auth requires location, name, and value", Span: span})
	}
	if authType == "api_key" && hasFieldInNodes(fields, "value") && !fieldSensitive(fields, "value") {
		*diags = append(*diags, Diagnostic{Severity: "warning", Message: "api_key auth value should be sensitive", Span: span})
	}
	if authType == "mtls" && (!hasFieldInNodes(fields, "cert_file") || !hasFieldInNodes(fields, "key_file")) {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: "mtls auth requires cert_file and key_file", Span: span})
	}
	if authType == "mtls" && hasFieldInNodes(fields, "key_file") && !fieldSensitive(fields, "key_file") {
		*diags = append(*diags, Diagnostic{Severity: "warning", Message: "mtls key_file should be sensitive", Span: span})
	}
}

func validateProxyRedirect(b *Block, diags *[]Diagnostic) {
	for _, n := range b.Body {
		name, fields, span, ok := namedFields(n)
		if !ok {
			continue
		}
		if name == "proxy" && !hasFieldInNodes(fields, "url") {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("http %q proxy requires url", b.ID), Span: span})
		}
		if name == "redirects" {
			mode := stringFromFields(fields, "mode")
			if mode != "" && mode != "disabled" && mode != "same_host" && mode != "allowlist" {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unsupported redirect mode %q", mode), Span: span})
			}
		}
	}
}

func validateHeaderSensitivity(nodes []Node, diags *[]Diagnostic) {
	for _, n := range nodes {
		name, fields, _, ok := namedFields(n)
		if !ok || name != "headers" {
			continue
		}
		for _, field := range fields {
			a, ok := field.(*Assignment)
			if !ok {
				continue
			}
			if strings.EqualFold(a.Name, "authorization") || strings.EqualFold(a.Name, "proxy-authorization") {
				if !isSensitiveValue(a.Value) {
					*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("header %q should be sensitive", a.Name), Span: a.Span})
				}
			}
		}
	}
}

func fieldSensitive(nodes []Node, name string) bool {
	for _, n := range nodes {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		return isSensitiveValue(a.Value)
	}
	return false
}

func namedFields(n Node) (string, []Node, Span, bool) {
	switch x := n.(type) {
	case *Block:
		return x.Type, x.Body, x.Span, true
	case *Assignment:
		if obj, ok := x.Value.(*Object); ok {
			return x.Name, obj.Fields, x.Span, true
		}
	}
	return "", nil, Span{}, false
}

func hasField(b *Block, name string) bool {
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok && a.Name == name {
			return true
		}
	}
	return false
}

func hasFieldInNodes(nodes []Node, name string) bool {
	for _, n := range nodes {
		if a, ok := n.(*Assignment); ok && a.Name == name {
			return true
		}
	}
	return false
}

func stringFromFields(nodes []Node, name string) string {
	for _, n := range nodes {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if lit, ok := a.Value.(*Literal); ok {
			if s, ok := lit.Data.(string); ok {
				return s
			}
		}
		if ref, ok := a.Value.(*Reference); ok {
			return ref.Path
		}
	}
	return ""
}

func hasDeniedLocalhost(b *Block) bool {
	for _, n := range b.Body {
		child, ok := n.(*Assignment)
		if !ok || child.Name != "deny_hosts" {
			continue
		}
		return true
	}
	return false
}

func allowedHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

func statusValueValid(b *Block, name string) bool {
	return statusValueValidNodes(b.Body, name)
}

func statusValueValidNodes(nodes []Node, name string) bool {
	for _, n := range nodes {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if a.Value.Kind() == "int" {
			return true
		}
		if l, ok := a.Value.(*List); ok {
			for _, item := range l.Items {
				if item.Kind() != "int" {
					return false
				}
			}
			return true
		}
		return false
	}
	return true
}

func blockRef(b *Block, name string) string {
	for _, n := range b.Body {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if r, ok := a.Value.(*Reference); ok {
			return r.Path
		}
		if lit, ok := a.Value.(*Literal); ok {
			if s, ok := lit.Data.(string); ok {
				return s
			}
		}
	}
	return ""
}

func refTargetID(ref string) string {
	if i := strings.LastIndexByte(ref, '.'); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func hasBoolField(b *Block, name string, want bool) bool {
	for _, n := range b.Body {
		a, ok := n.(*Assignment)
		if !ok || a.Name != name {
			continue
		}
		if lit, ok := a.Value.(*Literal); ok {
			if v, ok := lit.Data.(bool); ok {
				return v == want
			}
		}
	}
	return false
}

func isDeniedHost(u string) bool {
	return strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") || strings.Contains(u, "169.254.169.254")
}

func Explain(doc *Document, opts *Options) (*Normalized, []Diagnostic) {
	n, err := Compile(doc, opts)
	if err != nil {
		if e, ok := err.(ErrorList); ok {
			return n, e
		}
		return n, []Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	diags := Lint(doc, opts)
	n.Diagnostics = append(n.Diagnostics, diags...)
	return n, diags
}
