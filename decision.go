package bcl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type DecisionProgram struct {
	Modules            []string                             `json:"modules,omitempty"`
	Constants          map[string]any                       `json:"constants,omitempty"`
	Decisions          map[string]*DecisionDefinition       `json:"decisions,omitempty"`
	Contracts          map[string]*DecisionContract         `json:"contracts,omitempty"`
	Rankings           map[string]*RankingDefinition        `json:"rankings,omitempty"`
	Datasets           map[string]*DatasetDefinition        `json:"datasets,omitempty"`
	Actions            map[string]map[string]any            `json:"actions,omitempty"`
	Schemas            map[string]any                       `json:"schemas,omitempty"`
	ReasonCodeCatalogs map[string]map[string]map[string]any `json:"reason_code_catalogs,omitempty"`
	Bundles            map[string]*DecisionBundle           `json:"bundles,omitempty"`
	Releases           map[string]*DecisionRelease          `json:"releases,omitempty"`
	Gates              map[string]*DecisionGateDefinition   `json:"gates,omitempty"`
	RuleTemplates      map[string]map[string]any            `json:"rule_templates,omitempty"`
	Governance         map[string]any                       `json:"governance,omitempty"`
	Tests              []DecisionTest                       `json:"tests,omitempty"`
	Diagnostics        []Diagnostic                         `json:"diagnostics,omitempty"`
	Normalized         *Normalized                          `json:"normalized,omitempty"`
}

type DecisionDefinition struct {
	ID       string          `json:"id"`
	Module   string          `json:"module,omitempty"`
	Default  string          `json:"default,omitempty"`
	Strategy string          `json:"strategy,omitempty"`
	Rules    []DecisionRule  `json:"rules,omitempty"`
	Params   []DecisionParam `json:"params,omitempty"`
	Metadata map[string]any  `json:"metadata,omitempty"`
	Span     Span            `json:"span,omitempty"`
}

type DecisionContract struct {
	ID       string   `json:"id"`
	Effects  []string `json:"effects,omitempty"`
	Default  string   `json:"default,omitempty"`
	Strategy string   `json:"strategy,omitempty"`
	Span     Span     `json:"span,omitempty"`
}

type DecisionRule struct {
	ID         string         `json:"id"`
	Effect     string         `json:"effect,omitempty"`
	Priority   int64          `json:"priority,omitempty"`
	Phase      string         `json:"phase,omitempty"`
	Condition  map[string]any `json:"condition,omitempty"`
	Then       map[string]any `json:"then,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	ReasonCode string         `json:"reason_code,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Source     string         `json:"source,omitempty"`
	Order      int            `json:"-"`
	Span       Span           `json:"span,omitempty"`
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
	Source  DatasetSource       `json:"source,omitempty"`
	Records []DecisionCandidate `json:"records,omitempty"`
	Span    Span                `json:"span,omitempty"`
}

type DatasetSource struct {
	Adapter string         `json:"adapter,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
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

type DecisionParam struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Default  any    `json:"default,omitempty"`
}

type DecisionBundle struct {
	ID        string         `json:"id"`
	Decisions []string       `json:"decisions,omitempty"`
	Datasets  []string       `json:"datasets,omitempty"`
	Tests     []string       `json:"tests,omitempty"`
	Release   string         `json:"release,omitempty"`
	Approval  map[string]any `json:"approval,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Span      Span           `json:"span,omitempty"`
}

type DecisionRelease struct {
	ID       string         `json:"id"`
	Bundle   string         `json:"bundle,omitempty"`
	Version  string         `json:"version,omitempty"`
	Stage    string         `json:"stage,omitempty"`
	Approval map[string]any `json:"approval,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Span     Span           `json:"span,omitempty"`
}

type DecisionGateDefinition struct {
	ID                string         `json:"id"`
	Bundle            string         `json:"bundle,omitempty"`
	Decision          string         `json:"decision,omitempty"`
	Dataset           string         `json:"dataset,omitempty"`
	MinPassRate       float64        `json:"min_pass_rate,omitempty"`
	MaxDiagnostics    int64          `json:"max_diagnostics,omitempty"`
	NoDefaultOnly     bool           `json:"no_default_only,omitempty"`
	RequiredRules     []string       `json:"required_rules,omitempty"`
	ForbidTransitions []string       `json:"forbid_transitions,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	Span              Span           `json:"span,omitempty"`
}

type DecisionGateResult struct {
	ID          string               `json:"id"`
	Passed      bool                 `json:"passed"`
	Diagnostics []Diagnostic         `json:"diagnostics,omitempty"`
	Batch       *DecisionBatchReport `json:"batch,omitempty"`
}

type DecisionGateReport struct {
	BundleID    string               `json:"bundle_id,omitempty"`
	Passed      bool                 `json:"passed"`
	Results     []DecisionGateResult `json:"results,omitempty"`
	Diagnostics []Diagnostic         `json:"diagnostics,omitempty"`
}

type DecisionCounterfactual struct {
	Path    string `json:"path,omitempty"`
	Current any    `json:"current,omitempty"`
	Target  any    `json:"target,omitempty"`
	Reason  string `json:"reason,omitempty"`
	RuleID  string `json:"rule_id,omitempty"`
}

type DecisionObservation struct {
	DecisionID       string   `json:"decision_id,omitempty"`
	Effect           string   `json:"effect,omitempty"`
	PolicyID         string   `json:"policy_id,omitempty"`
	ReasonCode       string   `json:"reason_code,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	MatchedRules     []string `json:"matched_rules,omitempty"`
	SelectedRules    []string `json:"selected_rules,omitempty"`
	Score            float64  `json:"score,omitempty"`
	DiagnosticsCount int      `json:"diagnostics_count,omitempty"`
	InputHash        string   `json:"input_hash,omitempty"`
	LatencyMS        int64    `json:"latency_ms,omitempty"`
}

type DecisionPlatformRequest struct {
	Decision        string         `json:"decision"`
	Input           map[string]any `json:"input,omitempty"`
	Bundle          string         `json:"bundle,omitempty"`
	IncludeGates    bool           `json:"include_gates,omitempty"`
	Counterfactuals bool           `json:"counterfactuals,omitempty"`
	IncludeFeatures bool           `json:"include_features,omitempty"`
	Strict          bool           `json:"strict,omitempty"`
}

type DecisionPlatformReport struct {
	Decision       *DecisionResult          `json:"decision,omitempty"`
	Observation    DecisionObservation      `json:"observation,omitempty"`
	Gates          *DecisionGateReport      `json:"gates,omitempty"`
	Features       DecisionPlatformFeatures `json:"features,omitempty"`
	DatasetSources map[string]DatasetSource `json:"dataset_sources,omitempty"`
	Diagnostics    []Diagnostic             `json:"diagnostics,omitempty"`
	Metadata       map[string]any           `json:"metadata,omitempty"`
}

type DecisionPlatformFeatures struct {
	DecisionCount      int      `json:"decision_count,omitempty"`
	RuleCount          int      `json:"rule_count,omitempty"`
	DatasetCount       int      `json:"dataset_count,omitempty"`
	ExternalDatasets   int      `json:"external_datasets,omitempty"`
	GateCount          int      `json:"gate_count,omitempty"`
	BundleCount        int      `json:"bundle_count,omitempty"`
	ReleaseCount       int      `json:"release_count,omitempty"`
	SchemaCount        int      `json:"schema_count,omitempty"`
	ActionCount        int      `json:"action_count,omitempty"`
	RankingCount       int      `json:"ranking_count,omitempty"`
	ReasonCatalogCount int      `json:"reason_catalog_count,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
	Missing            []string `json:"missing,omitempty"`
}

type DecisionResult struct {
	DecisionID      string                   `json:"decision_id,omitempty"`
	Effect          string                   `json:"effect"`
	Allowed         bool                     `json:"allowed"`
	PolicyID        string                   `json:"policy_id,omitempty"`
	Reason          string                   `json:"reason,omitempty"`
	ReasonCode      string                   `json:"reason_code,omitempty"`
	Tags            []string                 `json:"tags,omitempty"`
	Score           float64                  `json:"score,omitempty"`
	Outcome         *DecisionOutcome         `json:"outcome,omitempty"`
	Attributes      map[string]any           `json:"attributes,omitempty"`
	Metadata        map[string]any           `json:"metadata,omitempty"`
	Actions         []DecisionAction         `json:"actions,omitempty"`
	Events          []DecisionAction         `json:"events,omitempty"`
	Obligations     []DecisionAction         `json:"obligations,omitempty"`
	Advice          []DecisionAction         `json:"advice,omitempty"`
	Rank            *DecisionRank            `json:"rank,omitempty"`
	Counterfactuals []DecisionCounterfactual `json:"counterfactuals,omitempty"`
	Evaluated       int                      `json:"evaluated"`
	Trace           []string                 `json:"trace,omitempty"`
	Explain         []DecisionTrace          `json:"explain,omitempty"`
	ExplainGraph    []DecisionExplainNode    `json:"explain_graph,omitempty"`
	Diagnostics     []Diagnostic             `json:"diagnostics,omitempty"`
}

type DecisionAnswer struct {
	DecisionID  string         `json:"decision_id,omitempty"`
	Effect      string         `json:"effect"`
	Allowed     bool           `json:"allowed"`
	Reason      string         `json:"reason,omitempty"`
	ReasonCode  string         `json:"reason_code,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Score       float64        `json:"score,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	Rank        *DecisionRank  `json:"rank,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
}

func (r *DecisionResult) Answer() DecisionAnswer {
	if r == nil {
		return DecisionAnswer{}
	}
	return DecisionAnswer{
		DecisionID:  r.DecisionID,
		Effect:      r.Effect,
		Allowed:     r.Allowed,
		Reason:      r.Reason,
		ReasonCode:  r.ReasonCode,
		Tags:        r.Tags,
		Score:       r.Score,
		Attributes:  r.Attributes,
		Rank:        r.Rank,
		Diagnostics: r.Diagnostics,
	}
}

func DecisionResultAnswer(result *DecisionResult) DecisionAnswer {
	if result == nil {
		return DecisionAnswer{}
	}
	return result.Answer()
}

type DecisionTrace struct {
	RuleID          string   `json:"rule_id,omitempty"`
	Source          string   `json:"source,omitempty"`
	Phase           string   `json:"phase,omitempty"`
	Status          string   `json:"status"`
	Effect          string   `json:"effect,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	ReasonCode      string   `json:"reason_code,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Message         string   `json:"message,omitempty"`
	Candidate       string   `json:"candidate,omitempty"`
	Priority        int64    `json:"priority,omitempty"`
	ConditionResult *bool    `json:"condition_result,omitempty"`
	ScoreDelta      float64  `json:"score_delta,omitempty"`
	Action          string   `json:"action,omitempty"`
	Event           string   `json:"event,omitempty"`
	CandidateScore  *float64 `json:"candidate_score,omitempty"`
}

type DecisionExplainNode struct {
	ID      string         `json:"id,omitempty"`
	Kind    string         `json:"kind"`
	Label   string         `json:"label,omitempty"`
	RuleID  string         `json:"rule_id,omitempty"`
	Status  string         `json:"status,omitempty"`
	Parent  string         `json:"parent,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type DecisionOutcome struct {
	Effect      string           `json:"effect"`
	Allowed     bool             `json:"allowed"`
	PolicyID    string           `json:"policy_id,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	ReasonCode  string           `json:"reason_code,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
	Score       float64          `json:"score,omitempty"`
	Attributes  map[string]any   `json:"attributes,omitempty"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
	Obligations []DecisionAction `json:"obligations,omitempty"`
	Advice      []DecisionAction `json:"advice,omitempty"`
}

func (r *DecisionResult) Reset() {
	if r == nil {
		return
	}
	r.DecisionID = ""
	r.Effect = ""
	r.Allowed = false
	r.PolicyID = ""
	r.Reason = ""
	r.ReasonCode = ""
	r.Tags = r.Tags[:0]
	r.Score = 0
	r.Rank = nil
	r.Evaluated = 0
	r.Actions = r.Actions[:0]
	r.Events = r.Events[:0]
	r.Obligations = r.Obligations[:0]
	r.Advice = r.Advice[:0]
	r.Counterfactuals = r.Counterfactuals[:0]
	r.Trace = r.Trace[:0]
	r.Explain = r.Explain[:0]
	r.ExplainGraph = r.ExplainGraph[:0]
	r.Diagnostics = r.Diagnostics[:0]
	clear(r.Attributes)
	clear(r.Metadata)
	if r.Outcome != nil {
		r.Outcome.Effect = ""
		r.Outcome.Allowed = false
		r.Outcome.PolicyID = ""
		r.Outcome.Reason = ""
		r.Outcome.ReasonCode = ""
		r.Outcome.Tags = nil
		r.Outcome.Score = 0
		r.Outcome.Attributes = nil
		r.Outcome.Metadata = nil
		r.Outcome.Obligations = nil
		r.Outcome.Advice = nil
	}
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
	Verbose       bool
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

type DecisionBatchCase struct {
	ID     string         `json:"id,omitempty"`
	Input  map[string]any `json:"input,omitempty"`
	Expect map[string]any `json:"expect,omitempty"`
}

type DecisionBatchCaseResult struct {
	ID          string          `json:"id,omitempty"`
	Result      *DecisionResult `json:"result,omitempty"`
	Passed      bool            `json:"passed"`
	DefaultOnly bool            `json:"default_only,omitempty"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

type DecisionBatchReport struct {
	DecisionID         string                    `json:"decision_id"`
	Cases              []DecisionBatchCaseResult `json:"cases,omitempty"`
	EffectCounts       map[string]int            `json:"effect_counts,omitempty"`
	RuleHitCounts      map[string]int            `json:"rule_hit_counts,omitempty"`
	MatchedRuleCounts  map[string]int            `json:"matched_rule_counts,omitempty"`
	SelectedRuleCounts map[string]int            `json:"selected_rule_counts,omitempty"`
	HitRules           []string                  `json:"hit_rules,omitempty"`
	UnhitRules         []string                  `json:"unhit_rules,omitempty"`
	DefaultOnlyCases   []string                  `json:"default_only_cases,omitempty"`
	DefaultOnlyCount   int                       `json:"default_only_count,omitempty"`
	FailedCount        int                       `json:"failed_count,omitempty"`
	Diagnostics        []Diagnostic              `json:"diagnostics,omitempty"`
}

type DecisionCompareCase struct {
	ID               string          `json:"id,omitempty"`
	Base             *DecisionResult `json:"base,omitempty"`
	Candidate        *DecisionResult `json:"candidate,omitempty"`
	Changed          bool            `json:"changed"`
	EffectTransition string          `json:"effect_transition,omitempty"`
	PolicyChanged    bool            `json:"policy_changed,omitempty"`
	DiagnosticsDelta int             `json:"diagnostics_delta,omitempty"`
	Diagnostics      []Diagnostic    `json:"diagnostics,omitempty"`
}

type DecisionCompareReport struct {
	DecisionID        string                `json:"decision_id"`
	Cases             []DecisionCompareCase `json:"cases,omitempty"`
	ChangedCases      []string              `json:"changed_cases,omitempty"`
	EffectTransitions map[string]int        `json:"effect_transitions,omitempty"`
	PolicyChanges     int                   `json:"policy_changes,omitempty"`
	BaseReport        *DecisionBatchReport  `json:"base_report,omitempty"`
	CandidateReport   *DecisionBatchReport  `json:"candidate_report,omitempty"`
	Diagnostics       []Diagnostic          `json:"diagnostics,omitempty"`
}

func NewDecisionEngine(program *DecisionProgram, opts *Options) *DecisionEngine {
	verbose := opts != nil && opts.Verbose
	return &DecisionEngine{
		Program: program,
		Options: opts,
		EvaluateOptions: DecisionEvaluateOptions{
			Explain:       true,
			ValidateInput: true,
			Verbose:       verbose,
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

func (e *DecisionEngine) EvaluateInto(decisionID string, input map[string]any, result *DecisionResult) error {
	if e == nil {
		return fmt.Errorf("nil decision engine")
	}
	return e.EvaluateWithOptionsInto(decisionID, input, e.EvaluateOptions, result)
}

func (e *DecisionEngine) EvaluateWithOptionsInto(decisionID string, input map[string]any, evalOpts DecisionEvaluateOptions, result *DecisionResult) error {
	if e == nil {
		return fmt.Errorf("nil decision engine")
	}
	return evaluateDecisionIntoInternal(e.Program, decisionID, input, e.Options, evalOpts, result)
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

func CompileDecisionBundle(programOrDoc any, bundleID string, opts *Options) (*DecisionBundle, error) {
	prog, err := decisionProgramFromAny(programOrDoc, opts)
	if err != nil {
		return nil, err
	}
	if prog == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	bundle := prog.Bundles[bundleID]
	if bundle == nil {
		return nil, fmt.Errorf("unknown decision bundle %q", bundleID)
	}
	return bundle, nil
}

func CompileDecisionRelease(programOrDoc any, releaseID string, opts *Options) (*DecisionRelease, error) {
	prog, err := decisionProgramFromAny(programOrDoc, opts)
	if err != nil {
		return nil, err
	}
	if prog == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	release := prog.Releases[releaseID]
	if release == nil {
		return nil, fmt.Errorf("unknown decision release %q", releaseID)
	}
	return release, nil
}

func decisionProgramFromAny(programOrDoc any, opts *Options) (*DecisionProgram, error) {
	switch x := programOrDoc.(type) {
	case *DecisionProgram:
		return x, nil
	case *Document:
		return CompileDecisionDocument(x, opts)
	default:
		return nil, fmt.Errorf("expected *DecisionProgram or *Document")
	}
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
	if hasErrorDiagnostics(prog.Diagnostics) {
		return prog, ErrorList(prog.Diagnostics)
	}
	return prog, nil
}

type decisionBuilder struct {
	opts       *Options
	consts     map[string]Value
	constVals  map[string]any
	templates  map[string]*Block
	compiler   *compiler
	moduleSeen map[string]bool
	order      int
}

func newDecisionBuilder(opts *Options) *decisionBuilder {
	if opts == nil {
		opts = &Options{}
	}
	b := &decisionBuilder{opts: opts, consts: map[string]Value{}, constVals: map[string]any{}, templates: map[string]*Block{}, moduleSeen: map[string]bool{}}
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
		Constants:          map[string]any{},
		Decisions:          map[string]*DecisionDefinition{},
		Contracts:          map[string]*DecisionContract{},
		Rankings:           map[string]*RankingDefinition{},
		Datasets:           map[string]*DatasetDefinition{},
		Actions:            map[string]map[string]any{},
		Schemas:            map[string]any{},
		ReasonCodeCatalogs: map[string]map[string]map[string]any{},
		Bundles:            map[string]*DecisionBundle{},
		Releases:           map[string]*DecisionRelease{},
		Gates:              map[string]*DecisionGateDefinition{},
		RuleTemplates:      map[string]map[string]any{},
		Governance:         map[string]any{},
		Normalized:         n,
	}
	b.collectConsts(doc.Items)
	b.collectRuleTemplates(doc.Items)
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

func (b *decisionBuilder) collectRuleTemplates(nodes []Node) {
	for _, n := range nodes {
		block, ok := n.(*Block)
		if !ok {
			continue
		}
		if block.Type == "rule_template" && block.ID != "" {
			b.templates[block.ID] = block
		}
		b.collectRuleTemplates(block.Body)
	}
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
		case "decision_table":
			b.mergeDecisionTable(prog, block, module)
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
		case "reason_code_catalog":
			if block.ID != "" {
				prog.ReasonCodeCatalogs[block.ID] = b.reasonCodeCatalogFromBlock(block)
			}
		case "decision_bundle":
			if bundle := b.bundleFromBlock(block); bundle != nil {
				prog.Bundles[bundle.ID] = bundle
			}
		case "decision_release":
			if release := b.releaseFromBlock(block); release != nil {
				prog.Releases[release.ID] = release
			}
		case "gate":
			if gate := b.gateFromBlock(block); gate != nil {
				prog.Gates[gate.ID] = gate
			}
		case "rule_template":
			if block.ID != "" {
				prog.RuleTemplates[block.ID] = b.ruleTemplateBodyFromBlock(block)
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
		case "test_matrix":
			prog.Tests = append(prog.Tests, b.testsFromMatrix(block)...)
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
	d.Params = append(d.Params, b.decisionParamsFromNodes(block.Body)...)
	if approval := b.approvalFromNodes(block.Body); len(approval) > 0 {
		d.Metadata["approval"] = approval
	}
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
			d.Metadata["explicit_default"] = true
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
	if decisionRulesHavePhase(d.Rules) {
		sortDecisionRules(d.Rules)
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
			if d.Metadata == nil {
				d.Metadata = map[string]any{}
			}
			d.Metadata["explicit_default"] = true
		}
		if contract.Strategy != "" {
			d.Strategy = contract.Strategy
		}
	}
}

func (b *decisionBuilder) mergeDecisionTable(prog *DecisionProgram, block *Block, module string) {
	if block.ID == "" {
		return
	}
	d := prog.Decisions[block.ID]
	if d == nil {
		d = &DecisionDefinition{ID: block.ID, Module: module, Default: "deny", Strategy: "first_match", Metadata: map[string]any{}, Span: block.Span}
		prog.Decisions[d.ID] = d
	}
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	d.Params = appendDecisionParams(d.Params, b.decisionParamsFromNodes(block.Body)...)
	if approval := b.approvalFromNodes(block.Body); len(approval) > 0 {
		d.Metadata["approval"] = approval
	}
	for _, n := range block.Body {
		a, ok := n.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "default":
			d.Default = scalarString(b.compiler.value(a.Value))
			if d.Default == "" {
				d.Default = "deny"
			}
			d.Metadata["explicit_default"] = true
		case "strategy":
			if strategy := scalarString(b.compiler.value(a.Value)); strategy != "" {
				d.Strategy = strategy
			}
		case "hit_policy":
			if policy := scalarString(b.compiler.value(a.Value)); policy != "" {
				d.Metadata["hit_policy"] = policy
				d.Strategy = strategyForHitPolicy(policy)
			}
		}
	}
	for _, row := range b.decisionRuleBlocks(block.Body, "row") {
		r := b.decisionRuleFromBlock(row, "decision_table")
		if r.ID == "" {
			r.ID = fmt.Sprintf("row-%d", r.Order)
		}
		d.Rules = append(d.Rules, r)
	}
	sortDecisionRules(d.Rules)
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
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	d.Params = appendDecisionParams(d.Params, b.decisionParamsFromNodes(block.Body)...)
	if approval := b.approvalFromNodes(block.Body); len(approval) > 0 {
		d.Metadata["approval"] = approval
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
	for _, child := range b.decisionRuleBlocks(block.Body, "rule") {
		r := b.decisionRuleFromBlock(child, "rule_set")
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
		case "reason_code":
			rule.ReasonCode = scalarString(b.compiler.value(aa.Value))
			next = j
		case "tags":
			rule.Tags = stringList(b.compiler.value(aa.Value))
			next = j
		case "priority":
			rule.Priority = intValue(b.compiler.value(aa.Value))
			next = j
		case "phase":
			rule.Phase = scalarString(b.compiler.value(aa.Value))
			next = j
		case "version", "status", "effective_from", "effective_until", "owner", "rationale":
			b.applyDecisionRuleMetadata(&rule, aa)
			next = j
		default:
			return rule, next
		}
	}
	return rule, next
}

func (b *decisionBuilder) decisionRuleBlocks(nodes []Node, want string) []*Block {
	var out []*Block
	for _, n := range nodes {
		child, ok := n.(*Block)
		if !ok {
			continue
		}
		if child.Type == want {
			out = append(out, child)
			continue
		}
		if child.Type == "use" && strings.HasPrefix(child.ID, "rule_template.") {
			out = append(out, b.expandRuleTemplateUse(child, want)...)
		}
	}
	return out
}

func (b *decisionBuilder) expandRuleTemplateUse(use *Block, want string) []*Block {
	templateID := strings.TrimPrefix(use.ID, "rule_template.")
	template := b.templates[templateID]
	if template == nil {
		return nil
	}
	var out []*Block
	for _, n := range template.Body {
		child, ok := n.(*Block)
		if !ok || child.Type != want {
			continue
		}
		copyBody := make([]Node, 0, len(child.Body)+len(use.Body))
		copyBody = append(copyBody, child.Body...)
		copyBody = append(copyBody, use.Body...)
		id := child.ID
		if override := scalarString(b.nodesToBody(use.Body)["id"]); override != "" {
			id = override
		}
		if id == "" {
			id = templateID + "-" + want
		}
		out = append(out, &Block{Type: want, ID: templateID + "." + id, Body: copyBody, Span: use.Span})
	}
	return out
}

func (b *decisionBuilder) decisionRuleFromBlock(block *Block, source string) DecisionRule {
	r := DecisionRule{ID: block.ID, Source: source, Order: b.nextOrder(), Span: block.Span}
	for _, item := range block.Body {
		a, ok := item.(*Assignment)
		if !ok {
			continue
		}
		switch a.Name {
		case "id":
			if id := scalarString(b.compiler.value(a.Value)); id != "" && strings.Contains(r.ID, ".") {
				prefix, _, _ := strings.Cut(r.ID, ".")
				r.ID = prefix + "." + id
			}
		case "priority":
			r.Priority = intValue(b.compiler.value(a.Value))
		case "phase":
			r.Phase = scalarString(b.compiler.value(a.Value))
		case "when":
			r.Condition = b.conditionValue(a.Value)
		case "then":
			r.Then = b.valueObject(a.Value)
			r.Effect = decisionEffectFromThen(r.Then)
		case "effect", "decision":
			r.Effect = scalarString(b.compiler.value(a.Value))
		case "reason":
			r.Reason = scalarString(b.compiler.value(a.Value))
		case "reason_code":
			r.ReasonCode = scalarString(b.compiler.value(a.Value))
		case "tags":
			r.Tags = stringList(b.compiler.value(a.Value))
		default:
			b.applyDecisionRuleMetadata(&r, a)
		}
	}
	return r
}

func (b *decisionBuilder) reasonCodeCatalogFromBlock(block *Block) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, n := range block.Body {
		child, ok := n.(*Block)
		if !ok || child.Type != "code" || child.ID == "" {
			continue
		}
		out[child.ID] = b.nodesToBody(child.Body)
	}
	return out
}

func (b *decisionBuilder) bundleFromBlock(block *Block) *DecisionBundle {
	if block.ID == "" {
		return nil
	}
	body := b.nodesToBody(block.Body)
	return &DecisionBundle{
		ID:        block.ID,
		Decisions: stringList(body["decisions"]),
		Datasets:  stringList(body["datasets"]),
		Tests:     stringList(body["tests"]),
		Release:   scalarString(body["release"]),
		Approval:  b.approvalFromNodes(block.Body),
		Metadata:  blockBodyMap(body["metadata"]),
		Span:      block.Span,
	}
}

func (b *decisionBuilder) releaseFromBlock(block *Block) *DecisionRelease {
	if block.ID == "" {
		return nil
	}
	body := b.nodesToBody(block.Body)
	return &DecisionRelease{
		ID:       block.ID,
		Bundle:   scalarString(body["bundle"]),
		Version:  scalarString(body["version"]),
		Stage:    scalarString(body["stage"]),
		Approval: b.approvalFromNodes(block.Body),
		Metadata: blockBodyMap(body["metadata"]),
		Span:     block.Span,
	}
}

func (b *decisionBuilder) gateFromBlock(block *Block) *DecisionGateDefinition {
	if block.ID == "" {
		return nil
	}
	body := b.nodesToBody(block.Body)
	minPass, _ := numericFloat(body["min_pass_rate"])
	gate := &DecisionGateDefinition{
		ID:                block.ID,
		Bundle:            scalarString(body["bundle"]),
		Decision:          scalarString(body["decision"]),
		Dataset:           scalarString(body["dataset"]),
		MinPassRate:       minPass,
		MaxDiagnostics:    intValue(body["max_diagnostics"]),
		NoDefaultOnly:     boolValue(body["no_default_only"]),
		RequiredRules:     stringList(body["required_rule"]),
		ForbidTransitions: stringList(body["forbid_transition"]),
		Metadata:          blockBodyMap(body["metadata"]),
		Span:              block.Span,
	}
	if xs := stringList(body["required_rules"]); len(xs) > 0 {
		gate.RequiredRules = xs
	}
	if xs := stringList(body["forbid_transitions"]); len(xs) > 0 {
		gate.ForbidTransitions = xs
	}
	return gate
}

func (b *decisionBuilder) ruleTemplateBodyFromBlock(block *Block) map[string]any {
	body := b.nodesToBody(block.Body)
	if params := b.decisionParamsFromNodes(block.Body); len(params) > 0 {
		items := make([]any, 0, len(params))
		for _, p := range params {
			items = append(items, map[string]any{"name": p.Name, "type": p.Type, "required": p.Required, "default": p.Default})
		}
		body["params"] = items
	}
	return body
}

func (b *decisionBuilder) approvalFromNodes(nodes []Node) map[string]any {
	for _, n := range nodes {
		switch x := n.(type) {
		case *Block:
			if x.Type == "approval" {
				return b.nodesToBody(x.Body)
			}
		case *Assignment:
			if x.Name == "approval" {
				return b.valueObject(x.Value)
			}
		}
	}
	return nil
}

func (b *decisionBuilder) decisionParamsFromNodes(nodes []Node) []DecisionParam {
	var out []DecisionParam
	for _, n := range nodes {
		p, ok := n.(*ParamDecl)
		if !ok {
			continue
		}
		out = append(out, DecisionParam{
			Name:     p.Name,
			Type:     p.Type,
			Required: p.Required,
			Default:  b.compiler.value(p.Default),
		})
	}
	return out
}

func appendDecisionParams(base []DecisionParam, extra ...DecisionParam) []DecisionParam {
	if len(extra) == 0 {
		return base
	}
	seen := map[string]int{}
	for i, p := range base {
		seen[p.Name] = i
	}
	for _, p := range extra {
		if idx, ok := seen[p.Name]; ok {
			base[idx] = p
			continue
		}
		seen[p.Name] = len(base)
		base = append(base, p)
	}
	return base
}

func (b *decisionBuilder) applyDecisionRuleMetadata(rule *DecisionRule, a *Assignment) {
	switch a.Name {
	case "version", "status", "effective_from", "effective_until", "owner", "rationale":
	default:
		return
	}
	if rule.Metadata == nil {
		rule.Metadata = map[string]any{}
	}
	rule.Metadata[a.Name] = b.compiler.value(a.Value)
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
	d := &DatasetDefinition{ID: block.ID, Module: module, Span: block.Span}
	for _, n := range block.Body {
		switch x := n.(type) {
		case *Block:
			switch x.Type {
			case "record":
				body := b.nodesToBody(x.Body)
				d.Records = append(d.Records, DecisionCandidate{ID: x.ID, Facts: body})
			case "source":
				d.Source = b.resolveDatasetSource(b.datasetSourceFromNodes(x.Body))
			}
		case *Assignment:
			if x.Name == "source" {
				d.Source = b.resolveDatasetSource(b.datasetSourceFromValue(x.Value))
			}
		}
	}
	return d
}

func (b *decisionBuilder) datasetSourceFromNodes(nodes []Node) DatasetSource {
	cfg := b.nodesToBody(nodes)
	return datasetSourceFromConfig(cfg)
}

func (b *decisionBuilder) datasetSourceFromValue(v Value) DatasetSource {
	switch x := v.(type) {
	case *Object:
		return datasetSourceFromConfig(b.nodesToBody(x.Fields))
	default:
		if m, ok := b.compiler.value(v).(map[string]any); ok {
			return datasetSourceFromConfig(m)
		}
		if adapter := scalarString(b.compiler.value(v)); adapter != "" {
			return DatasetSource{Adapter: adapter, Config: map[string]any{"adapter": adapter}}
		}
	}
	return DatasetSource{}
}

func datasetSourceFromConfig(cfg map[string]any) DatasetSource {
	if cfg == nil {
		cfg = map[string]any{}
	}
	adapter := firstNonEmpty(scalarString(cfg["adapter"]), scalarString(cfg["type"]), scalarString(cfg["kind"]))
	return DatasetSource{Adapter: adapter, Config: cfg}
}

func (b *decisionBuilder) resolveDatasetSource(source DatasetSource) DatasetSource {
	if !strings.EqualFold(source.Adapter, "file") || b == nil || b.opts == nil || b.opts.BaseDir == "" || source.Config == nil {
		return source
	}
	path := scalarString(source.Config["path"])
	if path == "" || filepath.IsAbs(path) {
		return source
	}
	source.Config["path"] = filepath.Join(b.opts.BaseDir, path)
	return source
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

func (b *decisionBuilder) testsFromMatrix(block *Block) []DecisionTest {
	var decision string
	for _, n := range block.Body {
		if a, ok := n.(*Assignment); ok && a.Name == "decision" {
			decision = scalarString(b.compiler.value(a.Value))
		}
	}
	var out []DecisionTest
	for _, n := range block.Body {
		child, ok := n.(*Block)
		if !ok || child.Type != "case" {
			continue
		}
		t := b.testFromBlock(child)
		if t.Name == "" {
			t.Name = child.ID
		}
		if block.ID != "" && t.Name != "" {
			t.Name = block.ID + "/" + t.Name
		}
		if t.Decision == "" {
			t.Decision = decision
		}
		out = append(out, t)
	}
	return out
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
	case "default", "effect", "priority", "phase", "when", "then", "reason", "reason_code", "tags", "hit_policy", "version", "status", "effective_from", "effective_until", "owner", "rationale", "match", "tenant", "actions", "resources":
		return false
	}
	return scalarString(b.compiler.value(a.Value)) != ""
}

func strategyForHitPolicy(policy string) string {
	switch policy {
	case "first":
		return "first_match"
	case "priority":
		return "highest_priority"
	case "collect":
		return "collect_all"
	case "unique":
		return "first_match"
	default:
		return policy
	}
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
	hasPhase := decisionRulesHavePhase(rules)
	sort.SliceStable(rules, func(i, j int) bool {
		if hasPhase {
			pi, pj := decisionPhaseIndex(rules[i].Phase), decisionPhaseIndex(rules[j].Phase)
			if pi != pj {
				return pi < pj
			}
		}
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		if rules[i].Order != rules[j].Order {
			return rules[i].Order < rules[j].Order
		}
		return rules[i].ID < rules[j].ID
	})
}

func decisionRulesHavePhase(rules []DecisionRule) bool {
	for _, rule := range rules {
		if rule.Phase != "" {
			return true
		}
	}
	return false
}

func EvaluateDecision(program *DecisionProgram, decision string, input map[string]any, opts *Options) (*DecisionResult, error) {
	verbose := opts != nil && opts.Verbose
	return evaluateDecisionInternal(program, decision, input, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true, Verbose: verbose})
}

func evaluateDecisionInternal(program *DecisionProgram, decision string, input map[string]any, opts *Options, evalOpts DecisionEvaluateOptions) (*DecisionResult, error) {
	return evaluateDecisionInternalWithStack(program, decision, input, opts, evalOpts, nil)
}

func evaluateDecisionInternalWithStack(program *DecisionProgram, decision string, input map[string]any, opts *Options, evalOpts DecisionEvaluateOptions, stack map[string]bool) (*DecisionResult, error) {
	result := &DecisionResult{}
	if err := evaluateDecisionIntoInternalWithStack(program, decision, input, opts, evalOpts, result, stack); err != nil {
		return nil, err
	}
	return result, nil
}

func evaluateDecisionIntoInternal(program *DecisionProgram, decision string, input map[string]any, opts *Options, evalOpts DecisionEvaluateOptions, result *DecisionResult) error {
	return evaluateDecisionIntoInternalWithStack(program, decision, input, opts, evalOpts, result, nil)
}

func evaluateDecisionIntoInternalWithStack(program *DecisionProgram, decision string, input map[string]any, opts *Options, evalOpts DecisionEvaluateOptions, result *DecisionResult, stack map[string]bool) error {
	if result == nil {
		return fmt.Errorf("nil decision result")
	}
	result.Reset()
	if program == nil {
		return fmt.Errorf("nil decision program")
	}
	def := program.Decisions[decision]
	if def == nil {
		return fmt.Errorf("unknown decision %q", decision)
	}
	if stack == nil {
		stack = map[string]bool{}
	}
	if stack[decision] {
		return fmt.Errorf("recursive decision call %q", decision)
	}
	stack[decision] = true
	defer delete(stack, decision)
	vars := decisionVars(program, input)
	result.DecisionID = decision
	result.Effect = firstNonEmpty(def.Default, "deny")
	if len(def.Params) > 0 {
		params, paramDiags := resolveDecisionParams(def.Params, input)
		if len(params) > 0 {
			vars["param"] = params
		}
		result.Diagnostics = append(result.Diagnostics, paramDiags...)
		if len(result.Diagnostics) > 0 && evalOpts.Strict {
			result.Allowed = result.Effect == "allow"
			return nil
		}
	}
	if evalOpts.ValidateInput {
		result.Diagnostics = append(result.Diagnostics, validateDecisionInput(program, decision, input, opts)...)
		if len(result.Diagnostics) > 0 && evalOpts.Strict {
			result.Allowed = result.Effect == "allow"
			return nil
		}
	}
	strategy := firstNonEmpty(def.Strategy, "deny_overrides")
	explain := evalOpts.Explain
	evalTime := decisionEvaluationTime(input, opts)
	conditionOpts := decisionEvalOptions(opts, program, input, evalOpts, stack, result)
	directSelect := strategy == "first_match" || strategy == "highest_priority"
	var selectedDirect DecisionRule
	hasSelectedDirect := false
	var matchedPolicies []DecisionRule
	var matchedSelectable []DecisionRule
	for _, rule := range def.Rules {
		if ok, msg := decisionRuleActive(rule, evalTime); !ok {
			if explain {
				result.Trace = append(result.Trace, rule.ID+": skipped effective window")
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "skipped_effective_window", Priority: rule.Priority, Message: msg})
			}
			continue
		}
		ok := true
		if rule.Condition != nil {
			var err error
			ok, err = evalNormalizedCondition(rule.Condition, vars, conditionOpts)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: rule.Span})
				if explain {
					result.Trace = append(result.Trace, rule.ID+": condition error")
					result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "error", Priority: rule.Priority, Message: err.Error()})
				}
				continue
			}
		}
		result.Evaluated++
		if !ok {
			if explain {
				result.Trace = append(result.Trace, rule.ID+": condition false")
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "skipped", Priority: rule.Priority, ConditionResult: boolPtr(false), Message: explainConditionFailure(rule.Condition, vars, opts)})
			}
			continue
		}
		if explain {
			result.Trace = append(result.Trace, rule.ID+": matched")
			result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "matched", Effect: rule.Effect, Reason: rule.Reason, ReasonCode: rule.ReasonCode, Tags: rule.Tags, Priority: rule.Priority, ConditionResult: boolPtr(true)})
		}
		if rule.Source == "rule_set" || rule.Effect == "" && rule.Then != nil {
			summary := applyDecisionRule(result, rule, opts)
			if explain {
				appendApplyTrace(result, rule, summary)
			}
			continue
		}
		if rule.Effect != "" {
			matchedSelectable = append(matchedSelectable, rule)
			if directSelect {
				if !hasSelectedDirect {
					selectedDirect = rule
					hasSelectedDirect = true
				}
			} else {
				matchedPolicies = append(matchedPolicies, rule)
			}
		}
	}
	if scalarString(def.Metadata["hit_policy"]) == "unique" && len(matchedSelectable) > 1 {
		ids := make([]string, 0, len(matchedSelectable))
		for _, rule := range matchedSelectable {
			ids = append(ids, rule.ID)
		}
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q unique hit policy matched multiple rules: %s", decision, strings.Join(ids, ", "))})
	}
	if directSelect && hasSelectedDirect {
		summary := applyDecisionRule(result, selectedDirect, opts)
		if explain {
			appendApplyTrace(result, selectedDirect, summary)
			result.Explain = append(result.Explain, DecisionTrace{RuleID: selectedDirect.ID, Source: selectedDirect.Source, Phase: selectedDirect.Phase, Status: "selected", Effect: selectedDirect.Effect, Reason: selectedDirect.Reason, ReasonCode: selectedDirect.ReasonCode, Tags: selectedDirect.Tags, Priority: selectedDirect.Priority})
		}
	} else {
		for _, selected := range chooseDecisionPolicies(strategy, matchedPolicies) {
			summary := applyDecisionRule(result, selected, opts)
			if explain {
				appendApplyTrace(result, selected, summary)
				result.Explain = append(result.Explain, DecisionTrace{RuleID: selected.ID, Source: selected.Source, Phase: selected.Phase, Status: "selected", Effect: selected.Effect, Reason: selected.Reason, ReasonCode: selected.ReasonCode, Tags: selected.Tags, Priority: selected.Priority})
			}
		}
	}
	if rank := evaluateDecisionRank(program, decision, vars, input, opts, result, explain); rank != nil {
		result.Rank = rank
	}
	result.Allowed = result.Effect == "allow"
	result.Outcome = normalizedDecisionOutcome(result)
	if !evalOpts.Explain {
		result.Explain = nil
	}
	result.ExplainGraph = decisionExplainGraph(result)
	return nil
}

func decisionPhaseIndex(phase string) int {
	switch phase {
	case "validate":
		return 0
	case "guard":
		return 1
	case "score":
		return 2
	case "decide":
		return 3
	case "notify":
		return 4
	case "":
		return 3
	default:
		return 5
	}
}

func explainConditionFailure(condition map[string]any, vars map[string]any, opts *Options) string {
	if len(condition) == 0 {
		return "condition false"
	}
	if expr := scalarString(condition["expr"]); expr != "" {
		if detail := explainExpressionFailure(expr, vars, opts); detail != "" {
			return detail
		}
		return "condition false: " + expr
	}
	op := scalarString(condition["op"])
	children, _ := condition["children"].([]any)
	switch op {
	case "all", "":
		for _, raw := range children {
			child, _ := raw.(map[string]any)
			ok, err := evalNormalizedCondition(child, vars, opts)
			if err == nil && !ok {
				return explainConditionFailure(child, vars, opts)
			}
		}
	case "any":
		return "condition false: no any branch matched"
	case "not":
		return "condition false: negated condition matched"
	case "none":
		return "condition false: at least one none branch matched"
	}
	return "condition false"
}

func explainExpressionFailure(expr string, vars map[string]any, opts *Options) string {
	for _, op := range []string{" not_in ", " starts_with ", " ends_with ", " contains ", " matches ", " has_any ", " has_all ", " == ", " != ", " >= ", " <= ", " > ", " < ", " in ", " has "} {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		leftRaw := strings.TrimSpace(expr[:idx])
		rightRaw := strings.TrimSpace(expr[idx+len(op):])
		evalOpts := evalOptionsFrom(opts, vars)
		left, leftErr := EvalExpr(leftRaw, evalOpts)
		right, rightErr := EvalExpr(rightRaw, evalOpts)
		if leftErr != nil || rightErr != nil {
			return ""
		}
		return fmt.Sprintf("condition false: %s %s %s (actual=%v expected=%v)", leftRaw, strings.TrimSpace(op), rightRaw, left, right)
	}
	return ""
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
	if input == nil {
		input = map[string]any{}
	}
	if len(program.Constants) == 0 {
		if context, ok := input["context"].(map[string]any); !ok || len(context) == 0 {
			return input
		}
	}
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

func resolveDecisionParams(params []DecisionParam, input map[string]any) (map[string]any, []Diagnostic) {
	out := map[string]any{}
	var diags []Diagnostic
	for _, p := range params {
		if p.Name == "" {
			continue
		}
		value := lookup(input, "params."+p.Name)
		if value == nil {
			value = lookup(input, "context.params."+p.Name)
		}
		if value == nil {
			value = p.Default
		}
		if value == nil {
			if p.Required {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision param %q is required", p.Name)})
			}
			continue
		}
		if p.Type != "" && !runtimeTypeMatches(p.Type, value) {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision param %q should be %s", p.Name, p.Type)})
			continue
		}
		out[p.Name] = value
	}
	return out, diags
}

func decisionEvalOptions(opts *Options, program *DecisionProgram, input map[string]any, evalOpts DecisionEvaluateOptions, stack map[string]bool, current *DecisionResult) *Options {
	cp := &Options{}
	if opts != nil {
		*cp = *opts
	}
	funcs := map[string]EvalFunction{}
	if opts != nil {
		for k, v := range opts.EvalFunctions {
			funcs[k] = v
		}
		addRuntimeScopeFunctions(funcs, "context", opts.Context)
		addRuntimeScopeFunctions(funcs, "session", opts.Session)
		addRequestHeaderFunction(funcs, opts.Context)
	}
	funcs["decision"] = func(args []any, _ *EvalOptions) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("decision requires 1 argument")
		}
		id := scalarString(args[0])
		if id == "" {
			id = fmt.Sprint(args[0])
		}
		res, err := evaluateDecisionInternalWithStack(program, id, input, cp, evalOpts, cloneBoolMap(stack))
		if err != nil {
			if current != nil {
				current.Diagnostics = append(current.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
			}
			return nil, err
		}
		if current != nil && evalOpts.Explain {
			current.Explain = append(current.Explain, DecisionTrace{Source: "decision_call", Status: "decision_call", Message: id, Effect: res.Effect, Reason: res.Reason, ReasonCode: res.ReasonCode, Tags: res.Tags})
		}
		return decisionResultMap(res), nil
	}
	cp.EvalFunctions = funcs
	return cp
}

func addRequestHeaderFunction(funcs map[string]EvalFunction, contextScope map[string]any) {
	funcs["context.request.header"] = func(args []any, _ *EvalOptions) (any, error) {
		if len(args) == 0 {
			return nil, fmt.Errorf("context.request.header requires a header name")
		}
		name := scalarString(args[0])
		if name == "" {
			name = fmt.Sprint(args[0])
		}
		headers, _ := lookup(contextScope, "request.headers").(map[string]any)
		if value, ok := lookupHeader(headers, name); ok {
			return value, nil
		}
		if len(args) > 1 {
			return args[1], nil
		}
		return nil, nil
	}
}

func lookupHeader(headers map[string]any, name string) (any, bool) {
	if headers == nil {
		return nil, false
	}
	for _, key := range []string{name, strings.ToLower(name), httpCanonicalHeaderKey(name)} {
		if value, ok := headers[key]; ok {
			return value, true
		}
	}
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return nil, false
}

func httpCanonicalHeaderKey(name string) string {
	parts := strings.Split(strings.ToLower(name), "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "-")
}

func addRuntimeScopeFunctions(funcs map[string]EvalFunction, scopeName string, scope map[string]any) {
	for _, name := range []string{scopeName, scopeName + ".required", scopeName + ".int", scopeName + ".bool", scopeName + ".float", scopeName + ".duration", scopeName + ".bytes", scopeName + ".list"} {
		fnName := name
		funcs[fnName] = func(args []any, _ *EvalOptions) (any, error) {
			return runtimeScopeCall(scopeName, scope, fnName, args)
		}
	}
}

func runtimeScopeCall(scopeName string, scope map[string]any, fnName string, args []any) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s call requires a key", scopeName)
	}
	key := scalarString(args[0])
	if key == "" {
		key = fmt.Sprint(args[0])
	}
	val := lookup(scope, key)
	if val == nil {
		if fnName == scopeName+".required" {
			return nil, fmt.Errorf("required %s %q is not set", scopeName, key)
		}
		if len(args) > 1 {
			return args[1], nil
		}
		return nil, nil
	}
	switch fnName {
	case scopeName + ".int":
		if i, ok := numericInt(val); ok {
			return i, nil
		}
		var i int64
		fmt.Sscan(fmt.Sprint(val), &i)
		return i, nil
	case scopeName + ".bool":
		if b, ok := val.(bool); ok {
			return b, nil
		}
		s := strings.ToLower(fmt.Sprint(val))
		return s == "true" || s == "1" || s == "yes", nil
	case scopeName + ".float":
		if f, ok := numericFloat(val); ok {
			return f, nil
		}
		var f float64
		fmt.Sscan(fmt.Sprint(val), &f)
		return f, nil
	case scopeName + ".list":
		switch xs := val.(type) {
		case []any:
			return xs, nil
		case []string:
			out := make([]any, 0, len(xs))
			for _, item := range xs {
				out = append(out, item)
			}
			return out, nil
		}
		sep := ","
		if len(args) > 1 {
			sep = scalarString(args[1])
		}
		return strings.Split(fmt.Sprint(val), sep), nil
	case scopeName + ".duration":
		return map[string]any{"$duration": fmt.Sprint(val)}, nil
	case scopeName + ".bytes":
		return map[string]any{"$bytes": fmt.Sprint(val)}, nil
	default:
		return val, nil
	}
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func decisionResultMap(result *DecisionResult) map[string]any {
	if result == nil {
		return nil
	}
	return map[string]any{
		"decision_id": result.DecisionID,
		"effect":      result.Effect,
		"allowed":     result.Allowed,
		"policy_id":   result.PolicyID,
		"reason":      result.Reason,
		"reason_code": result.ReasonCode,
		"tags":        append([]string(nil), result.Tags...),
		"score":       result.Score,
		"attributes":  result.Attributes,
		"metadata":    result.Metadata,
	}
}

func decisionEvaluationTime(input map[string]any, opts *Options) time.Time {
	for _, path := range []string{"time.now", "context.time.now"} {
		if t, ok := parseDecisionTime(lookup(input, path)); ok {
			return t
		}
	}
	return optionsNow(opts)
}

func decisionRuleActive(rule DecisionRule, now time.Time) (bool, string) {
	status := strings.ToLower(scalarString(rule.Metadata["status"]))
	if status != "" && status != "active" {
		return false, "rule status is " + status
	}
	if from, ok := parseDecisionTime(rule.Metadata["effective_from"]); ok && now.Before(from) {
		return false, "rule is not yet effective"
	}
	if until, ok := parseDecisionTime(rule.Metadata["effective_until"]); ok && now.After(until) {
		return false, "rule is no longer effective"
	}
	return true, ""
}

func parseDecisionTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x.UTC(), true
	case string:
		if x == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339, "2006-01-02"} {
			if t, err := time.Parse(layout, x); err == nil {
				return t.UTC(), true
			}
		}
	case map[string]any:
		for _, key := range []string{"$datetime", "$timestamp", "$date"} {
			if s, ok := x[key].(string); ok {
				return parseDecisionTime(s)
			}
		}
	}
	return time.Time{}, false
}

type decisionApplySummary struct {
	ScoreDelta      float64
	ActionStart     int
	EventStart      int
	ObligationStart int
	AdviceStart     int
}

func applyDecisionRule(result *DecisionResult, rule DecisionRule, opts *Options) decisionApplySummary {
	beforeScore := result.Score
	beforeActions := len(result.Actions)
	beforeEvents := len(result.Events)
	beforeObligations := len(result.Obligations)
	beforeAdvice := len(result.Advice)
	if rule.Effect != "" {
		result.Effect = rule.Effect
		result.PolicyID = rule.ID
		result.Reason = rule.Reason
		result.ReasonCode = rule.ReasonCode
		result.Tags = append([]string(nil), rule.Tags...)
	}
	if rule.Then != nil {
		if effect := scalarString(lookup(rule.Then, "decision")); effect != "" {
			result.Effect = effect
			result.PolicyID = rule.ID
		}
		applyDecisionOutcome(result, rule)
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
		result.Obligations = append(result.Obligations, decisionActionsFrom(rule.Then, "obligation")...)
		result.Advice = append(result.Advice, decisionActionsFrom(rule.Then, "advice")...)
	}
	return decisionApplySummary{
		ScoreDelta:      result.Score - beforeScore,
		ActionStart:     beforeActions,
		EventStart:      beforeEvents,
		ObligationStart: beforeObligations,
		AdviceStart:     beforeAdvice,
	}
}

func appendApplyTrace(result *DecisionResult, rule DecisionRule, summary decisionApplySummary) {
	if summary.ScoreDelta != 0 {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "score", Priority: rule.Priority, ScoreDelta: summary.ScoreDelta})
	}
	for _, action := range result.Actions[summary.ActionStart:] {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "action", Priority: rule.Priority, Action: action.Name})
	}
	for _, event := range result.Events[summary.EventStart:] {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "event", Priority: rule.Priority, Event: event.Name})
	}
	for _, obligation := range result.Obligations[summary.ObligationStart:] {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "obligation", Priority: rule.Priority, Action: obligation.Name})
	}
	for _, advice := range result.Advice[summary.AdviceStart:] {
		result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Phase: rule.Phase, Status: "advice", Priority: rule.Priority, Action: advice.Name})
	}
}

func decisionExplainGraph(result *DecisionResult) []DecisionExplainNode {
	if result == nil || len(result.Explain) == 0 {
		return nil
	}
	nodes := make([]DecisionExplainNode, 0, len(result.Explain)+1)
	root := "decision:" + result.DecisionID
	nodes = append(nodes, DecisionExplainNode{ID: root, Kind: "decision", Label: result.DecisionID, Status: result.Effect, Details: map[string]any{"effect": result.Effect, "policy_id": result.PolicyID}})
	for i, step := range result.Explain {
		kind := "trace"
		switch step.Status {
		case "matched", "selected", "skipped", "error", "skipped_effective_window":
			kind = "rule"
		case "score":
			kind = "score"
		case "action", "event", "obligation", "advice":
			kind = step.Status
		case "decision_call":
			kind = "decision_call"
		}
		id := fmt.Sprintf("%s:%d", kind, i)
		label := firstNonEmpty(step.RuleID, step.Action, step.Event, step.Message, step.Status)
		details := map[string]any{"status": step.Status}
		if step.Effect != "" {
			details["effect"] = step.Effect
		}
		if step.ReasonCode != "" {
			details["reason_code"] = step.ReasonCode
		}
		if len(step.Tags) > 0 {
			details["tags"] = step.Tags
		}
		if step.ScoreDelta != 0 {
			details["score_delta"] = step.ScoreDelta
		}
		nodes = append(nodes, DecisionExplainNode{ID: id, Kind: kind, Label: label, RuleID: step.RuleID, Status: step.Status, Parent: root, Details: details})
	}
	return nodes
}

func applyDecisionOutcome(result *DecisionResult, rule DecisionRule) {
	then := rule.Then
	outcome := blockBodyMap(then["outcome"])
	if effect := firstNonEmpty(scalarString(outcome["effect"]), scalarString(outcome["decision"])); effect != "" {
		result.Effect = effect
		result.PolicyID = rule.ID
	}
	if reason := scalarString(outcome["reason"]); reason != "" {
		result.Reason = reason
	}
	if reasonCode := firstNonEmpty(scalarString(outcome["reason_code"]), rule.ReasonCode); reasonCode != "" {
		result.ReasonCode = reasonCode
	}
	if len(rule.Tags) > 0 {
		result.Tags = append([]string(nil), rule.Tags...)
	}
	if attrs := firstNonEmptyDecisionMap(decisionMapValue(outcome["attributes"]), blockBodyMap(outcome["attributes"]), blockBodyMap(then["attributes"])); len(attrs) > 0 {
		if result.Attributes == nil {
			result.Attributes = map[string]any{}
		}
		mergeMap(result.Attributes, attrs)
	}
	if metadata := firstNonEmptyDecisionMap(decisionMapValue(outcome["metadata"]), blockBodyMap(outcome["metadata"]), blockBodyMap(then["metadata"])); len(metadata) > 0 {
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		mergeMap(result.Metadata, metadata)
	}
}

func decisionEffectFromThen(then map[string]any) string {
	if len(then) == 0 {
		return ""
	}
	if effect := scalarString(lookup(then, "decision")); effect != "" {
		return effect
	}
	outcome := blockBodyMap(then["outcome"])
	return firstNonEmpty(scalarString(outcome["effect"]), scalarString(outcome["decision"]))
}

func normalizedDecisionOutcome(result *DecisionResult) *DecisionOutcome {
	if result.Outcome == nil {
		result.Outcome = &DecisionOutcome{}
	}
	result.Outcome.Effect = result.Effect
	result.Outcome.Allowed = result.Allowed
	result.Outcome.PolicyID = result.PolicyID
	result.Outcome.Reason = result.Reason
	result.Outcome.ReasonCode = result.ReasonCode
	result.Outcome.Tags = append([]string(nil), result.Tags...)
	result.Outcome.Score = result.Score
	result.Outcome.Attributes = result.Attributes
	result.Outcome.Metadata = result.Metadata
	result.Outcome.Obligations = result.Obligations
	result.Outcome.Advice = result.Advice
	return result.Outcome
}

func blockBodyMap(v any) map[string]any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		for _, item := range x {
			if m := blockBodyMap(item); len(m) > 0 {
				return m
			}
		}
		return nil
	case []map[string]any:
		for _, item := range x {
			if m := blockBodyMap(item); len(m) > 0 {
				return m
			}
		}
		return nil
	default:
		if body, ok := decisionMapValue(x)["body"].([]any); ok {
			return decisionBodyItemsMap(body)
		}
		if body, ok := decisionMapValue(x)["body"].(map[string]any); ok {
			return body
		}
		if m := decisionMapValue(x); len(m) > 0 {
			return m
		}
	}
	return nil
}

func decisionBodyItemsMap(items []any) map[string]any {
	out := map[string]any{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name := scalarString(m["name"]); name != "" {
			out[name] = decisionLiteralValue(m["value"])
			continue
		}
		typ := scalarString(m["type"])
		if typ == "" || typ == "assignment" {
			if name := scalarString(m["name"]); name != "" {
				out[name] = decisionLiteralValue(m["value"])
			}
			continue
		}
		if body, ok := m["body"].([]any); ok {
			out[typ] = decisionBodyItemsMap(body)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func decisionLiteralValue(v any) any {
	if m, ok := v.(map[string]any); ok {
		if data, exists := m["data"]; exists {
			return data
		}
	}
	return v
}

func decisionMapValue(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func firstNonEmptyDecisionMap(a, b, c map[string]any) map[string]any {
	if len(a) > 0 {
		return a
	}
	if len(b) > 0 {
		return b
	}
	if len(c) > 0 {
		return c
	}
	return nil
}

func applyThenExpressions(result *DecisionResult, then map[string]any) {
	raw := then["$expr"]
	switch x := raw.(type) {
	case nil:
		return
	case []any:
		for _, item := range x {
			applyThenExpression(result, item)
		}
	case []map[string]any:
		for _, item := range x {
			applyThenExpression(result, item)
		}
	default:
		applyThenExpression(result, x)
	}
}

func applyThenExpression(result *DecisionResult, item any) {
	expr := scalarString(lookupExpr(item))
	if !strings.HasPrefix(expr, "score") {
		return
	}
	rest := strings.TrimSpace(strings.TrimPrefix(expr, "score"))
	if !strings.HasPrefix(rest, "+=") {
		return
	}
	value := strings.TrimSpace(strings.TrimPrefix(rest, "+="))
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return
	}
	if n, ok := numericFloat(parseInlineNumber(value)); ok {
		result.Score += n
	}
}

func decisionActionsFrom(then map[string]any, key string) []DecisionAction {
	switch x := then[key].(type) {
	case nil:
		return nil
	case []any:
		out := make([]DecisionAction, 0, len(x))
		for _, item := range x {
			if action, ok := decisionActionFrom(item); ok {
				out = append(out, action)
			}
		}
		return out
	case []map[string]any:
		out := make([]DecisionAction, 0, len(x))
		for _, item := range x {
			if action, ok := decisionActionFrom(item); ok {
				out = append(out, action)
			}
		}
		return out
	default:
		if action, ok := decisionActionFrom(x); ok {
			return []DecisionAction{action}
		}
		return nil
	}
}

func decisionActionFrom(item any) (DecisionAction, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		if name := scalarString(item); name != "" {
			return DecisionAction{Name: name}, true
		}
		return DecisionAction{}, false
	}
	name := scalarString(m["id"])
	body, _ := m["body"].(map[string]any)
	if name == "" {
		name = scalarString(m["name"])
	}
	if name == "" {
		return DecisionAction{}, false
	}
	return DecisionAction{Name: name, Params: body}, true
}

func evaluateDecisionRank(program *DecisionProgram, decision string, vars, input map[string]any, opts *Options, result *DecisionResult, explain bool) *DecisionRank {
	ranking := program.Rankings[decision]
	if ranking == nil {
		return nil
	}
	ctx := context.Background()
	candidates, iterator, err := candidateSourceFor(ctx, program, ranking, input, opts)
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: ranking.Span})
		return nil
	}
	if iterator != nil {
		defer iterator.Close()
	}
	var best *DecisionRank
	for i := 0; ; i++ {
		candidate, ok, err := nextRankingCandidate(ctx, candidates, i, iterator)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: ranking.Span})
			break
		}
		if !ok {
			break
		}
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
				if explain {
					result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "error", Candidate: candidate.ID, Priority: rule.Priority, Message: err.Error()})
				}
				pass = false
				break
			}
			if !ok {
				if explain {
					result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "skipped", Candidate: candidate.ID, Priority: rule.Priority, ConditionResult: boolPtr(false), Message: "ranking condition false"})
				}
				pass = false
				break
			}
			if explain {
				result.Explain = append(result.Explain, DecisionTrace{RuleID: rule.ID, Source: rule.Source, Status: "matched", Candidate: candidate.ID, Priority: rule.Priority, ConditionResult: boolPtr(true)})
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
		if explain {
			result.Explain = append(result.Explain, DecisionTrace{Status: "candidate_score", Candidate: candidate.ID, CandidateScore: floatPtr(score)})
		}
		rank := &DecisionRank{ID: candidate.ID, Score: score, Facts: candidate.Facts}
		if best == nil || rank.Score > best.Score {
			best = rank
		}
	}
	if best != nil {
		if explain {
			result.Explain = append(result.Explain, DecisionTrace{Status: "ranked", Candidate: best.ID, Message: fmt.Sprintf("score %.4f", best.Score)})
		}
	}
	return best
}

func candidateSourceFor(ctx context.Context, program *DecisionProgram, ranking *RankingDefinition, input map[string]any, opts *Options) ([]DecisionCandidate, DecisionRecordIterator, error) {
	candidates := candidatesForInput(input)
	if len(candidates) > 0 {
		return candidates, nil, nil
	}
	if ranking.Dataset != "" {
		it, err := OpenDecisionDataset(ctx, program, ranking.Dataset, opts)
		if err != nil {
			return nil, nil, err
		}
		return nil, it, nil
	}
	return nil, nil, nil
}

func nextRankingCandidate(ctx context.Context, candidates []DecisionCandidate, idx int, iterator DecisionRecordIterator) (DecisionCandidate, bool, error) {
	if iterator == nil {
		if idx >= len(candidates) {
			return DecisionCandidate{}, false, nil
		}
		return candidates[idx], true, nil
	}
	return iterator.Next(ctx)
}

func candidatesForInput(input map[string]any) []DecisionCandidate {
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
	return out
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
	return ValidateSchemaValue(decisionID, schema, input)
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
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		_, hasTypedWrapper := m["$"+want]
		return hasTypedWrapper || want == "date" || want == "datetime" || want == "duration" || want == "bytes" || want == "regex" || want == "cidr"
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

func boolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true") || strings.EqualFold(x, "yes")
	default:
		return false
	}
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

func hasErrorDiagnostics(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == "" || d.Severity == "error" {
			return true
		}
	}
	return false
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
		if d.Metadata == nil || d.Metadata["explicit_default"] != true {
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q has no explicit default", id), Span: d.Span})
		}
		seen := map[string]Span{}
		conditionEffects := map[string]string{}
		conditionSpans := map[string]Span{}
		catchAllSeen := false
		catchAllRule := ""
		for _, rule := range d.Rules {
			if rule.ID != "" {
				if first, ok := seen[rule.ID]; ok {
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q has duplicate rule %q", id, rule.ID), Span: first})
					diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q has duplicate rule %q", id, rule.ID), Span: rule.Span})
				}
				seen[rule.ID] = rule.Span
			}
			if rule.Phase != "" && !isDecisionPhase(rule.Phase) {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision %q rule %q uses invalid phase %q", id, rule.ID, rule.Phase), Span: rule.Span})
			}
			if firstNonEmpty(d.Strategy, "deny_overrides") == "first_match" {
				if catchAllSeen {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q rule %q is unreachable after catch-all rule %q", id, rule.ID, catchAllRule), Span: rule.Span})
				}
				if isCatchAllDecisionRule(rule) {
					catchAllSeen = true
					catchAllRule = rule.ID
				}
			}
			if key := decisionConditionKey(rule.Condition); key != "" && rule.Effect != "" {
				if prev, ok := conditionEffects[key]; ok && prev != rule.Effect {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q has conflicting effects %q/%q for equivalent conditions", id, prev, rule.Effect), Span: conditionSpans[key]})
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q has conflicting effects %q/%q for equivalent conditions", id, prev, rule.Effect), Span: rule.Span})
				} else if !ok {
					conditionEffects[key] = rule.Effect
					conditionSpans[key] = rule.Span
				}
			}
			if effect := decisionEffectFromThen(rule.Then); effect != "" {
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
			if rule.ReasonCode != "" {
				if catalog := prog.ReasonCodeCatalogs[id]; len(catalog) > 0 && catalog[rule.ReasonCode] == nil {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q rule %q references unknown reason code %q", id, rule.ID, rule.ReasonCode), Span: rule.Span})
				}
			}
		}
		diags = append(diags, validateDecisionRuleAnalysis(id, d, prog)...)
	}
	for id, ranking := range prog.Rankings {
		if prog.Decisions[id] == nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("ranking %q has no matching decision", id), Span: ranking.Span})
		}
		if ranking.Dataset != "" && prog.Datasets[ranking.Dataset] == nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("ranking %q references unknown dataset %q", id, ranking.Dataset), Span: ranking.Span})
		}
	}
	for id, dataset := range prog.Datasets {
		if dataset == nil {
			continue
		}
		if dataset.Source.Adapter != "" && len(dataset.Records) > 0 {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("dataset %q cannot mix source and inline records", id), Span: dataset.Span})
		}
		if dataset.Source.Adapter == "" && len(dataset.Source.Config) > 0 {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("dataset %q source requires adapter", id), Span: dataset.Span})
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
	diags = append(diags, validateDecisionBundlesAndReleases(prog)...)
	diags = append(diags, validateDecisionDependencyCycles(prog)...)
	return diags
}

func validateDecisionBundlesAndReleases(prog *DecisionProgram) []Diagnostic {
	var diags []Diagnostic
	for id, bundle := range prog.Bundles {
		if bundle == nil {
			continue
		}
		for _, decisionID := range bundle.Decisions {
			if prog.Decisions[decisionID] == nil {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision bundle %q references unknown decision %q", id, decisionID), Span: bundle.Span})
			}
		}
		for _, datasetID := range bundle.Datasets {
			if prog.Datasets[datasetID] == nil {
				diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision bundle %q references unknown dataset %q", id, datasetID), Span: bundle.Span})
			}
		}
	}
	for id, release := range prog.Releases {
		if release == nil {
			continue
		}
		if release.Bundle != "" && prog.Bundles[release.Bundle] == nil {
			diags = append(diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("decision release %q references unknown bundle %q", id, release.Bundle), Span: release.Span})
		}
		stage := strings.ToLower(release.Stage)
		if stage == "prod" || stage == "production" {
			if approvalStatus(release.Approval) != "approved" {
				diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("production release %q is not approved", id), Span: release.Span})
			}
			if bundle := prog.Bundles[release.Bundle]; bundle != nil {
				if approvalStatus(bundle.Approval) != "approved" {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("production release %q uses unapproved bundle %q", id, release.Bundle), Span: bundle.Span})
				}
				for _, decisionID := range bundle.Decisions {
					if d := prog.Decisions[decisionID]; d != nil && approvalStatus(blockBodyMap(d.Metadata["approval"])) != "approved" {
						diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("production release %q includes unapproved decision %q", id, decisionID), Span: d.Span})
					}
				}
			}
		}
	}
	for id, catalog := range prog.ReasonCodeCatalogs {
		used := map[string]bool{}
		if d := prog.Decisions[id]; d != nil {
			for _, rule := range d.Rules {
				if rule.ReasonCode != "" {
					used[rule.ReasonCode] = true
				}
			}
		}
		for code := range catalog {
			if !used[code] {
				diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q reason code %q is unused", id, code)})
			}
		}
	}
	return diags
}

func approvalStatus(approval map[string]any) string {
	return strings.ToLower(scalarString(approval["status"]))
}

func validateDecisionDependencyCycles(prog *DecisionProgram) []Diagnostic {
	graph := map[string][]string{}
	for id, d := range prog.Decisions {
		for _, rule := range d.Rules {
			for _, expr := range conditionExprs(rule.Condition) {
				graph[id] = append(graph[id], decisionCallRefsFromExpr(expr)...)
			}
		}
	}
	var diags []Diagnostic
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var dfs func(string, []string)
	dfs = func(id string, path []string) {
		if visiting[id] {
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision dependency cycle detected: %s -> %s", strings.Join(path, " -> "), id)})
			return
		}
		if visited[id] {
			return
		}
		visiting[id] = true
		for _, next := range graph[id] {
			dfs(next, append(path, id))
		}
		visiting[id] = false
		visited[id] = true
	}
	for id := range graph {
		dfs(id, nil)
	}
	return diags
}

func decisionCallRefsFromExpr(expr string) []string {
	var out []string
	for {
		idx := strings.Index(expr, `decision("`)
		quote := `"`
		if idx < 0 {
			idx = strings.Index(expr, `decision('`)
			quote = `'`
		}
		if idx < 0 {
			break
		}
		rest := expr[idx+len(`decision(`)+1:]
		end := strings.Index(rest, quote)
		if end < 0 {
			break
		}
		out = append(out, rest[:end])
		expr = rest[end+1:]
	}
	return out
}

func validateDecisionRuleAnalysis(id string, d *DecisionDefinition, prog *DecisionProgram) []Diagnostic {
	var diags []Diagnostic
	if d == nil {
		return nil
	}
	if d.Strategy == "first_match" || d.Strategy == "highest_priority" {
		for i := 0; i < len(d.Rules); i++ {
			for j := i + 1; j < len(d.Rules); j++ {
				a, b := d.Rules[i], d.Rules[j]
				if a.Priority == b.Priority && a.Effect != "" && b.Effect != "" && a.Effect != b.Effect && rulesMayOverlap(a, b) {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q has ambiguous same-priority rules %q/%q", id, a.ID, b.ID), Span: b.Span})
				}
			}
		}
	}
	if scalarString(d.Metadata["hit_policy"]) == "unique" {
		for i := 0; i < len(d.Rules); i++ {
			for j := i + 1; j < len(d.Rules); j++ {
				if d.Rules[i].Effect != "" && d.Rules[j].Effect != "" && rulesMayOverlap(d.Rules[i], d.Rules[j]) {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q unique hit policy may match multiple rules %q/%q", id, d.Rules[i].ID, d.Rules[j].ID), Span: d.Rules[j].Span})
				}
			}
		}
	}
	for _, warning := range numericOverlapWarnings(id, d.Rules) {
		diags = append(diags, warning)
	}
	diags = append(diags, enumCoverageWarnings(id, d.Rules, prog.Schemas[id])...)
	diags = append(diags, schemaPatternWarnings(id, d.Rules, prog.Schemas[id])...)
	return diags
}

func rulesMayOverlap(a, b DecisionRule) bool {
	ka := decisionConditionKey(a.Condition)
	kb := decisionConditionKey(b.Condition)
	if ka != "" && kb != "" && ka == kb {
		return true
	}
	ca, oka := simpleNumericCondition(a.Condition)
	cb, okb := simpleNumericCondition(b.Condition)
	if oka && okb && ca.Path == cb.Path {
		return numericConditionsOverlap(ca, cb)
	}
	return a.Condition == nil || b.Condition == nil
}

type numericCondition struct {
	Path string
	Op   string
	Val  float64
	Span Span
}

func simpleNumericCondition(condition map[string]any) (numericCondition, bool) {
	expr := scalarString(condition["expr"])
	if expr == "" {
		children := asAnySlice(condition["children"])
		if len(children) == 1 {
			if child, ok := children[0].(map[string]any); ok {
				return simpleNumericCondition(child)
			}
		}
		return numericCondition{}, false
	}
	for _, op := range []string{">=", "<=", ">", "<", "=="} {
		needle := " " + op + " "
		idx := strings.Index(expr, needle)
		if idx < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(needle):])
		if strings.ContainsAny(left, " ()[]{}+-*/%,") {
			return numericCondition{}, false
		}
		if n, ok := numericFloat(parseInlineNumber(right)); ok {
			return numericCondition{Path: left, Op: op, Val: n}, true
		}
	}
	return numericCondition{}, false
}

func numericConditionsOverlap(a, b numericCondition) bool {
	amin, amax := numericBounds(a)
	bmin, bmax := numericBounds(b)
	return amin <= bmax && bmin <= amax
}

func numericBounds(c numericCondition) (float64, float64) {
	switch c.Op {
	case "==":
		return c.Val, c.Val
	case ">=", ">":
		return c.Val, math.Inf(1)
	case "<=", "<":
		return math.Inf(-1), c.Val
	default:
		return math.Inf(-1), math.Inf(1)
	}
}

func numericOverlapWarnings(id string, rules []DecisionRule) []Diagnostic {
	var diags []Diagnostic
	for i := 0; i < len(rules); i++ {
		a, oka := simpleNumericCondition(rules[i].Condition)
		if !oka {
			continue
		}
		for j := i + 1; j < len(rules); j++ {
			b, okb := simpleNumericCondition(rules[j].Condition)
			if okb && a.Path == b.Path && rules[i].Effect != rules[j].Effect && numericConditionsOverlap(a, b) {
				diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q has overlapping numeric rules on %s", id, a.Path), Span: rules[j].Span})
			}
		}
	}
	return diags
}

func enumCoverageWarnings(id string, rules []DecisionRule, schema any) []Diagnostic {
	var diags []Diagnostic
	fields := flattenSchemaFields(schema)
	seen := map[string]map[string]bool{}
	for _, rule := range rules {
		path, value, ok := simpleEqualityCondition(rule.Condition)
		if !ok {
			continue
		}
		field := fields[path]
		if len(asAnySlice(field["enum"])) == 0 {
			continue
		}
		if seen[path] == nil {
			seen[path] = map[string]bool{}
		}
		seen[path][fmt.Sprint(value)] = true
	}
	for path, got := range seen {
		enum := asAnySlice(fields[path]["enum"])
		if len(got) > 0 && len(got) < len(enum) {
			diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q does not cover all enum values for %s", id, path)})
		}
	}
	return diags
}

func simpleEqualityCondition(condition map[string]any) (string, any, bool) {
	expr := scalarString(condition["expr"])
	if expr == "" {
		children := asAnySlice(condition["children"])
		if len(children) == 1 {
			if child, ok := children[0].(map[string]any); ok {
				return simpleEqualityCondition(child)
			}
		}
		return "", nil, false
	}
	idx := strings.Index(expr, " == ")
	if idx < 0 {
		return "", nil, false
	}
	left := strings.TrimSpace(expr[:idx])
	right := strings.TrimSpace(expr[idx+4:])
	if strings.ContainsAny(left, " ()[]{}+-*/%,") {
		return "", nil, false
	}
	if lit, ok := parsePatternLiteral(right); ok {
		return left, lit, true
	}
	return "", nil, false
}

func schemaPatternWarnings(id string, rules []DecisionRule, schema any) []Diagnostic {
	if schema == nil {
		return nil
	}
	fields := flattenSchemaFields(schema)
	var diags []Diagnostic
	for _, rule := range rules {
		for _, expr := range conditionExprs(rule.Condition) {
			for _, mp := range matchPatternsFromExpr(expr) {
				subject := strings.TrimSpace(mp.Subject)
				base := fields[subject]
				if len(base) == 0 {
					continue
				}
				diags = append(diags, validatePatternAgainstSchema(id, subject, mp.Pattern, fields, rule.Span)...)
			}
		}
	}
	return diags
}

func conditionExprs(condition map[string]any) []string {
	if len(condition) == 0 {
		return nil
	}
	if expr := scalarString(condition["expr"]); expr != "" {
		return []string{expr}
	}
	var out []string
	for _, raw := range asAnySlice(condition["children"]) {
		if child, ok := raw.(map[string]any); ok {
			out = append(out, conditionExprs(child)...)
		}
	}
	return out
}

type matchPatternRef struct {
	Subject string
	Pattern string
}

func matchPatternsFromExpr(expr string) []matchPatternRef {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "match(") && strings.HasSuffix(expr, ")") {
		args := splitTopLevel(expr[len("match("):len(expr)-1], ',')
		if len(args) < 2 {
			return nil
		}
		var out []matchPatternRef
		for _, arg := range args[1:] {
			arg = strings.TrimSpace(arg)
			if !strings.HasPrefix(arg, "case(") || !strings.HasSuffix(arg, ")") {
				continue
			}
			parts := splitTopLevel(arg[len("case("):len(arg)-1], ',')
			if len(parts) < 2 {
				continue
			}
			pat, _ := splitPatternGuard(parts[0])
			out = append(out, matchPatternRef{Subject: strings.TrimSpace(args[0]), Pattern: pat})
		}
		return out
	}
	if strings.HasPrefix(expr, "match ") {
		open := strings.IndexByte(expr, '{')
		close := strings.LastIndexByte(expr, '}')
		if open < 0 || close < open {
			return nil
		}
		subject := strings.TrimSpace(expr[len("match "):open])
		var out []matchPatternRef
		for _, rawCase := range splitMatchCases(expr[open+1 : close]) {
			pat, _, _, err := parseRawCase(rawCase)
			if err == nil {
				out = append(out, matchPatternRef{Subject: subject, Pattern: pat})
			}
		}
		return out
	}
	return nil
}

func flattenSchemaFields(schema any) map[string]map[string]any {
	out := map[string]map[string]any{}
	var walk func(prefix string, fields []map[string]any)
	walk = func(prefix string, fields []map[string]any) {
		for _, field := range fields {
			name := fieldName(field)
			if name == "" {
				continue
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			out[path] = field
			walk(path, schemaFieldsFromAny(field["fields"]))
		}
	}
	if m, ok := schema.(map[string]any); ok {
		walk("", schemaFieldsFromAny(m["fields"]))
	}
	return out
}

func validatePatternAgainstSchema(id, subject, pattern string, fields map[string]map[string]any, sp Span) []Diagnostic {
	node := compilePatternNode(pattern)
	var diags []Diagnostic
	var walk func(path string, n *patternNode)
	walk = func(path string, n *patternNode) {
		if n == nil {
			return
		}
		switch n.Kind {
		case patternObject:
			for _, pf := range n.Fields {
				childPath := path + "." + pf.Key
				field := fields[childPath]
				if len(field) == 0 {
					diags = append(diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q pattern references unknown schema field %q", id, childPath), Span: sp})
					continue
				}
				validatePatternNodeForField(id, childPath, pf.Node, field, sp, &diags)
				walk(childPath, pf.Node)
			}
		case patternAlias, patternSome, patternNot, patternAnyCollection, patternAllCollection:
			walk(path, n.Child)
		case patternAlt, patternList, patternTypeCtor:
			for _, child := range n.Children {
				walk(path, child)
			}
		}
	}
	walk(subject, node)
	return diags
}

func validatePatternNodeForField(id, path string, node *patternNode, field map[string]any, sp Span, diags *[]Diagnostic) {
	if node == nil {
		return
	}
	if node.Kind == patternAlias {
		validatePatternNodeForField(id, path, node.Child, field, sp, diags)
		return
	}
	if node.Kind == patternMissing {
		if required, _ := field["required"].(bool); required {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q pattern expects required field %q to be missing", id, path), Span: sp})
		}
		return
	}
	if node.Kind == patternTypedBind {
		if typ := scalarString(field["type"]); typ != "" && !schemaTypeCompatible(typ, node.Type) {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q pattern binding for %q expects %s but schema is %s", id, path, node.Type, typ), Span: sp})
		}
	}
	if node.Kind == patternLiteral {
		enum := asAnySlice(field["enum"])
		if len(enum) > 0 {
			found := false
			for _, item := range enum {
				if equalLoose(item, node.Literal) {
					found = true
					break
				}
			}
			if !found {
				*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q pattern literal for %q is outside schema enum", id, path), Span: sp})
			}
		}
		if typ := scalarString(field["type"]); typ != "" && !runtimeTypeMatches(typ, node.Literal) {
			*diags = append(*diags, Diagnostic{Severity: "warning", Message: fmt.Sprintf("decision %q pattern literal for %q is incompatible with schema type %s", id, path, typ), Span: sp})
		}
	}
}

func schemaTypeCompatible(schemaType, patternType string) bool {
	schemaType = resolveBuiltinAlias(schemaType)
	if schemaType == patternType {
		return true
	}
	return schemaType == "float" && patternType == "number" || schemaType == "number" && patternType == "float" || schemaType == "list" && patternType == "array" || schemaType == "object" && patternType == "map"
}

func decisionConditionKey(condition map[string]any) string {
	if len(condition) == 0 {
		return "<catch-all>"
	}
	b, err := json.Marshal(condition)
	if err != nil {
		return ""
	}
	return string(b)
}

func isCatchAllDecisionRule(rule DecisionRule) bool {
	if len(rule.Condition) == 0 {
		return true
	}
	if op := scalarString(rule.Condition["op"]); op == "expr" {
		expr := strings.TrimSpace(scalarString(rule.Condition["expr"]))
		return expr == "true"
	}
	return false
}

func isDecisionStrategy(strategy string) bool {
	switch strategy {
	case "", "deny_overrides", "allow_overrides", "first_match", "highest_priority", "all_must_pass", "collect_all":
		return true
	default:
		return false
	}
}

func isDecisionPhase(phase string) bool {
	switch phase {
	case "validate", "guard", "score", "decide", "notify":
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
	for _, key := range []string{"action", "event", "obligation", "advice"} {
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
	verbose := opts != nil && opts.Verbose
	return evaluateDecisionScenarioInternal(program, scenario, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true, Verbose: verbose})
}

func EvaluateDecisionBatch(program *DecisionProgram, decisionID string, cases []DecisionBatchCase, opts *Options) (*DecisionBatchReport, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	if decisionID == "" {
		return nil, fmt.Errorf("missing decision id")
	}
	report := &DecisionBatchReport{
		DecisionID:         decisionID,
		EffectCounts:       map[string]int{},
		RuleHitCounts:      map[string]int{},
		MatchedRuleCounts:  map[string]int{},
		SelectedRuleCounts: map[string]int{},
	}
	for i, c := range cases {
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("case-%d", i+1)
		}
		appendDecisionBatchCase(report, program, decisionID, id, c.Input, c.Expect, opts)
	}
	report.HitRules = sortedMapKeys(report.RuleHitCounts)
	report.UnhitRules = decisionUnhitRules(program, decisionID, report.RuleHitCounts)
	return report, nil
}

func appendDecisionBatchCase(report *DecisionBatchReport, program *DecisionProgram, decisionID, id string, input, expect map[string]any, opts *Options) {
	verbose := opts != nil && opts.Verbose
	result, err := evaluateDecisionInternal(program, decisionID, input, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true, Verbose: verbose})
	cr := DecisionBatchCaseResult{ID: id, Result: result, Passed: true}
	if err != nil {
		cr.Passed = false
		cr.Diagnostics = append(cr.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
		report.Diagnostics = append(report.Diagnostics, cr.Diagnostics...)
		report.FailedCount++
		report.Cases = append(report.Cases, cr)
		return
	}
	report.EffectCounts[result.Effect]++
	for _, step := range result.Explain {
		if step.RuleID == "" {
			continue
		}
		switch step.Status {
		case "matched":
			report.MatchedRuleCounts[step.RuleID]++
			report.RuleHitCounts[step.RuleID]++
		case "selected":
			report.SelectedRuleCounts[step.RuleID]++
			report.RuleHitCounts[step.RuleID]++
		}
	}
	cr.DefaultOnly = result.PolicyID == ""
	if cr.DefaultOnly {
		report.DefaultOnlyCount++
		report.DefaultOnlyCases = append(report.DefaultOnlyCases, id)
	}
	cr.Diagnostics = append(cr.Diagnostics, result.Diagnostics...)
	if !decisionBatchExpectationPassed(result, expect, &cr.Diagnostics) || len(result.Diagnostics) > 0 {
		cr.Passed = false
		report.FailedCount++
	}
	if !verbose {
		result.Trace = nil
		result.Explain = nil
		result.ExplainGraph = nil
	}
	report.Diagnostics = append(report.Diagnostics, cr.Diagnostics...)
	report.Cases = append(report.Cases, cr)
}

func EvaluateDecisionDataset(program *DecisionProgram, decisionID, datasetID string, opts *Options) (*DecisionBatchReport, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	if decisionID == "" {
		return nil, fmt.Errorf("missing decision id")
	}
	ctx := context.Background()
	it, err := OpenDecisionDataset(ctx, program, datasetID, opts)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	report := &DecisionBatchReport{
		DecisionID:         decisionID,
		EffectCounts:       map[string]int{},
		RuleHitCounts:      map[string]int{},
		MatchedRuleCounts:  map[string]int{},
		SelectedRuleCounts: map[string]int{},
	}
	for i := 0; ; i++ {
		record, ok, err := it.Next(ctx)
		if err != nil {
			return report, err
		}
		if !ok {
			break
		}
		id := record.ID
		if id == "" {
			id = fmt.Sprintf("case-%d", i+1)
		}
		appendDecisionBatchCase(report, program, decisionID, id, record.Facts, nil, opts)
	}
	report.HitRules = sortedMapKeys(report.RuleHitCounts)
	report.UnhitRules = decisionUnhitRules(program, decisionID, report.RuleHitCounts)
	return report, nil
}

func decisionUnhitRules(program *DecisionProgram, decisionID string, hits map[string]int) []string {
	if program == nil || program.Decisions == nil {
		return nil
	}
	def := program.Decisions[decisionID]
	if def == nil {
		return nil
	}
	var out []string
	for _, rule := range def.Rules {
		if rule.ID != "" && hits[rule.ID] == 0 {
			out = append(out, rule.ID)
		}
	}
	sort.Strings(out)
	return out
}

func sortedMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func CompareDecisionBatch(base, candidate *DecisionProgram, decisionID string, cases []DecisionBatchCase, opts *Options) (*DecisionCompareReport, error) {
	compareCases := make([]DecisionBatchCase, 0, len(cases))
	for _, c := range cases {
		compareCases = append(compareCases, DecisionBatchCase{ID: c.ID, Input: c.Input})
	}
	baseReport, err := EvaluateDecisionBatch(base, decisionID, compareCases, opts)
	if err != nil {
		return nil, err
	}
	candidateReport, err := EvaluateDecisionBatch(candidate, decisionID, compareCases, opts)
	if err != nil {
		return nil, err
	}
	report := &DecisionCompareReport{
		DecisionID:        decisionID,
		EffectTransitions: map[string]int{},
		BaseReport:        baseReport,
		CandidateReport:   candidateReport,
	}
	max := len(baseReport.Cases)
	if len(candidateReport.Cases) > max {
		max = len(candidateReport.Cases)
	}
	for i := 0; i < max; i++ {
		var baseCase, candCase DecisionBatchCaseResult
		if i < len(baseReport.Cases) {
			baseCase = baseReport.Cases[i]
		}
		if i < len(candidateReport.Cases) {
			candCase = candidateReport.Cases[i]
		}
		id := firstNonEmpty(baseCase.ID, candCase.ID, fmt.Sprintf("case-%d", i+1))
		cc := DecisionCompareCase{ID: id, Base: baseCase.Result, Candidate: candCase.Result}
		if baseCase.Result != nil && candCase.Result != nil {
			cc.EffectTransition = baseCase.Result.Effect + "->" + candCase.Result.Effect
			cc.PolicyChanged = baseCase.Result.PolicyID != candCase.Result.PolicyID
			cc.DiagnosticsDelta = len(candCase.Result.Diagnostics) - len(baseCase.Result.Diagnostics)
			cc.Changed = baseCase.Result.Effect != candCase.Result.Effect || cc.PolicyChanged || cc.DiagnosticsDelta != 0
			if cc.Changed {
				report.ChangedCases = append(report.ChangedCases, id)
				report.EffectTransitions[cc.EffectTransition]++
			}
			if cc.PolicyChanged {
				report.PolicyChanges++
			}
		} else {
			cc.Changed = true
			report.ChangedCases = append(report.ChangedCases, id)
		}
		cc.Diagnostics = append(cc.Diagnostics, baseCase.Diagnostics...)
		cc.Diagnostics = append(cc.Diagnostics, candCase.Diagnostics...)
		report.Diagnostics = append(report.Diagnostics, cc.Diagnostics...)
		report.Cases = append(report.Cases, cc)
	}
	return report, nil
}

func CompareDecisionDataset(base, candidate *DecisionProgram, decisionID, datasetID string, opts *Options) (*DecisionCompareReport, error) {
	if base == nil {
		return nil, fmt.Errorf("nil base decision program")
	}
	sourceProgram := base
	if base.Datasets[datasetID] == nil && candidate != nil {
		sourceProgram = candidate
	}
	ctx := context.Background()
	it, err := OpenDecisionDataset(ctx, sourceProgram, datasetID, opts)
	if err != nil {
		return nil, err
	}
	defer it.Close()
	var cases []DecisionBatchCase
	for {
		record, ok, err := it.Next(ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		cases = append(cases, DecisionBatchCase{ID: record.ID, Input: record.Facts})
	}
	return CompareDecisionBatch(base, candidate, decisionID, cases, opts)
}

func EvaluateDecisionGates(program *DecisionProgram, bundleID string, opts *Options) (*DecisionGateReport, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	report := &DecisionGateReport{BundleID: bundleID, Passed: true}
	for _, gate := range program.Gates {
		if gate == nil {
			continue
		}
		if bundleID != "" && gate.Bundle != "" && gate.Bundle != bundleID {
			continue
		}
		if bundleID != "" && gate.Bundle == "" {
			if bundle := program.Bundles[bundleID]; bundle == nil || !stringIn(gate.Decision, bundle.Decisions) {
				continue
			}
		}
		result := DecisionGateResult{ID: gate.ID, Passed: true}
		if gate.Decision == "" || gate.Dataset == "" {
			result.Passed = false
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("gate %q requires decision and dataset", gate.ID), Span: gate.Span})
		} else {
			batch, err := EvaluateDecisionDataset(program, gate.Decision, gate.Dataset, opts)
			result.Batch = batch
			if err != nil {
				result.Passed = false
				result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: err.Error(), Span: gate.Span})
			} else {
				passedCases := len(batch.Cases) - batch.FailedCount
				passRate := 1.0
				if len(batch.Cases) > 0 {
					passRate = float64(passedCases) / float64(len(batch.Cases))
				}
				if gate.MinPassRate > 0 && passRate < gate.MinPassRate {
					result.Passed = false
					result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("gate %q pass rate %.2f below %.2f", gate.ID, passRate, gate.MinPassRate), Span: gate.Span})
				}
				if gate.MaxDiagnostics > 0 && int64(len(batch.Diagnostics)) > gate.MaxDiagnostics {
					result.Passed = false
					result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("gate %q diagnostics count %d above %d", gate.ID, len(batch.Diagnostics), gate.MaxDiagnostics), Span: gate.Span})
				}
				if gate.NoDefaultOnly && batch.DefaultOnlyCount > 0 {
					result.Passed = false
					result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("gate %q has default-only cases", gate.ID), Span: gate.Span})
				}
				for _, ruleID := range gate.RequiredRules {
					if batch.RuleHitCounts[ruleID] == 0 {
						result.Passed = false
						result.Diagnostics = append(result.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("gate %q required rule %q was not hit", gate.ID, ruleID), Span: gate.Span})
					}
				}
			}
		}
		if !result.Passed {
			report.Passed = false
		}
		report.Diagnostics = append(report.Diagnostics, result.Diagnostics...)
		report.Results = append(report.Results, result)
	}
	if bundleID != "" && len(report.Results) == 0 {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Severity: "warning", Message: fmt.Sprintf("bundle %q has no decision gates", bundleID)})
	}
	return report, nil
}

func EvaluateDecisionPlatform(program *DecisionProgram, req DecisionPlatformRequest, opts *Options) (*DecisionPlatformReport, error) {
	if program == nil {
		return nil, fmt.Errorf("nil decision program")
	}
	if req.Decision == "" {
		return nil, fmt.Errorf("missing decision id")
	}
	report := &DecisionPlatformReport{
		DatasetSources: decisionDatasetSources(program),
		Metadata:       map[string]any{},
	}
	start := time.Now()
	var result *DecisionResult
	var err error
	verbose := opts != nil && opts.Verbose
	if req.Counterfactuals {
		result, err = CounterfactualDecision(program, req.Decision, req.Input, opts)
	} else {
		result, err = evaluateDecisionInternal(program, req.Decision, req.Input, opts, DecisionEvaluateOptions{Explain: true, ValidateInput: true, Strict: req.Strict, Verbose: verbose})
	}
	if result != nil {
		if result.Metadata == nil {
			result.Metadata = map[string]any{}
		}
		result.Metadata["latency_ms"] = time.Since(start).Milliseconds()
		report.Decision = result
		report.Observation = DecisionResultObservation(result, req.Input, opts)
		report.Diagnostics = append(report.Diagnostics, result.Diagnostics...)
	}
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
		return report, err
	}
	if req.Strict && result != nil && len(result.Diagnostics) > 0 {
		err := fmt.Errorf("decision %q evaluation produced diagnostics", req.Decision)
		report.Diagnostics = append(report.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
		return report, err
	}
	if req.IncludeGates || req.Bundle != "" {
		bundleID := req.Bundle
		if bundleID == "" {
			bundleID = decisionBundleFor(program, req.Decision)
		}
		gates, gateErr := EvaluateDecisionGates(program, bundleID, opts)
		report.Gates = gates
		if gates != nil {
			report.Diagnostics = append(report.Diagnostics, gates.Diagnostics...)
		}
		if gateErr != nil {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Severity: "error", Message: gateErr.Error()})
			return report, gateErr
		}
	}
	if req.IncludeFeatures {
		report.Features = InspectDecisionPlatform(program)
	}
	report.Metadata["latency_ms"] = time.Since(start).Milliseconds()
	return report, nil
}

func InspectDecisionPlatform(program *DecisionProgram) DecisionPlatformFeatures {
	if program == nil {
		return DecisionPlatformFeatures{Missing: []string{"program"}}
	}
	features := DecisionPlatformFeatures{
		DecisionCount:      len(program.Decisions),
		DatasetCount:       len(program.Datasets),
		GateCount:          len(program.Gates),
		BundleCount:        len(program.Bundles),
		ReleaseCount:       len(program.Releases),
		SchemaCount:        len(program.Schemas),
		ActionCount:        len(program.Actions),
		RankingCount:       len(program.Rankings),
		ReasonCatalogCount: len(program.ReasonCodeCatalogs),
	}
	for _, decision := range program.Decisions {
		if decision != nil {
			features.RuleCount += len(decision.Rules)
		}
	}
	for _, dataset := range program.Datasets {
		if dataset != nil && dataset.Source.Adapter != "" && !strings.EqualFold(dataset.Source.Adapter, "inline") {
			features.ExternalDatasets++
		}
	}
	addCapability := func(name string, ok bool) {
		if ok {
			features.Capabilities = append(features.Capabilities, name)
		} else {
			features.Missing = append(features.Missing, name)
		}
	}
	addCapability("decisions", features.DecisionCount > 0)
	addCapability("rules", features.RuleCount > 0)
	addCapability("datasets", features.DatasetCount > 0)
	addCapability("external_dataset_adapters", features.ExternalDatasets > 0)
	addCapability("schemas", features.SchemaCount > 0)
	addCapability("gates", features.GateCount > 0)
	addCapability("bundles", features.BundleCount > 0)
	addCapability("reason_catalogs", features.ReasonCatalogCount > 0)
	addCapability("rankings", features.RankingCount > 0)
	addCapability("actions", features.ActionCount > 0)
	sort.Strings(features.Capabilities)
	sort.Strings(features.Missing)
	return features
}

func decisionDatasetSources(program *DecisionProgram) map[string]DatasetSource {
	if program == nil || len(program.Datasets) == 0 {
		return nil
	}
	out := map[string]DatasetSource{}
	for id, dataset := range program.Datasets {
		if dataset == nil {
			continue
		}
		source := dataset.Source
		if source.Adapter == "" {
			source = DatasetSource{Adapter: "inline", Config: map[string]any{"records": int64(len(dataset.Records))}}
		}
		out[id] = source
	}
	return out
}

func decisionBundleFor(program *DecisionProgram, decisionID string) string {
	if program == nil || decisionID == "" {
		return ""
	}
	var ids []string
	for id, bundle := range program.Bundles {
		if bundle != nil && stringIn(decisionID, bundle.Decisions) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func CounterfactualDecision(program *DecisionProgram, decisionID string, input map[string]any, opts *Options) (*DecisionResult, error) {
	result, err := EvaluateDecision(program, decisionID, input, opts)
	if err != nil {
		return result, err
	}
	def := program.Decisions[decisionID]
	if def == nil {
		return result, nil
	}
	vars := decisionVars(program, input)
	if params, _ := resolveDecisionParams(def.Params, input); len(params) > 0 {
		vars["param"] = params
	}
	for _, rule := range def.Rules {
		if traceHasRuleStatus(result.Explain, rule.ID, "matched") {
			continue
		}
		for _, expr := range conditionExprs(rule.Condition) {
			if suggestion, ok := counterfactualFromExpr(rule.ID, expr, vars); ok {
				result.Counterfactuals = append(result.Counterfactuals, suggestion)
				break
			}
		}
		if len(result.Counterfactuals) >= 3 {
			break
		}
	}
	return result, nil
}

func DecisionResultObservation(result *DecisionResult, input map[string]any, opts *Options) DecisionObservation {
	if result == nil {
		return DecisionObservation{}
	}
	payload, _ := json.Marshal(input)
	sum := sha256.Sum256(payload)
	obs := DecisionObservation{
		DecisionID:       result.DecisionID,
		Effect:           result.Effect,
		PolicyID:         result.PolicyID,
		ReasonCode:       result.ReasonCode,
		Tags:             append([]string(nil), result.Tags...),
		MatchedRules:     traceRuleIDs(result.Explain, "matched"),
		SelectedRules:    traceRuleIDs(result.Explain, "selected"),
		Score:            result.Score,
		DiagnosticsCount: len(result.Diagnostics),
		InputHash:        fmt.Sprintf("%x", sum[:]),
	}
	if n, ok := intScalarValue(lookup(result.Metadata, "latency_ms")); ok {
		obs.LatencyMS = int64(n)
	}
	return obs
}

func traceHasRuleStatus(trace []DecisionTrace, ruleID, status string) bool {
	for _, step := range trace {
		if step.RuleID == ruleID && step.Status == status {
			return true
		}
	}
	return false
}

func counterfactualFromExpr(ruleID, expr string, vars map[string]any) (DecisionCounterfactual, bool) {
	for _, op := range []string{" >= ", " <= ", " == ", " != ", " > ", " < "} {
		idx := strings.Index(expr, op)
		if idx < 0 {
			continue
		}
		left := strings.TrimSpace(expr[:idx])
		right := strings.TrimSpace(expr[idx+len(op):])
		if left == "" || strings.ContainsAny(left, " ()[]{}+-*/%,") {
			return DecisionCounterfactual{}, false
		}
		expected, ok := parsePatternLiteral(right)
		if !ok {
			expected = lookup(vars, right)
			if expected == nil {
				expected = parseInlineNumber(strings.Trim(right, `"`))
			}
		}
		current := lookup(vars, left)
		if compareSimple(current, expected, strings.TrimSpace(op)) {
			return DecisionCounterfactual{}, false
		}
		target := expected
		switch strings.TrimSpace(op) {
		case "!=":
			target = fmt.Sprintf("not %v", expected)
		case ">":
			if n, ok := numericFloat(expected); ok {
				target = n + 1
			}
		case "<":
			if n, ok := numericFloat(expected); ok {
				target = n - 1
			}
		}
		return DecisionCounterfactual{
			Path:    left,
			Current: current,
			Target:  target,
			Reason:  fmt.Sprintf("make %s satisfy %s", left, strings.TrimSpace(op)),
			RuleID:  ruleID,
		}, true
	}
	return DecisionCounterfactual{}, false
}

func compareSimple(left, right any, op string) bool {
	switch op {
	case "==":
		return equalLoose(left, right)
	case "!=":
		return !equalLoose(left, right)
	case ">=", ">", "<=", "<":
		lf, lok := numericFloat(left)
		rf, rok := numericFloat(right)
		if !lok || !rok {
			return false
		}
		switch op {
		case ">=":
			return lf >= rf
		case ">":
			return lf > rf
		case "<=":
			return lf <= rf
		case "<":
			return lf < rf
		}
	}
	return false
}

func decisionBatchExpectationPassed(result *DecisionResult, expect map[string]any, diags *[]Diagnostic) bool {
	if len(expect) == 0 {
		return true
	}
	ok := true
	for key, want := range expect {
		var got any
		switch {
		case key == "effect":
			got = result.Effect
		case key == "allowed":
			got = result.Allowed
		case key == "policy_id":
			got = result.PolicyID
		case key == "reason_code":
			got = result.ReasonCode
		case key == "score":
			got = result.Score
		case key == "action":
			got = decisionActionsContain(result.Actions, scalarString(want))
		case key == "event":
			got = decisionActionsContain(result.Events, scalarString(want))
		case key == "obligation":
			got = decisionActionsContain(result.Obligations, scalarString(want))
		case key == "advice":
			got = decisionActionsContain(result.Advice, scalarString(want))
		case key == "matched_rules":
			got = traceRuleIDs(result.Explain, "matched")
		case key == "selected_rules":
			got = traceRuleIDs(result.Explain, "selected")
		case key == "skipped_rules":
			got = traceRuleIDs(result.Explain, "skipped")
		case key == "tags":
			got = result.Tags
		case strings.HasPrefix(key, "attributes."):
			got = lookupMapPath(result.Attributes, strings.TrimPrefix(key, "attributes."))
		case strings.HasPrefix(key, "metadata."):
			got = lookupMapPath(result.Metadata, strings.TrimPrefix(key, "metadata."))
		default:
			continue
		}
		if !equalLoose(got, want) {
			if b, ok := got.(bool); ok && scalarString(want) != "" {
				if b {
					continue
				}
			}
			ok = false
			*diags = append(*diags, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected %s %#v, got %#v", key, want, got)})
		}
	}
	return ok
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
	if want := scalarString(scenario.Expect["reason_code"]); want != "" && result.ReasonCode != want {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected reason_code %q", want)})
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
	if want := scalarString(scenario.Expect["obligation"]); want != "" && !decisionActionsContain(result.Obligations, want) {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected obligation %q", want)})
	}
	if want := scalarString(scenario.Expect["advice"]); want != "" && !decisionActionsContain(result.Advice, want) {
		out.Passed = false
		out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected advice %q", want)})
	}
	for key, want := range scenario.Expect {
		switch {
		case strings.HasPrefix(key, "attributes."):
			path := strings.TrimPrefix(key, "attributes.")
			if !equalLoose(lookupMapPath(result.Attributes, path), want) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected attributes.%s %#v", path, want)})
			}
		case strings.HasPrefix(key, "metadata."):
			path := strings.TrimPrefix(key, "metadata.")
			if !equalLoose(lookupMapPath(result.Metadata, path), want) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected metadata.%s %#v", path, want)})
			}
		case strings.HasPrefix(key, "outcome.attributes."):
			path := strings.TrimPrefix(key, "outcome.attributes.")
			if result.Outcome == nil || !equalLoose(lookupMapPath(result.Outcome.Attributes, path), want) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected outcome.attributes.%s %#v", path, want)})
			}
		case strings.HasPrefix(key, "outcome.metadata."):
			path := strings.TrimPrefix(key, "outcome.metadata.")
			if result.Outcome == nil || !equalLoose(lookupMapPath(result.Outcome.Metadata, path), want) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: fmt.Sprintf("expected outcome.metadata.%s %#v", path, want)})
			}
		case key == "matched_rules":
			if !equalStringSet(traceRuleIDs(result.Explain, "matched"), stringList(want)) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: "expected matched_rules"})
			}
		case key == "selected_rules":
			if !equalStringSet(traceRuleIDs(result.Explain, "selected"), stringList(want)) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: "expected selected_rules"})
			}
		case key == "skipped_rules":
			if !equalStringSet(traceRuleIDs(result.Explain, "skipped"), stringList(want)) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: "expected skipped_rules"})
			}
		case key == "tags":
			if !equalStringSet(result.Tags, stringList(want)) {
				out.Passed = false
				out.Diagnostics = append(out.Diagnostics, Diagnostic{Severity: "error", Message: "expected tags"})
			}
		}
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

func traceRuleIDs(trace []DecisionTrace, status string) []string {
	seen := map[string]bool{}
	var out []string
	for _, step := range trace {
		if step.Status == status && step.RuleID != "" && !seen[step.RuleID] {
			seen[step.RuleID] = true
			out = append(out, step.RuleID)
		}
	}
	sort.Strings(out)
	return out
}

func equalStringSet(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
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

func lookupMapPath(m map[string]any, path string) any {
	if len(m) == 0 || path == "" {
		return nil
	}
	return lookup(m, path)
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
