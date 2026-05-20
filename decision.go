package bcl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type DecisionProgram struct {
	Modules     []string                       `json:"modules,omitempty"`
	Constants   map[string]any                 `json:"constants,omitempty"`
	Decisions   map[string]*DecisionDefinition `json:"decisions,omitempty"`
	Contracts   map[string]*DecisionContract   `json:"contracts,omitempty"`
	Rankings    map[string]*RankingDefinition  `json:"rankings,omitempty"`
	Datasets    map[string]*DatasetDefinition  `json:"datasets,omitempty"`
	Actions     map[string]map[string]any      `json:"actions,omitempty"`
	Governance  map[string]any                 `json:"governance,omitempty"`
	Tests       []DecisionTest                 `json:"tests,omitempty"`
	Diagnostics []Diagnostic                   `json:"diagnostics,omitempty"`
	Normalized  *Normalized                    `json:"normalized,omitempty"`
}

type DecisionDefinition struct {
	ID       string         `json:"id"`
	Module   string         `json:"module,omitempty"`
	Default  string         `json:"default,omitempty"`
	Strategy string         `json:"strategy,omitempty"`
	Rules    []DecisionRule `json:"rules,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Span     Span           `json:"span,omitempty"`
}

type DecisionContract struct {
	ID       string   `json:"id"`
	Effects  []string `json:"effects,omitempty"`
	Default  string   `json:"default,omitempty"`
	Strategy string   `json:"strategy,omitempty"`
	Span     Span     `json:"span,omitempty"`
}

type DecisionRule struct {
	ID        string         `json:"id"`
	Effect    string         `json:"effect,omitempty"`
	Priority  int64          `json:"priority,omitempty"`
	Condition map[string]any `json:"condition,omitempty"`
	Then      map[string]any `json:"then,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Source    string         `json:"source,omitempty"`
	Span      Span           `json:"span,omitempty"`
}

type RankingDefinition struct {
	ID           string         `json:"id"`
	Module       string         `json:"module,omitempty"`
	Dataset      string         `json:"dataset,omitempty"`
	Selection    string         `json:"selection,omitempty"`
	PriorityPath string         `json:"priority_path,omitempty"`
	CostPath     string         `json:"cost_path,omitempty"`
	Rules        []DecisionRule `json:"rules,omitempty"`
	Scores       []RankingScore `json:"scores,omitempty"`
	Span         Span           `json:"span,omitempty"`
}

type RankingScore struct {
	ID        string  `json:"id"`
	Metric    string  `json:"metric,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
	Normalize []any   `json:"normalize,omitempty"`
}

type DatasetDefinition struct {
	ID      string              `json:"id"`
	Module  string              `json:"module,omitempty"`
	Records []DecisionCandidate `json:"records,omitempty"`
}

type DecisionCandidate struct {
	ID    string         `json:"id"`
	Facts map[string]any `json:"facts,omitempty"`
}

type DecisionTest struct {
	Name     string         `json:"name"`
	Decision string         `json:"decision,omitempty"`
	Input    map[string]any `json:"input,omitempty"`
	Expect   map[string]any `json:"expect,omitempty"`
}

type DecisionResult struct {
	DecisionID  string           `json:"decision_id,omitempty"`
	Effect      string           `json:"effect"`
	Allowed     bool             `json:"allowed"`
	PolicyID    string           `json:"policy_id,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Score       float64          `json:"score,omitempty"`
	Actions     []DecisionAction `json:"actions,omitempty"`
	Events      []DecisionAction `json:"events,omitempty"`
	Rank        *DecisionRank    `json:"rank,omitempty"`
	Evaluated   int              `json:"evaluated"`
	Trace       []string         `json:"trace,omitempty"`
	Explain     []DecisionTrace  `json:"explain,omitempty"`
	Diagnostics []Diagnostic     `json:"diagnostics,omitempty"`
}

type DecisionTrace struct {
	RuleID    string `json:"rule_id,omitempty"`
	Source    string `json:"source,omitempty"`
	Status    string `json:"status"`
	Effect    string `json:"effect,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

type DecisionAction struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
}

type DecisionRank struct {
	ID    string         `json:"id"`
	Score float64        `json:"score"`
	Facts map[string]any `json:"facts,omitempty"`
}

type DecisionActionHandler func(action DecisionAction, result *DecisionResult) error
type DecisionRankingScorer func(candidate DecisionCandidate, ranking *RankingDefinition, input map[string]any) (float64, bool, error)

type DecisionScenario struct {
	Name     string         `json:"name,omitempty"`
	Decision string         `json:"decision"`
	Input    map[string]any `json:"input,omitempty"`
	Expect   map[string]any `json:"expect,omitempty"`
}

type DecisionScenarioResult struct {
	Name        string          `json:"name,omitempty"`
	Passed      bool            `json:"passed"`
	Decision    *DecisionResult `json:"decision,omitempty"`
	Expected    map[string]any  `json:"expected,omitempty"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

func CompileDecisionFile(path string, opts *Options) (*DecisionProgram, error) {
	doc, err := ParsePath(path)
	if err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &Options{}
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Dir(path)
	}
	opts.ResolveImports = true
	opts.ResolveModules = true
	return CompileDecisionDocument(doc, opts)
}

func CompileDecisionDir(dir string, opts *Options) (*DecisionProgram, error) {
	domain, err := CompileDomainDir(dir, opts)
	if domain != nil && domain.Decisions != nil {
		return domain.Decisions, err
	}
	if err != nil {
		return nil, err
	}
	return &DecisionProgram{Constants: map[string]any{}, Decisions: map[string]*DecisionDefinition{}, Contracts: map[string]*DecisionContract{}, Rankings: map[string]*RankingDefinition{}, Datasets: map[string]*DatasetDefinition{}, Actions: map[string]map[string]any{}}, nil
}

func CompileDecisionDocument(doc *Document, opts *Options) (*DecisionProgram, error) {
	if opts == nil {
		opts = &Options{}
	}
	normalized, err := Compile(doc, opts)
	prog := newDecisionBuilder(opts).build(doc, normalized)
	if normalized != nil {
		prog.Diagnostics = append(prog.Diagnostics, normalized.Diagnostics...)
	}
	prog.Diagnostics = append(prog.Diagnostics, validateDecisionProgram(prog)...)
	if err != nil {
		return prog, err
	}
	if len(prog.Diagnostics) > 0 {
		return prog, ErrorList(prog.Diagnostics)
	}
	return prog, nil
}

type decisionBuilder struct {
	opts       *Options
	consts     map[string]Value
	constVals  map[string]any
	compiler   *compiler
	moduleSeen map[string]bool
}

func newDecisionBuilder(opts *Options) *decisionBuilder {
	if opts == nil {
		opts = &Options{}
	}
	b := &decisionBuilder{opts: opts, consts: map[string]Value{}, constVals: map[string]any{}, moduleSeen: map[string]bool{}}
	b.compiler = &compiler{
		opts:        opts,
		out:         &Normalized{Body: map[string]any{}, Constants: map[string]any{}, Params: map[string]any{}, Predicates: map[string]any{}, Sets: map[string][]any{}, Types: map[string]string{}, Schemas: map[string]any{}, Namespaces: map[string]any{}},
		consts:      b.consts,
		sets:        map[string][]Value{},
		types:       map[string]string{},
		schemaDecls: map[string]*SchemaDecl{},
		blockIndex:  map[string]*Block{},
		spreadStack: map[string]bool{},
	}
	return b
}

func (b *decisionBuilder) build(doc *Document, n *Normalized) *DecisionProgram {
	prog := &DecisionProgram{
		Constants:  map[string]any{},
		Decisions:  map[string]*DecisionDefinition{},
		Contracts:  map[string]*DecisionContract{},
		Rankings:   map[string]*RankingDefinition{},
		Datasets:   map[string]*DatasetDefinition{},
		Actions:    map[string]map[string]any{},
		Governance: map[string]any{},
		Normalized: n,
	}
	b.collectConsts(doc.Items)
	if n != nil {
		for k, v := range n.Constants {
			prog.Constants[k] = v
			b.constVals[k] = v
		}
		for k, v := range n.Predicates {
			b.compiler.out.Predicates[k] = v
		}
	}
	for k, v := range b.constVals {
		prog.Constants[k] = v
	}
	b.walk(prog, doc.Items, "")
	b.applyContracts(prog)
	sort.Strings(prog.Modules)
	return prog
}

func (b *decisionBuilder) collectConsts(nodes []Node) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *ConstDecl:
			b.consts[x.Name] = x.Value
			b.constVals[x.Name] = b.compiler.value(x.Value)
		case *Block:
			b.collectConsts(x.Body)
		case *Assignment:
			if o, ok := x.Value.(*Object); ok {
				b.collectConsts(o.Fields)
			}
		}
	}
}

func (b *decisionBuilder) walk(prog *DecisionProgram, nodes []Node, module string) {
	for _, n := range nodes {
		block, ok := n.(*Block)
		if !ok {
			continue
		}
		switch block.Type {
		case "module":
			if block.ID != "" && !b.moduleSeen[block.ID] {
				prog.Modules = append(prog.Modules, block.ID)
				b.moduleSeen[block.ID] = true
			}
			b.walk(prog, block.Body, block.ID)
		case "policy":
			if d := b.decisionFromPolicy(block, module); d != nil {
				prog.Decisions[d.ID] = d
			}
		case "decision_schema":
			if c := b.contractFromBlock(block); c != nil {
				prog.Contracts[c.ID] = c
			}
		case "rule_set":
			b.mergeRuleSet(prog, block, module)
		case "ranking":
			if r := b.rankingFromBlock(block, module); r != nil {
				prog.Rankings[r.ID] = r
			}
		case "dataset":
			if d := b.datasetFromBlock(block, module); d != nil {
				prog.Datasets[d.ID] = d
			}
		case "action":
			if block.ID != "" {
				prog.Actions[block.ID] = b.nodesToBody(block.Body)
			}
		case "governance":
			mergeMap(prog.Governance, b.nodesToBody(block.Body))
		case "test":
			if t := b.testFromBlock(block); t.Name != "" {
				prog.Tests = append(prog.Tests, t)
			}
		default:
			b.walk(prog, block.Body, module)
		}
	}
}

func (b *decisionBuilder) decisionFromPolicy(block *Block, module string) *DecisionDefinition {
	if block.ID == "" {
		return nil
	}
	d := &DecisionDefinition{ID: block.ID, Module: module, Default: "deny", Strategy: "deny_overrides", Span: block.Span, Metadata: map[string]any{}}
	for i := 0; i < len(block.Body); i++ {
		a, ok := block.Body[i].(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "default":
			d.Default = scalarString(b.compiler.value(a.Value))
			if d.Default == "" {
				d.Default = "deny"
			}
		case "strategy":
			d.Strategy = scalarString(b.compiler.value(a.Value))
		case "effect":
			rule := DecisionRule{ID: block.ID, Effect: scalarString(b.compiler.value(a.Value)), Source: "policy", Span: a.Span}
			rule.Condition = conditionAfter(block.Body, i, b)
			d.Rules = append(d.Rules, rule)
		default:
			if !isInlineDecisionEffect(a, b) {
				continue
			}
			rule, next := b.inlineRuleFrom(block.Body, i, a, "policy")
			d.Rules = append(d.Rules, rule)
			i = next
		}
	}
	return d
}

func (b *decisionBuilder) contractFromBlock(block *Block) *DecisionContract {
	if block.ID == "" {
		return nil
	}
	c := &DecisionContract{ID: block.ID, Span: block.Span}
	for _, n := range block.Body {
		a, ok := n.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "effects":
			c.Effects = stringList(b.compiler.value(a.Value))
		case "default":
			c.Default = scalarString(b.compiler.value(a.Value))
		case "strategy":
			c.Strategy = scalarString(b.compiler.value(a.Value))
		}
	}
	return c
}

func (b *decisionBuilder) applyContracts(prog *DecisionProgram) {
	for id, contract := range prog.Contracts {
		d := prog.Decisions[id]
		if d == nil {
			d = &DecisionDefinition{ID: id, Default: "deny", Strategy: "deny_overrides"}
			prog.Decisions[id] = d
		}
		if contract.Default != "" {
			d.Default = contract.Default
		}
		if contract.Strategy != "" {
			d.Strategy = contract.Strategy
		}
	}
}

func (b *decisionBuilder) mergeRuleSet(prog *DecisionProgram, block *Block, module string) {
	if block.ID == "" {
		return
	}
	d := prog.Decisions[block.ID]
	if d == nil {
		d = &DecisionDefinition{ID: block.ID, Module: module, Default: "deny", Strategy: "deny_overrides", Metadata: map[string]any{}, Span: block.Span}
		prog.Decisions[d.ID] = d
	}
	for _, n := range block.Body {
		a, ok := n.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "strategy", "execution_mode":
			if strategy := scalarString(b.compiler.value(a.Value)); strategy != "" {
				d.Strategy = strategy
			}
		}
	}
	for _, n := range block.Body {
		child, ok := n.(*Block)
		if !ok || child.Type != "rule" {
			continue
		}
		r := DecisionRule{ID: child.ID, Source: "rule_set", Span: child.Span}
		for _, item := range child.Body {
			a, ok := item.(*Assignment)
			if !ok {
				continue
			}
			switch a.Name {
			case "priority":
				r.Priority = intValue(b.compiler.value(a.Value))
			case "when":
				r.Condition = b.conditionValue(a.Value)
			case "then":
				r.Then = b.valueObject(a.Value)
				r.Effect = scalarString(lookup(r.Then, "decision"))
			case "reason":
				r.Reason = scalarString(b.compiler.value(a.Value))
			}
		}
		d.Rules = append(d.Rules, r)
	}
	sortDecisionRules(d.Rules)
}

func (b *decisionBuilder) inlineRuleFrom(nodes []Node, i int, a *Assignment, source string) (DecisionRule, int) {
	rule := DecisionRule{Effect: a.Name, ID: scalarString(b.compiler.value(a.Value)), Source: source, Span: a.Span}
	if rule.ID == "" {
		rule.ID = rule.Effect
	}
	next := i
	for j := i + 1; j < len(nodes); j++ {
		aa, ok := nodes[j].(*Assignment)
		if !ok {
			break
		}
		if isInlineDecisionEffect(aa, b) {
			break
		}
		switch aa.Name {
		case "when":
			rule.Condition = b.conditionValue(aa.Value)
			next = j
		case "then":
			rule.Then = b.valueObject(aa.Value)
			next = j
		case "reason":
			rule.Reason = scalarString(b.compiler.value(aa.Value))
			next = j
		case "priority":
			rule.Priority = intValue(b.compiler.value(aa.Value))
			next = j
		default:
			return rule, next
		}
	}
	return rule, next
}

func (b *decisionBuilder) rankingFromBlock(block *Block, module string) *RankingDefinition {
	r := &RankingDefinition{ID: block.ID, Module: module, Selection: "best", Span: block.Span}
	for _, n := range block.Body {
		switch x := n.(type) {
		case *Assignment:
			switch x.Name {
			case "dataset":
				r.Dataset = scalarString(b.compiler.value(x.Value))
			case "selection":
				r.Selection = scalarString(b.compiler.value(x.Value))
			case "priority_path":
				r.PriorityPath = scalarString(b.compiler.value(x.Value))
			case "cost_path":
				r.CostPath = scalarString(b.compiler.value(x.Value))
			}
		case *Block:
			switch x.Type {
			case "rule":
				rule := DecisionRule{ID: x.ID, Source: "ranking", Span: x.Span}
				for _, item := range x.Body {
					if a, ok := item.(*Assignment); ok && a.Name == "when" {
						rule.Condition = b.conditionValue(a.Value)
					}
				}
				r.Rules = append(r.Rules, rule)
			case "score":
				score := RankingScore{ID: x.ID, Weight: 1}
				for _, item := range x.Body {
					a, ok := item.(*Assignment)
					if !ok {
						continue
					}
					switch a.Name {
					case "metric":
						score.Metric = scalarString(b.compiler.value(a.Value))
					case "weight":
						score.Weight, _ = numericFloat(b.compiler.value(a.Value))
					case "normalize":
						score.Normalize = append(score.Normalize, b.compiler.value(a.Value))
					}
				}
				r.Scores = append(r.Scores, score)
			}
		}
	}
	return r
}

func (b *decisionBuilder) datasetFromBlock(block *Block, module string) *DatasetDefinition {
	d := &DatasetDefinition{ID: block.ID, Module: module}
	for _, n := range block.Body {
		child, ok := n.(*Block)
		if !ok || child.Type != "record" {
			continue
		}
		body := b.nodesToBody(child.Body)
		d.Records = append(d.Records, DecisionCandidate{ID: child.ID, Facts: body})
	}
	return d
}

func (b *decisionBuilder) testFromBlock(block *Block) DecisionTest {
	t := DecisionTest{Name: block.ID}
	for _, n := range block.Body {
		a, ok := n.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "decision":
			t.Decision = scalarString(b.compiler.value(a.Value))
		case "input":
			t.Input = b.valueObject(a.Value)
		case "expect":
			t.Expect = b.valueObject(a.Value)
		}
	}
	return t
}

func (b *decisionBuilder) conditionValue(v Value) map[string]any {
	if cond, ok := v.(*Condition); ok {
		if m, ok := b.compiler.conditionToInterface(cond).(map[string]any); ok {
			return m
		}
	}
	if expr, ok := v.(*Expr); ok {
		return map[string]any{"op": "expr", "expr": expr.Raw}
	}
	return nil
}

func (b *decisionBuilder) valueObject(v Value) map[string]any {
	switch x := v.(type) {
	case *Object:
		return b.nodesToBody(x.Fields)
	default:
		if m, ok := b.compiler.value(v).(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func (b *decisionBuilder) nodesToBody(nodes []Node) map[string]any {
	return b.compiler.nodesToBody(nodes, "")
}

func isInlineDecisionEffect(a *Assignment, b *decisionBuilder) bool {
	if a == nil {
		return false
	}
	switch a.Name {
	case "default", "effect", "priority", "when", "then", "reason", "match", "tenant", "actions", "resources":
		return false
	}
	return scalarString(b.compiler.value(a.Value)) != ""
}

func conditionAfter(nodes []Node, i int, b *decisionBuilder) map[string]any {
	for j := i + 1; j < len(nodes); j++ {
		a, ok := nodes[j].(*Assignment)
		if !ok {
			continue
		}
		if a.Name == "when" {
			return b.conditionValue(a.Value)
		}
	}
	return nil
}

func sortDecisionRules(rules []DecisionRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority == rules[j].Priority {
			return rules[i].ID < rules[j].ID
		}
		return rules[i].Priority > rules[j].Priority
	})
}

func EvaluateDecision(program *DecisionProgram, decision string, input map[string]any, opts *Options) (*DecisionResult, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	def := program.Decisions[decision]
	if def == nil {
		return nil, fmt.Errorf("unknown decision %q", decision)
	}
	vars := decisionVars(program, input)
	result := &DecisionResult{DecisionID: decision, Effect: firstNonEmpty(def.Default, "deny")}
	strategy := firstNonEmpty(def.Strategy, "deny_overrides")
	var matchedPolicies []DecisionRule
	for _, rule := range def.Rules {
		ok := true
		if rule.Condition != nil {
			var err error
			ok, err = evalNormalizedCondition(rule.Condition, vars, opts)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: rule.Span})
				result.Trace = append(result.Trace, rule.ID+": condition error")
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "error", Message: err.Error()})
				continue
			}
		}
		result.Evaluated++
		if !ok {
			result.Trace = append(result.Trace, rule.ID+": condition false")
			result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "skipped", Message: "condition false"})
			continue
		}
		result.Trace = append(result.Trace, rule.ID+": matched")
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "matched", Effect: rule.Effect, Reason: rule.Reason})
		if rule.Source == "rule_set" {
			applyDecisionRule(result, rule, opts)
			continue
		}
		if rule.Effect != "" {
			matchedPolicies = append(matchedPolicies, rule)
		}
	}
	if selected, ok := chooseDecisionPolicy(strategy, matchedPolicies); ok {
		applyDecisionRule(result, selected, opts)
		result.Explain = append(result.Explain, DecisionTrace{RuleID: selected.ID, Source: selected.Source, Status: "selected", Effect: selected.Effect, Reason: selected.Reason})
	}
	if rank := evaluateDecisionRank(program, decision, vars, input, opts, result); rank != nil {
		result.Rank = rank
	}
	result.Allowed = result.Effect == "allow"
	return result, nil
}

func chooseDecisionPolicy(strategy string, rules []DecisionRule) (DecisionRule, bool) {
	if len(rules) == 0 {
		return DecisionRule{}, false
	}
	switch strategy {
	case "first_match", "highest_priority":
		return rules[0], true
	case "allow_overrides":
		for _, rule := range rules {
			if rule.Effect == "allow" {
				return rule, true
			}
		}
	case "all_must_pass":
		for _, rule := range rules {
			if rule.Effect == "deny" {
				return rule, true
			}
		}
		return rules[len(rules)-1], true
	case "deny_overrides":
		fallthrough
	default:
		for _, rule := range rules {
			if rule.Effect == "deny" {
				return rule, true
			}
		}
	}
	return rules[0], true
}

func decisionVars(program *DecisionProgram, input map[string]any) map[string]any {
	vars := map[string]any{}
	for k, v := range program.Constants {
		vars[k] = v
	}
	for k, v := range input {
		if k == "context" {
			if m, ok := v.(map[string]any); ok {
				for ck, cv := range m {
					vars[ck] = cv
				}
			}
		}
		vars[k] = v
	}
	return vars
}

func applyDecisionRule(result *DecisionResult, rule DecisionRule, opts *Options) {
	if rule.Effect != "" {
		result.Effect = rule.Effect
		result.PolicyID = rule.ID
		result.Reason = rule.Reason
	}
	if rule.Then != nil {
		if effect := scalarString(lookup(rule.Then, "decision")); effect != "" {
			result.Effect = effect
			result.PolicyID = rule.ID
		}
		applyThenExpressions(result, rule.Then)
		for _, action := range decisionActionsFrom(rule.Then, "action") {
			result.Actions = append(result.Actions, action)
			if opts != nil && opts.DecisionActions != nil {
				if h := opts.DecisionActions[action.Name]; h != nil {
					if err := h(action, result); err != nil {
						result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: rule.Span})
					}
				}
			}
		}
		result.Events = append(result.Events, decisionActionsFrom(rule.Then, "event")...)
	}
}

func applyThenExpressions(result *DecisionResult, then map[string]any) {
	raw := then["$expr"]
	items := asAnySlice(raw)
	if len(items) == 0 && raw != nil {
		items = []any{raw}
	}
	for _, item := range items {
		expr := scalarString(lookupExpr(item))
		parts := strings.Fields(expr)
		if len(parts) == 3 && parts[0] == "score" && parts[1] == "+=" {
			if n, ok := numericFloat(parseInlineNumber(parts[2])); ok {
				result.Score += n
			}
		}
	}
}

func decisionActionsFrom(then map[string]any, key string) []DecisionAction {
	var out []DecisionAction
	for _, item := range asAnySlice(then[key]) {
		m, ok := item.(map[string]any)
		if !ok {
			if name := scalarString(item); name != "" {
				out = append(out, DecisionAction{Name: name})
			}
			continue
		}
		name := scalarString(m["id"])
		body, _ := m["body"].(map[string]any)
		if name == "" {
			name = scalarString(m["name"])
		}
		if name != "" {
			out = append(out, DecisionAction{Name: name, Params: body})
		}
	}
	return out
}

func evaluateDecisionRank(program *DecisionProgram, decision string, vars, input map[string]any, opts *Options, result *DecisionResult) *DecisionRank {
	ranking := program.Rankings[decision]
	if ranking == nil {
		return nil
	}
	candidates := candidatesFor(program, ranking, input)
	var best *DecisionRank
	for _, candidate := range candidates {
		candidateVars := cloneStringAny(vars)
		candidateVars["provider"] = providerFacts(candidate)
		pass := true
		for _, rule := range ranking.Rules {
			if rule.Condition == nil {
				continue
			}
			ok, err := evalNormalizedCondition(rule.Condition, candidateVars, opts)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: rule.Span})
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "error", Candidate: candidate.ID, Message: err.Error()})
				pass = false
				break
			}
			if !ok {
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "skipped", Candidate: candidate.ID, Message: "ranking condition false"})
				pass = false
				break
			}
		}
		if !pass {
			continue
		}
		score, ok, err := scoreCandidate(candidate, ranking, input, opts)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: ranking.Span})
			continue
		}
		if !ok {
			continue
		}
		rank := &DecisionRank{ID: candidate.ID, Score: score, Facts: candidate.Facts}
		if best == nil || rank.Score > best.Score {
			best = rank
		}
	}
	if best != nil {
		result.Explain = append(result.Explain, DecisionTrace{Status: "ranked", Candidate: best.ID, Message: fmt.Sprintf("score %.4f", best.Score)})
	}
	return best
}

func candidatesFor(program *DecisionProgram, ranking *RankingDefinition, input map[string]any) []DecisionCandidate {
	var out []DecisionCandidate
	for _, item := range asAnySlice(input["candidates"]) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := scalarString(m["id"])
		facts, _ := m["facts"].(map[string]any)
		if facts == nil {
			facts = m
		}
		out = append(out, DecisionCandidate{ID: id, Facts: facts})
	}
	if len(out) > 0 {
		return out
	}
	if ranking.Dataset != "" {
		if d := program.Datasets[ranking.Dataset]; d != nil {
			return d.Records
		}
	}
	return nil
}

func scoreCandidate(candidate DecisionCandidate, ranking *RankingDefinition, input map[string]any, opts *Options) (float64, bool, error) {
	if opts != nil && opts.DecisionRankers != nil {
		if scorer := opts.DecisionRankers[ranking.ID]; scorer != nil {
			return scorer(candidate, ranking, input)
		}
	}
	score := 0.0
	provider := providerFacts(candidate)
	if ranking.PriorityPath != "" {
		if n, ok := numericFloat(lookup(map[string]any{"provider": provider}, ranking.PriorityPath)); ok {
			score += n
		}
	}
	if ranking.CostPath != "" {
		if n, ok := numericFloat(lookup(map[string]any{"provider": provider}, ranking.CostPath)); ok {
			score -= n
		}
	}
	for _, metric := range ranking.Scores {
		if n, ok := numericFloat(lookup(map[string]any{"provider": provider}, metric.Metric)); ok {
			score += n * metric.Weight
		}
	}
	return score, true, nil
}

func providerFacts(candidate DecisionCandidate) map[string]any {
	if provider, ok := candidate.Facts["provider"].(map[string]any); ok {
		return provider
	}
	return candidate.Facts
}

func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if ref, ok := x["$ref"].(string); ok {
			return ref
		}
		if expr, ok := x["$expr"].(string); ok {
			return expr
		}
	}
	return ""
}

func asAnySlice(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case []map[string]any:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, item)
		}
		return out
	default:
		return []any{x}
	}
}

func lookupExpr(v any) any {
	if m, ok := v.(map[string]any); ok {
		if expr, ok := m["$expr"]; ok {
			return expr
		}
	}
	return v
}

func parseInlineNumber(s string) any {
	if strings.Contains(s, ".") {
		var f float64
		if _, err := fmt.Sscan(s, &f); err == nil {
			return f
		}
	}
	var i int64
	if _, err := fmt.Sscan(s, &i); err == nil {
		return i
	}
	return s
}

func cloneStringAny(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func validateDecisionProgram(prog *DecisionProgram) []Diagnostic {
	if prog == nil {
		return nil
	}
	var diags []Diagnostic
	for id, contract := range prog.Contracts {
		allowed := map[string]bool{}
		for _, effect := range contract.Effects {
			allowed[effect] = true
		}
		if len(allowed) == 0 {
			continue
		}
		if contract.Default != "" && !allowed[contract.Default] {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q default effect %q is not declared", id, contract.Default), Span: contract.Span})
		}
		if d := prog.Decisions[id]; d != nil {
			if d.Default != "" && !allowed[d.Default] {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q default effect %q is not declared", id, d.Default), Span: d.Span})
			}
			for _, rule := range d.Rules {
				if rule.Effect != "" && !allowed[rule.Effect] {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q rule %q uses undeclared effect %q", id, rule.ID, rule.Effect), Span: rule.Span})
				}
			}
		}
	}
	return diags
}

func ReadDecisionScenarioFile(path string) (*DecisionScenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	scenario := &DecisionScenario{}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		m, err := parseSimpleYAMLMap(string(b))
		if err != nil {
			return nil, err
		}
		scenarioFromMap(scenario, m)
	default:
		if err := json.Unmarshal(b, scenario); err != nil {
			return nil, err
		}
	}
	if scenario.Input == nil {
		scenario.Input = map[string]any{}
	}
	if scenario.Expect == nil {
		scenario.Expect = map[string]any{}
	}
	return scenario, nil
}

func EvaluateDecisionScenario(program *DecisionProgram, scenario *DecisionScenario, opts *Options) (*DecisionScenarioResult, error) {
	if scenario == nil {
		return nil, fmt.Errorf("nil decision scenario")
	}
	result, err := EvaluateDecision(program, scenario.Decision, scenario.Input, opts)
	out := &DecisionScenarioResult{Name: scenario.Name, Passed: true, Decision: result, Expected: scenario.Expect}
	if err != nil {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
		return out, err
	}
	if want := scalarString(scenario.Expect["effect"]); want != "" && result.Effect != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected effect %q", want)})
	}
	if want, ok := scenario.Expect["allowed"].(bool); ok && result.Allowed != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected allowed %v", want)})
	}
	if want := scalarString(scenario.Expect["rank.id"]); want != "" && (result.Rank == nil || result.Rank.ID != want) {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected rank.id %q", want)})
	}
	out.Diagnostics = append(out.Diagnostics, result.Diagnostics...)
	if len(result.Diagnostics) > 0 {
		out.Passed = false
	}
	return out, nil
}

func scenarioFromMap(s *DecisionScenario, m map[string]any) {
	s.Name = scalarString(m["name"])
	s.Decision = scalarString(m["decision"])
	if input, ok := m["input"].(map[string]any); ok {
		s.Input = input
	}
	if expect, ok := m["expect"].(map[string]any); ok {
		s.Expect = expect
	}
}

func parseSimpleYAMLMap(src string) (map[string]any, error) {
	root := map[string]any{}
	stack := []yamlFrame{{indent: -1, value: root}}
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := countLeadingSpaces(line)
		text := strings.TrimSpace(line)
		for len(stack) > 1 && indent <= stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		cur, ok := stack[len(stack)-1].value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unsupported yaml nesting near %q", text)
		}
		key, val, ok := strings.Cut(text, ":")
		if !ok {
			return nil, fmt.Errorf("expected yaml key/value near %q", text)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if val == "" {
			child := map[string]any{}
			cur[key] = child
			stack = append(stack, yamlFrame{indent: indent, value: child})
			continue
		}
		cur[key] = parseYAMLScalar(val)
	}
	return root, nil
}

type yamlFrame struct {
	indent int
	value  any
}

func countLeadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

func parseYAMLScalar(s string) any {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'' {
			return s[1 : len(s)-1]
		}
	}
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}
		}
		var out []any
		for _, part := range splitTopLevel(inner, ',') {
			out = append(out, parseYAMLScalar(part))
		}
		return out
	}
	if strings.ContainsAny(s, ".eE") {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	return s
}
