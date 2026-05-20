package bcl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Schemas     map[string]any                 `json:"schemas,omitempty"`
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
	Order     int            `json:"-"`
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
	RuleID          string   `json:"rule_id,omitempty"`
	Source          string   `json:"source,omitempty"`
	Status          string   `json:"status"`
	Effect          string   `json:"effect,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Message         string   `json:"message,omitempty"`
	Candidate       string   `json:"candidate,omitempty"`
	Priority        int64    `json:"priority,omitempty"`
	ConditionResult *bool    `json:"condition_result,omitempty"`
	ScoreDelta      float64  `json:"score_delta,omitempty"`
	Action          string   `json:"action,omitempty"`
	Event           string   `json:"event,omitempty"`
	CandidateScore  *float64 `json:"candidate_score,omitempty"`
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
type DecisionInputValidator func(decisionID string, input map[string]any) []Diagnostic

type DecisionEvaluateOptions struct {
	Explain       bool
	ValidateInput bool
	Strict        bool
}

type DecisionEngine struct {
	Program         *DecisionProgram
	Options         *Options
	EvaluateOptions DecisionEvaluateOptions
}

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

func NewDecisionEngine(program *DecisionProgram, opts *Options) *DecisionEngine {
	return &DecisionEngine{
		Program: program,
		Options: opts,
		EvaluateOptions: DecisionEvaluateOptions{
			Explain:       true,
			ValidateInput: true,
		},
	}
}

func (e *DecisionEngine) Evaluate(decisionID string, input map[string]any) (*DecisionResult, error) {
	if e == nil {
		return nil, fmt.Errorf("nil decision engine")
	}
	return e.EvaluateWithOptions(decisionID, input, e.EvaluateOptions)
}

func (e *DecisionEngine) EvaluateWithOptions(decisionID string, input map[string]any, evalOpts DecisionEvaluateOptions) (*DecisionResult, error) {
	if e == nil {
		return nil, fmt.Errorf("nil decision engine")
	}
	return evaluateDecisionInternal(e.Program, decisionID, input, e.Options, evalOpts)
}

func (e *DecisionEngine) EvaluateScenario(scenario *DecisionScenario) (*DecisionScenarioResult, error) {
	if e == nil {
		return nil, fmt.Errorf("nil decision engine")
	}
	return evaluateDecisionScenarioInternal(e.Program, scenario, e.Options, e.EvaluateOptions)
}

func ExplainDecision(program *DecisionProgram, decision string, input map[string]any, opts *Options) (*DecisionResult, error) {
	return evaluateDecisionInternal(program, decision, input, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true})
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
	return &DecisionProgram{Constants: map[string]any{}, Decisions: map[string]*DecisionDefinition{}, Contracts: map[string]*DecisionContract{}, Rankings: map[string]*RankingDefinition{}, Datasets: map[string]*DatasetDefinition{}, Actions: map[string]map[string]any{}, Schemas: map[string]any{}}, nil
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
	order      int
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
		Schemas:    map[string]any{},
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
		for k, v := range n.Schemas {
			prog.Schemas[k] = v
		}
	}
	for k, v := range b.constVals {
		prog.Constants[k] = v
	}
	b.walk(prog, doc.Items, "")
	b.collectSchemas(prog, doc.Items)
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

func (b *decisionBuilder) collectSchemas(prog *DecisionProgram, nodes []Node) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *SchemaDecl:
			if x.Name != "" {
				prog.Schemas[x.Name] = schemaToMap(x, b.compiler)
			}
		case *Block:
			if x.Type == "schema" && x.ID != "" {
				prog.Schemas[x.ID] = b.schemaBlockToMap(x)
			}
			b.collectSchemas(prog, x.Body)
		case *Assignment:
			if o, ok := x.Value.(*Object); ok {
				b.collectSchemas(prog, o.Fields)
			}
		}
	}
}

func (b *decisionBuilder) schemaBlockToMap(block *Block) map[string]any {
	fieldsByName := map[string]map[string]any{}
	for _, n := range block.Body {
		a, ok := n.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "required", "optional":
			path := scalarString(b.compiler.value(a.Value))
			if path == "" {
				continue
			}
			field := schemaBlockField(fieldsByName, path)
			field["required"] = a.Name == "required"
			if field["type"] == nil {
				field["type"] = "any"
			}
		case "type":
			parts := strings.Fields(scalarString(b.compiler.value(a.Value)))
			if len(parts) < 2 {
				continue
			}
			field := schemaBlockField(fieldsByName, parts[0])
			field["type"] = parts[1]
		}
	}
	fields := make([]map[string]any, 0, len(fieldsByName))
	for _, field := range fieldsByName {
		fields = append(fields, field)
	}
	sort.Slice(fields, func(i, j int) bool {
		return scalarString(fields[i]["name"]) < scalarString(fields[j]["name"])
	})
	return map[string]any{"fields": fields}
}

func schemaBlockField(fields map[string]map[string]any, path string) map[string]any {
	if field := fields[path]; field != nil {
		return field
	}
	field := map[string]any{"name": path, "required": false}
	fields[path] = field
	return field
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
			rule := DecisionRule{ID: block.ID, Effect: scalarString(b.compiler.value(a.Value)), Source: "policy", Order: b.nextOrder(), Span: a.Span}
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
		r := DecisionRule{ID: child.ID, Source: "rule_set", Order: b.nextOrder(), Span: child.Span}
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
	rule := DecisionRule{Effect: a.Name, ID: scalarString(b.compiler.value(a.Value)), Source: source, Order: b.nextOrder(), Span: a.Span}
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
				rule := DecisionRule{ID: x.ID, Source: "ranking", Order: b.nextOrder(), Span: x.Span}
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

func (b *decisionBuilder) nextOrder() int {
	b.order++
	return b.order
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
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		if rules[i].Order != rules[j].Order {
			return rules[i].Order < rules[j].Order
		}
		return rules[i].ID < rules[j].ID
	})
}

func EvaluateDecision(program *DecisionProgram, decision string, input map[string]any, opts *Options) (*DecisionResult, error) {
	return evaluateDecisionInternal(program, decision, input, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true})
}

func evaluateDecisionInternal(program *DecisionProgram, decision string, input map[string]any, opts *Options, evalOpts DecisionEvaluateOptions) (*DecisionResult, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	def := program.Decisions[decision]
	if def == nil {
		return nil, fmt.Errorf("unknown decision %q", decision)
	}
	vars := decisionVars(program, input)
	result := &DecisionResult{DecisionID: decision, Effect: firstNonEmpty(def.Default, "deny")}
	if evalOpts.ValidateInput {
		result.Diagnostics = append(result.Diagnostics, validateDecisionInput(program, decision, input, opts)...)
		if len(result.Diagnostics) > 0 && evalOpts.Strict {
			result.Allowed = result.Effect == "allow"
			return result, nil
		}
	}
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
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "error", Priority: rule.Priority, Message: err.Error()})
				continue
			}
		}
		result.Evaluated++
		if !ok {
			result.Trace = append(result.Trace, rule.ID+": condition false")
			result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "skipped", Priority: rule.Priority, ConditionResult: boolPtr(false), Message: "condition false"})
			continue
		}
		result.Trace = append(result.Trace, rule.ID+": matched")
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "matched", Effect: rule.Effect, Reason: rule.Reason, Priority: rule.Priority, ConditionResult: boolPtr(true)})
		if rule.Source == "rule_set" {
			appendApplyTrace(result, rule, applyDecisionRule(result, rule, opts))
			continue
		}
		if rule.Effect != "" {
			matchedPolicies = append(matchedPolicies, rule)
		}
	}
	for _, selected := range chooseDecisionPolicies(strategy, matchedPolicies) {
		appendApplyTrace(result, selected, applyDecisionRule(result, selected, opts))
		result.Explain = append(result.Explain, DecisionTrace{RuleID: selected.ID, Source: selected.Source, Status: "selected", Effect: selected.Effect, Reason: selected.Reason, Priority: selected.Priority})
	}
	if rank := evaluateDecisionRank(program, decision, vars, input, opts, result); rank != nil {
		result.Rank = rank
	}
	result.Allowed = result.Effect == "allow"
	if !evalOpts.Explain {
		result.Explain = nil
	}
	return result, nil
}

func chooseDecisionPolicy(strategy string, rules []DecisionRule) (DecisionRule, bool) {
	selected := chooseDecisionPolicies(strategy, rules)
	if len(selected) == 0 {
		return DecisionRule{}, false
	}
	return selected[0], true
}

func chooseDecisionPolicies(strategy string, rules []DecisionRule) []DecisionRule {
	if len(rules) == 0 {
		return nil
	}
	switch strategy {
	case "first_match", "highest_priority":
		return []DecisionRule{rules[0]}
	case "collect_all":
		return append([]DecisionRule(nil), rules...)
	case "allow_overrides":
		for _, rule := range rules {
			if rule.Effect == "allow" {
				return []DecisionRule{rule}
			}
		}
	case "all_must_pass":
		for _, rule := range rules {
			if rule.Effect == "deny" {
				return []DecisionRule{rule}
			}
		}
		return []DecisionRule{rules[len(rules)-1]}
	case "deny_overrides":
		fallthrough
	default:
		for _, rule := range rules {
			if rule.Effect == "deny" {
				return []DecisionRule{rule}
			}
		}
	}
	return []DecisionRule{rules[0]}
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

type decisionApplySummary struct {
	ScoreDelta float64
	Actions    []DecisionAction
	Events     []DecisionAction
}

func applyDecisionRule(result *DecisionResult, rule DecisionRule, opts *Options) decisionApplySummary {
	beforeScore := result.Score
	beforeActions := len(result.Actions)
	beforeEvents := len(result.Events)
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
	return decisionApplySummary{
		ScoreDelta: result.Score - beforeScore,
		Actions:    append([]DecisionAction(nil), result.Actions[beforeActions:]...),
		Events:     append([]DecisionAction(nil), result.Events[beforeEvents:]...),
	}
}

func appendApplyTrace(result *DecisionResult, rule DecisionRule, summary decisionApplySummary) {
	if summary.ScoreDelta != 0 {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "score", Priority: rule.Priority, ScoreDelta: summary.ScoreDelta})
	}
	for _, action := range summary.Actions {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "action", Priority: rule.Priority, Action: action.Name})
	}
	for _, event := range summary.Events {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "event", Priority: rule.Priority, Event: event.Name})
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
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "error", Candidate: candidate.ID, Priority: rule.Priority, Message: err.Error()})
				pass = false
				break
			}
			if !ok {
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "skipped", Candidate: candidate.ID, Priority: rule.Priority, ConditionResult: boolPtr(false), Message: "ranking condition false"})
				pass = false
				break
			}
			result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "matched", Candidate: candidate.ID, Priority: rule.Priority, ConditionResult: boolPtr(true)})
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
		result.Explain = append(result.Explain, DecisionTrace{Status: "candidate_score", Candidate: candidate.ID, CandidateScore: floatPtr(score)})
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

func validateDecisionInput(program *DecisionProgram, decisionID string, input map[string]any, opts *Options) []Diagnostic {
	if input == nil {
		input = map[string]any{}
	}
	var diags []Diagnostic
	if opts != nil && opts.DecisionInputValidator != nil {
		diags = append(diags, opts.DecisionInputValidator(decisionID, input)...)
	}
	if program == nil || program.Schemas == nil {
		return diags
	}
	schema := program.Schemas[decisionID]
	if schema == nil {
		return diags
	}
	return append(diags, validateDecisionInputSchema(decisionID, schema, input)...)
}

func validateDecisionInputSchema(decisionID string, schema any, input map[string]any) []Diagnostic {
	m, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	var diags []Diagnostic
	for _, field := range schemaFieldsFromAny(m["fields"]) {
		validateDecisionInputField(decisionID, field, input, fieldName(field), &diags)
	}
	return diags
}

func validateDecisionInputField(decisionID string, field map[string]any, input map[string]any, path string, diags *[]Diagnostic) {
	name := fieldName(field)
	if name == "" {
		return
	}
	v := lookup(input, path)
	required, _ := field["required"].(bool)
	if v == nil {
		if required {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input missing required field %q", decisionID, path)})
		}
		return
	}
	if typ := scalarString(field["type"]); typ != "" && !runtimeTypeMatches(typ, v) {
		*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input field %q should be %s", decisionID, path, typ)})
		return
	}
	if enum := asAnySlice(field["enum"]); len(enum) > 0 {
		found := false
		for _, item := range enum {
			if equalLoose(v, item) {
				found = true
				break
			}
		}
		if !found {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input field %q is not in enum", decisionID, path)})
		}
	}
	if min, ok := numericFloat(field["min"]); ok {
		if got, ok := numericFloat(v); ok && got < min {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input field %q is below minimum", decisionID, path)})
		}
	}
	if max, ok := numericFloat(field["max"]); ok {
		if got, ok := numericFloat(v); ok && got > max {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input field %q is above maximum", decisionID, path)})
		}
	}
	if pattern := scalarString(field["pattern"]); pattern != "" {
		re, err := regexp.Compile(pattern)
		if err != nil {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q schema field %q has invalid pattern: %v", decisionID, path, err)})
		} else if !re.MatchString(fmt.Sprint(v)) {
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q input field %q does not match pattern", decisionID, path)})
		}
	}
	for _, child := range schemaFieldsFromAny(field["fields"]) {
		childPath := path + "." + fieldName(child)
		validateDecisionInputField(decisionID, child, input, childPath, diags)
	}
}

func schemaFieldsFromAny(v any) []map[string]any {
	switch xs := v.(type) {
	case []map[string]any:
		return xs
	case []any:
		out := make([]map[string]any, 0, len(xs))
		for _, item := range xs {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func fieldName(field map[string]any) string {
	return scalarString(field["name"])
}

func runtimeTypeMatches(want string, v any) bool {
	want = resolveBuiltinAlias(want)
	switch want {
	case "any", "expression", "enum":
		return true
	case "number":
		_, ok := numericFloat(v)
		return ok
	case "int":
		_, ok := intScalarValue(v)
		return ok
	case "float":
		_, ok := numericFloat(v)
		return ok
	case "string", "identifier", "url", "email", "date", "datetime", "regex", "cidr", "ip", "time", "duration", "bytes":
		_, ok := v.(string)
		if ok {
			return true
		}
		_, ok = v.(map[string]any)
		return ok && (want == "date" || want == "datetime" || want == "duration" || want == "bytes" || want == "regex" || want == "cidr")
	case "bool":
		_, ok := v.(bool)
		return ok
	case "list", "array":
		_, ok := sliceValues(v)
		return ok
	case "map", "object", "block":
		_, ok := v.(map[string]any)
		return ok
	default:
		return true
	}
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

func boolPtr(v bool) *bool {
	return &v
}

func floatPtr(v float64) *float64 {
	return &v
}

func validateDecisionProgram(prog *DecisionProgram) []Diagnostic {
	if prog == nil {
		return nil
	}
	var diags []Diagnostic
	for id, d := range prog.Decisions {
		if d == nil {
			continue
		}
		if !isDecisionStrategy(d.Strategy) {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q uses invalid strategy %q", id, d.Strategy), Span: d.Span})
		}
		seen := map[string]Span{}
		for _, rule := range d.Rules {
			if rule.ID != "" {
				if first, ok := seen[rule.ID]; ok {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q has duplicate rule %q", id, rule.ID), Span: first})
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q has duplicate rule %q", id, rule.ID), Span: rule.Span})
				}
				seen[rule.ID] = rule.Span
			}
			if effect := scalarString(lookup(rule.Then, "decision")); effect != "" {
				if contract := prog.Contracts[id]; contract != nil && len(contract.Effects) > 0 && !stringIn(effect, contract.Effects) {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q rule %q sets undeclared effect %q", id, rule.ID, effect), Span: rule.Span})
				}
			}
			for _, action := range decisionActionsFrom(rule.Then, "action") {
				if len(prog.Actions) > 0 && prog.Actions[action.Name] == nil {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q rule %q references unknown action %q", id, rule.ID, action.Name), Span: rule.Span})
				}
			}
			diags = append(diags, validateDecisionActionPayloads(id, rule)...)
		}
	}
	for id, ranking := range prog.Rankings {
		if prog.Decisions[id] == nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("ranking %q has no matching decision", id), Span: ranking.Span})
		}
		if ranking.Dataset != "" && prog.Datasets[ranking.Dataset] == nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("ranking %q references unknown dataset %q", id, ranking.Dataset), Span: ranking.Span})
		}
	}
	for id, contract := range prog.Contracts {
		if !isDecisionStrategy(contract.Strategy) {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q uses invalid strategy %q", id, contract.Strategy), Span: contract.Span})
		}
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

func isDecisionStrategy(strategy string) bool {
	switch strategy {
	case "", "deny_overrides", "allow_overrides", "first_match", "highest_priority", "all_must_pass", "collect_all":
		return true
	default:
		return false
	}
}

func stringIn(s string, xs []string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func validateDecisionActionPayloads(decisionID string, rule DecisionRule) []Diagnostic {
	if rule.Then == nil {
		return nil
	}
	var diags []Diagnostic
	for _, key := range []string{"action", "event"} {
		for _, item := range asAnySlice(rule.Then[key]) {
			if scalarString(item) != "" {
				continue
			}
			m, ok := item.(map[string]any)
			if !ok || scalarString(m["id"]) == "" && scalarString(m["name"]) == "" {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q rule %q has invalid %s payload", decisionID, rule.ID, key), Span: rule.Span})
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
	return evaluateDecisionScenarioInternal(program, scenario, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true})
}

func evaluateDecisionScenarioInternal(program *DecisionProgram, scenario *DecisionScenario, opts *Options, evalOpts DecisionEvaluateOptions) (*DecisionScenarioResult, error) {
	if scenario == nil {
		return nil, fmt.Errorf("nil decision scenario")
	}
	result, err := evaluateDecisionInternal(program, scenario.Decision, scenario.Input, opts, evalOpts)
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
	if want := scalarString(scenario.Expect["policy_id"]); want != "" && result.PolicyID != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected policy_id %q", want)})
	}
	if want := scalarString(scenario.Expect["reason"]); want != "" && result.Reason != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected reason %q", want)})
	}
	if want, ok := numericFloat(scenario.Expect["score"]); ok && result.Score != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected score %.4f", want)})
	}
	if want := scalarString(scenario.Expect["action"]); want != "" && !decisionActionsContain(result.Actions, want) {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected action %q", want)})
	}
	if want := scalarString(scenario.Expect["event"]); want != "" && !decisionActionsContain(result.Events, want) {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected event %q", want)})
	}
	if want, ok := scenario.Expect["diagnostics"].(string); ok && want == "none" && len(result.Diagnostics) > 0 {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: "expected no diagnostics"})
	}
	out.Diagnostics = append(out.Diagnostics, result.Diagnostics...)
	if len(result.Diagnostics) > 0 {
		out.Passed = false
	}
	return out, nil
}

func decisionActionsContain(actions []DecisionAction, name string) bool {
	for _, action := range actions {
		if action.Name == name {
			return true
		}
	}
	return false
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
