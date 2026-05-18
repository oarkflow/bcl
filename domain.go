package bcl

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type DomainKind string

const (
	DomainBCL     DomainKind = "bcl"
	DomainPolicy  DomainKind = "policy"
	DomainRuntime DomainKind = "runtime"
	DomainConnect DomainKind = "connector"
	DomainAction  DomainKind = "action"
	DomainSchema  DomainKind = "schema"
)

type DomainFile struct {
	Path       string      `json:"path"`
	Kind       DomainKind  `json:"kind"`
	Document   *Document   `json:"-"`
	Normalized *Normalized `json:"normalized,omitempty"`
}

type DomainProgram struct {
	Files       []DomainFile        `json:"files,omitempty"`
	Runtime     map[string]any      `json:"runtime,omitempty"`
	Connectors  []map[string]any    `json:"connectors,omitempty"`
	Actions     []map[string]any    `json:"actions,omitempty"`
	Schemas     map[string]any      `json:"schemas,omitempty"`
	Policies    []PolicyIR          `json:"policies,omitempty"`
	PolicyPlan  *PolicyPlan         `json:"policy_plan,omitempty"`
	Diagnostics []Diagnostic        `json:"diagnostics,omitempty"`
	Normalized  map[string][]string `json:"normalized_files,omitempty"`
}

type PolicyIR struct {
	Kind      string         `json:"kind"`
	ID        string         `json:"id"`
	Tenant    string         `json:"tenant,omitempty"`
	Effect    string         `json:"effect"`
	Priority  int64          `json:"priority,omitempty"`
	Match     PolicyMatch    `json:"match,omitempty"`
	Condition map[string]any `json:"condition,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Span      Span           `json:"span,omitempty"`
}

type PolicyMatch struct {
	Actions   []string `json:"actions,omitempty"`
	Resources []string `json:"resources,omitempty"`
}

type PolicyPlan struct {
	Strategy      string                                 `json:"strategy"`
	DefaultEffect string                                 `json:"default_effect"`
	Policies      []PolicyIR                             `json:"policies"`
	Index         map[string]map[string]map[string][]int `json:"index"`
}

type PolicyDecision struct {
	Effect     string    `json:"effect"`
	PolicyID   string    `json:"policy_id,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	Evaluated  int       `json:"evaluated"`
	Candidates int       `json:"candidates"`
	Trace      []string  `json:"trace,omitempty"`
	Policy     *PolicyIR `json:"policy,omitempty"`
}

func DetectDomain(path string) DomainKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pcl":
		return DomainPolicy
	case ".rcl":
		return DomainRuntime
	case ".ccl":
		return DomainConnect
	case ".acl":
		return DomainAction
	case ".schema":
		return DomainSchema
	default:
		return DomainBCL
	}
}

func CompileDomainFile(path string, opts *Options) (*DomainFile, error) {
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
	n, err := Compile(doc, opts)
	return &DomainFile{Path: path, Kind: DetectDomain(path), Document: doc, Normalized: n}, err
}

func CompileDomainDir(dir string, opts *Options) (*DomainProgram, error) {
	paths, err := domainFiles(dir)
	if err != nil {
		return nil, err
	}
	prog := &DomainProgram{Schemas: map[string]any{}, Normalized: map[string][]string{}}
	for _, path := range paths {
		df, err := CompileDomainFile(path, cloneOptions(opts))
		if err != nil {
			if e, ok := err.(ErrorList); ok {
				prog.Diagnostics = append(prog.Diagnostics, e...)
			} else {
				prog.Diagnostics = append(prog.Diagnostics, Diagnostic{Severity: "error", Message: err.Error()})
			}
			continue
		}
		prog.Files = append(prog.Files, *df)
		prog.Normalized[string(df.Kind)] = append(prog.Normalized[string(df.Kind)], df.Path)
		compileDomainFileInto(prog, df)
	}
	prog.Diagnostics = append(prog.Diagnostics, validateDomainSchemas(prog)...)
	prog.PolicyPlan = BuildPolicyPlan(prog.Policies, prog.Runtime)
	if len(prog.Diagnostics) > 0 {
		return prog, ErrorList(prog.Diagnostics)
	}
	return prog, nil
}

func validateDomainSchemas(prog *DomainProgram) []Diagnostic {
	schemas := map[string]*SchemaDecl{}
	aliases := map[string]string{}
	for _, file := range prog.Files {
		for _, n := range file.Document.Items {
			switch x := n.(type) {
			case *SchemaDecl:
				schemas[x.Name] = x
			case *TypeDecl:
				aliases[x.Name] = x.Type
			}
		}
	}
	if len(schemas) == 0 {
		return nil
	}
	var diags []Diagnostic
	for _, file := range prog.Files {
		if file.Kind == DomainSchema {
			continue
		}
		validateSchemas(file.Document.Items, schemas, aliases, &diags)
	}
	return diags
}

func domainFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".bcl", ".pcl", ".rcl", ".ccl", ".acl", ".schema":
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func cloneOptions(opts *Options) *Options {
	if opts == nil {
		return nil
	}
	cp := *opts
	return &cp
}

func compileDomainFileInto(prog *DomainProgram, df *DomainFile) {
	n := df.Normalized
	switch df.Kind {
	case DomainRuntime:
		if prog.Runtime == nil {
			prog.Runtime = map[string]any{}
		}
		mergeMap(prog.Runtime, n.Body)
		for _, b := range n.Blocks {
			if typ := stringValue(b["type"]); typ == "engine" || typ == "runtime" || typ == "evaluation" {
				if body, ok := b["body"].(map[string]any); ok {
					prog.Runtime[typ] = body
				}
			}
		}
	case DomainConnect:
		prog.Connectors = append(prog.Connectors, n.Blocks...)
	case DomainAction:
		prog.Actions = append(prog.Actions, n.Blocks...)
	case DomainSchema:
		for k, v := range n.Schemas {
			prog.Schemas[k] = v
		}
	case DomainPolicy:
		for _, p := range policiesFromNormalized(n) {
			prog.Policies = append(prog.Policies, p)
		}
	default:
		for _, p := range policiesFromNormalized(n) {
			prog.Policies = append(prog.Policies, p)
		}
	}
}

func policiesFromNormalized(n *Normalized) []PolicyIR {
	out := make([]PolicyIR, 0)
	for _, block := range n.Blocks {
		if stringValue(block["type"]) != "policy" {
			continue
		}
		body, _ := block["body"].(map[string]any)
		p := PolicyIR{
			Kind:     "policy",
			ID:       stringValue(block["id"]),
			Tenant:   stringValue(body["tenant"]),
			Effect:   stringValue(body["effect"]),
			Priority: intValue(body["priority"]),
			Reason:   firstNonEmpty(stringValue(body["reason"]), stringValue(body["reason_code"])),
		}
		if p.Effect == "" {
			p.Effect = "allow"
		}
		if match, ok := body["match"].(map[string]any); ok {
			p.Match.Actions = stringList(match["actions"])
			p.Match.Resources = stringList(match["resources"])
		} else {
			p.Match.Actions = stringList(body["actions"])
			p.Match.Resources = stringList(body["resources"])
		}
		if cond, ok := body["when"].(map[string]any); ok {
			p.Condition = cond
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].ID < out[j].ID
		}
		return out[i].Priority > out[j].Priority
	})
	return out
}

func BuildPolicyPlan(policies []PolicyIR, runtime map[string]any) *PolicyPlan {
	plan := &PolicyPlan{
		Strategy:      "deny_overrides",
		DefaultEffect: "deny",
		Policies:      append([]PolicyIR(nil), policies...),
		Index:         map[string]map[string]map[string][]int{},
	}
	if engine, ok := runtime["engine"].(map[string]any); ok {
		if v := stringValue(engine["evaluation_strategy"]); v != "" {
			plan.Strategy = v
		}
		if v := stringValue(engine["default_decision"]); v != "" {
			plan.DefaultEffect = v
		}
	}
	sort.SliceStable(plan.Policies, func(i, j int) bool {
		if plan.Policies[i].Priority == plan.Policies[j].Priority {
			return plan.Policies[i].ID < plan.Policies[j].ID
		}
		return plan.Policies[i].Priority > plan.Policies[j].Priority
	})
	for i, p := range plan.Policies {
		tenants := []string{wildcard(p.Tenant)}
		actions := wildcardList(p.Match.Actions)
		resources := resourceTypes(p.Match.Resources)
		for _, tenant := range tenants {
			for _, resource := range resources {
				for _, action := range actions {
					addPolicyIndex(plan.Index, tenant, resource, action, i)
				}
			}
		}
	}
	return plan
}

func (p *PolicyPlan) Evaluate(input map[string]any, opts *Options) PolicyDecision {
	return EvaluatePolicyPlan(p, input, opts)
}

func EvaluatePolicyPlan(plan *PolicyPlan, input map[string]any, opts *Options) PolicyDecision {
	if plan == nil {
		return PolicyDecision{Effect: "deny"}
	}
	tenant := firstNonEmpty(pathString(input, "tenant.id"), pathString(input, "tenant"), pathString(input, "subject.tenant"))
	action := firstNonEmpty(pathString(input, "action"), pathString(input, "request.action"))
	resource := firstNonEmpty(pathString(input, "resource"), pathString(input, "request.resource"))
	resourceType := resourceType(resource)
	candidates := candidatePolicyIndexes(plan, tenant, resourceType, action)
	decision := PolicyDecision{Effect: plan.DefaultEffect, Candidates: len(candidates)}
	var fallback *PolicyIR
	for _, idx := range candidates {
		p := &plan.Policies[idx]
		if !policyMatches(*p, tenant, action, resource) {
			continue
		}
		decision.Evaluated++
		ok := true
		var err error
		if p.Condition != nil {
			ok, err = evalNormalizedCondition(p.Condition, input, opts)
			if err != nil {
				decision.Trace = append(decision.Trace, p.ID+": condition error: "+err.Error())
				continue
			}
		}
		if !ok {
			decision.Trace = append(decision.Trace, p.ID+": condition false")
			continue
		}
		decision.Trace = append(decision.Trace, p.ID+": matched")
		switch plan.Strategy {
		case "first_match", "highest_priority":
			return policyDecision(*p, decision)
		case "allow_overrides":
			if p.Effect == "allow" {
				return policyDecision(*p, decision)
			}
		case "deny_overrides":
			if p.Effect == "deny" {
				return policyDecision(*p, decision)
			}
		}
		if fallback == nil {
			fallback = p
		}
	}
	if fallback != nil {
		return policyDecision(*fallback, decision)
	}
	return decision
}

func policyDecision(p PolicyIR, d PolicyDecision) PolicyDecision {
	d.Effect = p.Effect
	d.PolicyID = p.ID
	d.Reason = p.Reason
	cp := p
	d.Policy = &cp
	return d
}

func addPolicyIndex(index map[string]map[string]map[string][]int, tenant, resource, action string, idx int) {
	if index[tenant] == nil {
		index[tenant] = map[string]map[string][]int{}
	}
	if index[tenant][resource] == nil {
		index[tenant][resource] = map[string][]int{}
	}
	index[tenant][resource][action] = append(index[tenant][resource][action], idx)
}

func candidatePolicyIndexes(plan *PolicyPlan, tenant, resourceType, action string) []int {
	var out []int
	seen := map[int]bool{}
	for _, tk := range []string{tenant, "*"} {
		for _, rk := range []string{resourceType, "*"} {
			for _, ak := range []string{action, "*"} {
				for _, idx := range plan.Index[tk][rk][ak] {
					if !seen[idx] {
						seen[idx] = true
						out = append(out, idx)
					}
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := plan.Policies[out[i]], plan.Policies[out[j]]
		if a.Priority == b.Priority {
			return a.ID < b.ID
		}
		return a.Priority > b.Priority
	})
	return out
}

func policyMatches(p PolicyIR, tenant, action, resource string) bool {
	return patternMatches(wildcard(p.Tenant), tenant) &&
		anyPatternMatches(p.Match.Actions, action) &&
		anyPatternMatches(p.Match.Resources, resource)
}

func anyPatternMatches(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if patternMatches(p, value) {
			return true
		}
	}
	return false
}

func patternMatches(pattern, value string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

func wildcard(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

func wildcardList(xs []string) []string {
	if len(xs) == 0 {
		return []string{"*"}
	}
	return xs
}

func resourceTypes(patterns []string) []string {
	if len(patterns) == 0 {
		return []string{"*"}
	}
	out := make([]string, 0, len(patterns))
	seen := map[string]bool{}
	for _, p := range patterns {
		t := resourceType(p)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func resourceType(resource string) string {
	if resource == "" || resource == "*" {
		return "*"
	}
	if i := strings.IndexByte(resource, ':'); i >= 0 {
		if i == 0 {
			return "*"
		}
		return resource[:i]
	}
	return resource
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func pathString(input map[string]any, path string) string {
	return stringValue(lookup(input, path))
}

func stringList(v any) []string {
	xs, ok := v.([]any)
	if !ok {
		if s := stringValue(v); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		if s := stringValue(x); s != "" {
			out = append(out, s)
		}
	}
	return out
}
