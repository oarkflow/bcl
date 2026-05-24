package bcl

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
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
	schemaNames := collectSchemaNames(doc.Items)
	aliases := map[string]string{}
	predicates := map[string]*Block{}
	refs := map[string][]Span{}
	var walk func([]Node, int, string)
	walk = func(nodes []Node, depth int, currentType string) {
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
					if old, ok := seen[key]; ok && shouldCheckGlobalBlockDuplicate(x.Type, depth, schemaNames) {
						_ = old
						diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("duplicate block %s", key), Span: x.Span})
					}
					if _, ok := blocks[key]; !ok {
						seen[key] = x.Span
						blocks[key] = x
					}
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
				walk(x.Body, depth+1, x.Type)
			case *Spread:
				target := x.Target
				if target != "" && !strings.Contains(target, ".") && currentType != "" {
					target = currentType + "." + target
				}
				if target != "" {
					refs[target] = append(refs[target], x.Span)
				}
				walk(x.Body, depth, currentType)
			case *Assignment:
				validateValueAdvanced(x.Value, &diags, refs, constUses, setUses)
				if o, ok := x.Value.(*Object); ok {
					walk(o.Fields, depth, currentType)
				}
			}
		}
	}
	walk(doc.Items, 0, "")
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

func collectSchemaNames(nodes []Node) map[string]bool {
	out := map[string]bool{}
	var walk func([]Node)
	walk = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *SchemaDecl:
				out[x.Name] = true
			case *Block:
				walk(x.Body)
			case *Assignment:
				if o, ok := x.Value.(*Object); ok {
					walk(o.Fields)
				}
			}
		}
	}
	walk(nodes)
	return out
}

func shouldCheckGlobalBlockDuplicate(blockType string, depth int, schemaNames map[string]bool) bool {
	if blockType == "override" {
		return false
	}
	if depth == 0 {
		return true
	}
	return !schemaNames[blockType]
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
	if !hasVersion && (opts == nil || !opts.Partial) {
		diags = append(diags, Diagnostic{Severity: "warning", Message: "missing bcl version declaration", Span: doc.Span})
	}
	if opts != nil && opts.Partial {
		return diags
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
		hasCatchAll := false
		catchAllSeen := false
		literals := map[string]bool{}
		for _, rawCase := range splitMatchCases(raw[open+1 : close]) {
			pat, guard, _, err := parseRawCase(rawCase)
			if err != nil {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match case: " + err.Error(), Span: sp})
				continue
			}
			validateMatchPatternCase(pat, guard, catchAllSeen, literals, sp, diags)
			if guard == "" && patternIsCatchAll(pat) {
				catchAllSeen = true
			}
			if guard == "" && patternIsCatchAll(pat) {
				hasCatchAll = true
			}
			if guard != "" {
				if _, err := CompileExpression(guard); err != nil {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match guard: " + err.Error(), Span: sp})
				}
			}
		}
		if !hasCatchAll {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: "match expression has no catch-all case", Span: sp})
		}
		return
	}
	if strings.HasPrefix(raw, "match(") && strings.HasSuffix(raw, ")") {
		args := splitTopLevel(raw[len("match("):len(raw)-1], ',')
		hasCatchAll := false
		catchAllSeen := false
		literals := map[string]bool{}
		for _, arg := range args[1:] {
			arg = strings.TrimSpace(arg)
			if !strings.HasPrefix(arg, "case(") || !strings.HasSuffix(arg, ")") {
				hasCatchAll = true
				continue
			}
			parts := splitTopLevel(arg[len("case("):len(arg)-1], ',')
			if len(parts) < 2 {
				*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match case: case requires pattern and result", Span: sp})
				continue
			}
			pat, guard := splitPatternGuard(parts[0])
			validateMatchPatternCase(pat, guard, catchAllSeen, literals, sp, diags)
			if guard == "" && patternIsCatchAll(pat) {
				catchAllSeen = true
			}
			if guard == "" && patternIsCatchAll(pat) {
				hasCatchAll = true
			}
			if guard != "" {
				if _, err := CompileExpression(guard); err != nil {
					*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match guard: " + err.Error(), Span: sp})
				}
			}
		}
		if !hasCatchAll {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: "match expression has no catch-all case", Span: sp})
		}
	}
}

func validateMatchPatternCase(pattern, guard string, catchAllSeen bool, literals map[string]bool, sp Span, diags *[]Diagnostic) {
	if patternHasInvalid(compilePatternNode(pattern)) {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: "invalid match pattern", Span: sp})
	}
	if catchAllSeen {
		*diags = append(*diags, Diagnostic{Severity: "warning", Message: "unreachable match case after catch-all", Span: sp})
	}
	if guard == "" {
		if lit, ok := parsePatternLiteral(strings.TrimSpace(pattern)); ok {
			key := fmt.Sprintf("%#v", lit)
			if literals[key] {
				*diags = append(*diags, Diagnostic{Severity: "warning", Message: "duplicate match literal case", Span: sp})
			}
			literals[key] = true
		}
	}
	validatePatternBindings(pattern, sp, diags)
	validatePatternAlternativeBindings(pattern, sp, diags)
}

func patternHasInvalid(node *patternNode) bool {
	if node == nil {
		return true
	}
	if node.Kind == patternInvalid {
		return true
	}
	if patternHasInvalid(node.Child) && node.Child != nil {
		return true
	}
	for _, child := range node.Children {
		if patternHasInvalid(child) {
			return true
		}
	}
	for _, field := range node.Fields {
		if patternHasInvalid(field.Node) {
			return true
		}
	}
	return false
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

func validatePatternAlternativeBindings(pattern string, sp Span, diags *[]Diagnostic) {
	alts := splitTopLevel(strings.TrimSpace(pattern), '|')
	if len(alts) < 2 {
		return
	}
	var base []string
	for i, alt := range alts {
		names := patternBindingNames(alt)
		sortStrings(names)
		if i == 0 {
			base = names
			continue
		}
		if !stringSlicesEqual(base, names) {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: "pattern alternatives bind different names", Span: sp})
			return
		}
	}
}

func sortStrings(xs []string) {
	sort.Strings(xs)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func patternBindingNames(pattern string) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "_" || pattern == "ANY" || pattern == "NONE" || pattern == "NULL" || pattern == "MISSING" {
		return nil
	}
	if alts := splitTopLevel(pattern, '|'); len(alts) > 1 {
		seen := map[string]bool{}
		var out []string
		for _, alt := range alts {
			for _, name := range patternBindingNames(alt) {
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
		return out
	}
	if strings.HasPrefix(pattern, "not ") {
		return nil
	}
	if parts := splitTopLevelWord(pattern, "as"); len(parts) == 2 {
		out := patternBindingNames(parts[0])
		name := strings.TrimSpace(parts[1])
		if isBindName(name) && name != "_" {
			out = append(out, name)
		}
		return out
	}
	if strings.HasPrefix(pattern, "SOME(") && strings.HasSuffix(pattern, ")") {
		return patternBindingNames(pattern[len("SOME(") : len(pattern)-1])
	}
	if (strings.HasPrefix(pattern, "ANY(") || strings.HasPrefix(pattern, "EXISTS(")) && strings.HasSuffix(pattern, ")") {
		open := strings.IndexByte(pattern, '(')
		return patternBindingNames(pattern[open+1 : len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "ALL(") && strings.HasSuffix(pattern, ")") {
		return patternBindingNames(pattern[len("ALL(") : len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "{") && strings.HasSuffix(pattern, "}") {
		var out []string
		for _, field := range splitTopLevel(pattern[1:len(pattern)-1], ',') {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if strings.HasPrefix(field, "...") {
				name := strings.TrimSpace(strings.TrimPrefix(field, "..."))
				if isBindName(name) && name != "_" {
					out = append(out, name)
				}
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
			if item == "" {
				continue
			}
			if strings.HasPrefix(item, "...") {
				name := strings.TrimSpace(strings.TrimPrefix(item, "..."))
				if isBindName(name) && name != "_" {
					out = append(out, name)
				}
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

func patternIsCatchAll(pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	return pattern == "_" || pattern == "ANY"
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
	for _, d := range ValidateSchemaValue(schema.Name, schemaDeclValidationMap(schema), blockValidationMap(b)) {
		*diags = append(*diags, d)
	}
}

func schemaDeclValidationMap(schema *SchemaDecl) map[string]any {
	fields := make([]map[string]any, 0, len(schema.Fields))
	for _, f := range schema.Fields {
		fields = append(fields, schemaFieldValidationMap(f))
	}
	m := map[string]any{"fields": fields}
	if len(schema.Options) > 0 {
		options := map[string]any{}
		for key, value := range schema.Options {
			options[key] = valueInterface(value)
		}
		m["options"] = options
	}
	return m
}

func schemaFieldValidationMap(f SchemaField) map[string]any {
	m := map[string]any{"name": f.Name, "type": f.Type, "required": f.Required}
	if f.Ref != "" {
		m["ref"] = f.Ref
	}
	if f.Const != nil {
		m["const"] = valueInterface(f.Const)
	}
	if len(f.Enum) > 0 {
		vals := make([]any, 0, len(f.Enum))
		for _, v := range f.Enum {
			vals = append(vals, valueInterface(v))
		}
		m["enum"] = vals
	}
	if len(f.Fields) > 0 {
		children := make([]map[string]any, 0, len(f.Fields))
		for _, child := range f.Fields {
			children = append(children, schemaFieldValidationMap(child))
		}
		m["fields"] = children
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
	for key, value := range map[string]Value{
		"min": f.Min, "max": f.Max, "exclusive_min": f.ExclusiveMin, "exclusive_max": f.ExclusiveMax,
		"multiple_of": f.MultipleOf, "min_len": f.MinLen, "max_len": f.MaxLen, "min_items": f.MinItems,
		"max_items": f.MaxItems, "min_props": f.MinProps, "max_props": f.MaxProps,
	} {
		if value != nil {
			m[key] = valueInterface(value)
		}
	}
	if f.Pattern != "" {
		m["pattern"] = f.Pattern
	}
	if f.Format != "" {
		m["format"] = f.Format
	}
	if f.PatternProperties != nil {
		m["pattern_properties"] = valueInterface(f.PatternProperties)
	}
	if f.DependentRequired != nil {
		m["dependent_required"] = valueInterface(f.DependentRequired)
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
	return m
}

func blockValidationMap(b *Block) map[string]any {
	out := map[string]any{}
	for _, n := range b.Body {
		if a, ok := n.(*Assignment); ok {
			out[a.Name] = validationValue(a.Value)
		}
	}
	return out
}

func validationValue(v Value) any {
	if ref, ok := v.(*Reference); ok {
		return ref.Path
	}
	if call, ok := v.(*Call); ok && schemaWrapperCall(call.Name) && len(call.Args) == 1 {
		return map[string]any{"$" + call.Name: validationValue(call.Args[0])}
	}
	if obj, ok := v.(*Object); ok {
		out := map[string]any{}
		for _, n := range obj.Fields {
			if a, ok := n.(*Assignment); ok {
				out[a.Name] = validationValue(a.Value)
			}
		}
		return out
	}
	return valueInterface(v)
}

func schemaWrapperCall(name string) bool {
	switch name {
	case "regex", "cidr", "duration", "ip", "url", "email", "bytes":
		return true
	default:
		return false
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
	blockTypes := map[string]bool{}
	for key := range blocks {
		if root, _, ok := strings.Cut(key, "."); ok {
			blockTypes[root] = true
		}
	}
	for ref, spans := range refs {
		root, _, dotted := strings.Cut(ref, ".")
		if ref == "" || !dotted || !blockTypes[root] || blocks[ref] != nil || hasBlockPathPrefix(blocks, ref) {
			continue
		}
		seen := map[string]bool{}
		for _, sp := range spans {
			key := fmt.Sprintf("%d:%d:%d:%d", sp.Start.Line, sp.Start.Column, sp.End.Line, sp.End.Column)
			if seen[key] {
				continue
			}
			seen[key] = true
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("unknown reference %q", ref), Span: sp})
		}
	}
}

func hasBlockPathPrefix(blocks map[string]*Block, ref string) bool {
	parts := strings.Split(ref, ".")
	if len(parts) < 3 {
		return false
	}
	for i := len(parts) - 1; i >= 2; i-- {
		if blocks[strings.Join(parts[:i], ".")] != nil {
			return true
		}
	}
	return false
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
	var collectDecls func([]Node)
	collectDecls = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ConstDecl:
				decls.consts[x.Name] = x.Span
			case *Block:
				if x.Type == "set" && x.ID != "" {
					decls.sets[x.ID] = x.Span
				}
				collectDecls(x.Body)
			case *Assignment:
				if o, ok := x.Value.(*Object); ok {
					collectDecls(o.Fields)
				}
			}
		}
	}
	var countUses func([]Node)
	countUses = func(nodes []Node) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *ConstDecl:
				countDeclarationValueUsage(x.Value, decls, uses)
			case *Block:
				countUses(x.Body)
			case *Assignment:
				countDeclarationValueUsage(x.Value, decls, uses)
			}
		}
	}
	collectDecls(doc.Items)
	countUses(doc.Items)
	return decls, uses
}

func countDeclarationValueUsage(v Value, decls usageSet, uses usageCount) {
	validateValueAdvanced(v, &[]Diagnostic{}, map[string][]Span{}, uses.consts, uses.sets)
	countExprConstUsage(v, decls.consts, uses.consts)
}

func countExprConstUsage(v Value, consts map[string]Span, uses map[string]int) {
	switch x := v.(type) {
	case *Expr:
		for _, name := range exprIdentifierNames(x.Raw) {
			if _, ok := consts[name]; ok {
				uses[name]++
			}
		}
	case *List:
		for _, item := range x.Items {
			countExprConstUsage(item, consts, uses)
		}
	case *Object:
		for _, item := range x.Fields {
			if a, ok := item.(*Assignment); ok {
				countExprConstUsage(a.Value, consts, uses)
			}
		}
	case *Call:
		for _, arg := range x.Args {
			countExprConstUsage(arg, consts, uses)
		}
	case *Condition:
		for _, child := range x.Children {
			countExprConstUsage(child, consts, uses)
		}
		if x.Expr != nil {
			countExprConstUsage(x.Expr, consts, uses)
		}
	}
}

func exprIdentifierNames(raw string) []string {
	toks, err := exprTokens(raw)
	if err != nil {
		return nil
	}
	var names []string
	for _, tok := range toks {
		if tok.kind == tokIdent && !isExprOperator(tok.text) && !isKeyword(tok.text) {
			names = append(names, tok.text)
		}
	}
	return names
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
				if prefix, ok := dottedSchemaTopLevel(f.Name); ok {
					allowed[prefix] = true
				}
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

func dottedSchemaTopLevel(name string) (string, bool) {
	head, _, ok := strings.Cut(name, ".")
	return head, ok && head != ""
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

func allowedHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
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
