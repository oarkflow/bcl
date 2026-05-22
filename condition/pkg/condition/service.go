package condition

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/audit"
	"github.com/oarkflow/condition/pkg/storage"
)

type Service struct {
	store storage.Store
	cfg   Config
}

func NewService(store storage.Store, cfg Config) *Service {
	if store == nil {
		store = storage.NewMemoryStore()
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 5 * time.Second
	}
	if cfg.MaxRequestBytes == 0 {
		cfg.MaxRequestBytes = 1 << 20
	}
	return &Service{store: store, cfg: cfg}
}

func (s *Service) Store() storage.Store { return s.store }

func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.cfg
}

func (s *Service) Publish(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	resp, err := s.PublishVersion(ctx, req)
	if err != nil {
		return resp, err
	}
	if _, err := s.Activate(ctx, resp.Definition.Name, resp.Definition.Version, resp.Definition.Environment); err != nil {
		return resp, err
	}
	return resp, nil
}

func (s *Service) PublishVersion(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	start := time.Now()
	report, program, source, err := s.validatePublish(ctx, ValidationRequest{
		Name: req.Name, Version: req.Version, Environment: req.Environment, Path: req.Path, Source: req.Source, BaseDir: req.BaseDir, RunTests: req.RunTests, Bundle: req.Bundle,
	})
	if err != nil {
		return nil, err
	}
	if !report.Publishable {
		err := fmt.Errorf("definition %q is not publishable", report.Name)
		envelope := s.audit(ctx, "publish_failed", report.Name, report.Version, report.Environment, report.Digest, req, report, start, nil)
		return &PublishResponse{Definition: storage.DefinitionRecord{Name: report.Name, Version: report.Version, Environment: report.Environment, Source: source, SourcePath: req.Path, Digest: report.Digest}, Tests: report.Tests, Gates: report.Gates, Audit: envelope}, err
	}
	name, version, env := report.Name, report.Version, report.Environment
	record := storage.DefinitionRecord{
		Name:        name,
		Version:     version,
		Environment: env,
		Source:      source,
		SourcePath:  req.Path,
		Digest:      audit.DigestBytes([]byte(source)),
		Program:     program,
		PublishedAt: time.Now(),
		Metadata:    req.Metadata,
	}
	if err := s.store.SaveDefinitionVersion(ctx, record); err != nil {
		return nil, err
	}
	envelope := s.audit(ctx, "publish_version", name, version, env, record.Digest, req, map[string]any{"published": true}, start, nil)
	return &PublishResponse{Definition: record, Tests: report.Tests, Gates: report.Gates, Audit: envelope}, nil
}

func (s *Service) ListDefinitions(ctx context.Context) ([]storage.DefinitionRecord, error) {
	return s.store.ListDefinitions(ctx)
}

func (s *Service) GetDefinition(ctx context.Context, name string) (storage.DefinitionRecord, error) {
	return s.store.GetDefinition(ctx, name)
}

func (s *Service) Validate(ctx context.Context, req ValidationRequest) (*ValidationReport, error) {
	report, _, _, err := s.validatePublish(ctx, req)
	if err != nil && report == nil {
		return nil, err
	}
	return report, err
}

func (s *Service) ListVersions(ctx context.Context, name, environment string) ([]storage.DefinitionRecord, error) {
	return s.store.ListDefinitionVersions(ctx, name, first(environment, s.cfg.Environment))
}

func (s *Service) Activate(ctx context.Context, name, version, environment string) (*ActivationResponse, error) {
	start := time.Now()
	env := first(environment, s.cfg.Environment)
	if err := s.store.ActivateDefinition(ctx, name, version, env); err != nil {
		return nil, err
	}
	record, err := s.store.GetActiveDefinition(ctx, name, env)
	if err != nil {
		return nil, err
	}
	envelope := s.audit(ctx, "activate", name, version, env, record.Digest, map[string]any{"name": name, "version": version, "environment": env}, map[string]any{"active": true}, start, nil)
	return &ActivationResponse{Definition: record, Audit: envelope}, nil
}

func (s *Service) Rollback(ctx context.Context, name, targetVersion, environment string) (*ActivationResponse, error) {
	start := time.Now()
	resp, err := s.Activate(ctx, name, targetVersion, environment)
	if err != nil {
		return nil, err
	}
	resp.Audit = s.audit(ctx, "rollback", name, targetVersion, first(environment, s.cfg.Environment), resp.Definition.Digest, map[string]any{"name": name, "target_version": targetVersion}, map[string]any{"active": true}, start, nil)
	return resp, nil
}

func (s *Service) Evaluate(ctx context.Context, definition string, req EvaluateRequest) (*EvaluateResponse, error) {
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, first(req.Environment, s.cfg.Environment))
	if err != nil {
		return nil, err
	}
	if req.Decision == "" {
		req.Decision = firstDecision(record.Program)
	}
	report, err := bcl.EvaluateDecisionPlatform(record.Program, bcl.DecisionPlatformRequest{
		Decision:        req.Decision,
		Input:           req.Input,
		Bundle:          req.Bundle,
		IncludeGates:    req.IncludeGates,
		Counterfactuals: req.Counterfactuals,
		IncludeFeatures: req.IncludeFeatures,
	}, &bcl.Options{AllowTime: true})
	if err != nil {
		envelope := s.audit(ctx, "evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		return &EvaluateResponse{Report: report, Audit: envelope}, err
	}
	trace := []string(nil)
	if report != nil && report.Decision != nil {
		trace = report.Decision.Trace
	}
	workflow := workflowFromDecision(report)
	envelope := s.audit(ctx, "evaluate", definition, record.Version, record.Environment, record.Digest, req, report, start, trace)
	return &EvaluateResponse{Report: report, Workflow: workflow, Audit: envelope}, nil
}

func (s *Service) Test(ctx context.Context, definition, bundle string) (*TestReport, error) {
	start := time.Now()
	record, err := s.store.GetDefinition(ctx, definition)
	if err != nil {
		return nil, err
	}
	report := s.runTests(record.Program, firstDecision(record.Program), bundle)
	envelope := s.audit(ctx, "test", definition, record.Version, record.Environment, record.Digest, map[string]any{"bundle": bundle}, report, start, nil)
	report.Audit = &envelope
	_ = s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: "test", Definition: definition, CreatedAt: time.Now(), Payload: map[string]any{"report": report}})
	if !report.Passed {
		return report, fmt.Errorf("definition %q tests failed", definition)
	}
	return report, nil
}

func (s *Service) Gates(ctx context.Context, definition, bundle string) (*bcl.DecisionGateReport, error) {
	start := time.Now()
	record, err := s.store.GetDefinition(ctx, definition)
	if err != nil {
		return nil, err
	}
	report, err := bcl.EvaluateDecisionGates(record.Program, bundle, &bcl.Options{AllowTime: true})
	_ = s.audit(ctx, "gates", definition, record.Version, record.Environment, record.Digest, map[string]any{"bundle": bundle}, report, start, nil)
	return report, err
}

func (s *Service) Simulate(ctx context.Context, definition string, req SimulationRequest) (*SimulationResponse, error) {
	return s.comparePrograms(ctx, "simulate", definition, req)
}

func (s *Service) Compare(ctx context.Context, definition string, req SimulationRequest) (*SimulationResponse, error) {
	return s.comparePrograms(ctx, "compare", definition, req)
}

func (s *Service) comparePrograms(ctx context.Context, operation, definition string, req SimulationRequest) (*SimulationResponse, error) {
	start := time.Now()
	base, err := s.store.GetDefinition(ctx, definition)
	if err != nil {
		return nil, err
	}
	candidate, err := s.candidateProgram(req)
	if err != nil {
		return nil, err
	}
	decision := first(req.Decision, firstDecision(base.Program))
	var compare *bcl.DecisionCompareReport
	if req.Dataset != "" {
		compare, err = bcl.CompareDecisionDataset(base.Program, candidate, decision, req.Dataset, &bcl.Options{AllowTime: true})
	} else {
		compare, err = bcl.CompareDecisionBatch(base.Program, candidate, decision, req.Cases, &bcl.Options{AllowTime: true})
	}
	if err != nil {
		envelope := s.audit(ctx, operation+"_failed", definition, base.Version, base.Environment, base.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		return &SimulationResponse{Compare: compare, Audit: envelope}, err
	}
	envelope := s.audit(ctx, operation, definition, base.Version, base.Environment, base.Digest, req, compare, start, nil)
	_ = s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: operation, Definition: definition, CreatedAt: time.Now(), Payload: map[string]any{"compare": compare}})
	return &SimulationResponse{Compare: compare, Audit: envelope}, nil
}

func (s *Service) Reload(ctx context.Context, req ReloadRequest) (*ReloadResponse, error) {
	start := time.Now()
	last, err := s.store.GetDefinition(ctx, req.Name)
	if err != nil {
		return nil, err
	}
	path := first(req.Path, last.SourcePath)
	resp, err := s.Publish(ctx, PublishRequest{Name: req.Name, Path: path, Version: last.Version, Environment: last.Environment, RunTests: req.RunTests, Metadata: last.Metadata})
	if err != nil {
		envelope := s.audit(ctx, "reload_failed", req.Name, last.Version, last.Environment, last.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		return &ReloadResponse{Reloaded: false, KeptLast: true, Publish: resp, Audit: envelope}, nil
	}
	envelope := s.audit(ctx, "reload", req.Name, resp.Definition.Version, resp.Definition.Environment, resp.Definition.Digest, req, map[string]any{"reloaded": true}, start, nil)
	return &ReloadResponse{Reloaded: true, Publish: resp, Audit: envelope}, nil
}

func (s *Service) ListAudits(ctx context.Context) ([]audit.Envelope, error) {
	return s.store.ListAudits(ctx)
}

func (s *Service) QueryAudits(ctx context.Context, opts storage.ListOptions) ([]audit.Envelope, error) {
	return s.store.ListAuditsQuery(ctx, opts)
}

func (s *Service) QueryReports(ctx context.Context, opts storage.ListOptions) ([]storage.ReportRecord, error) {
	return s.store.ListReportsQuery(ctx, opts)
}

func (s *Service) GetAudit(ctx context.Context, id string) (audit.Envelope, error) {
	return s.store.GetAudit(ctx, id)
}

func (s *Service) VerifyAudits(ctx context.Context) error {
	records, err := s.store.ListAudits(ctx)
	if err != nil {
		return err
	}
	return audit.VerifyChain(records)
}

func (s *Service) Ready(ctx context.Context) error {
	_, err := s.store.ListDefinitions(ctx)
	return err
}

func (s *Service) StartWorkflow(ctx context.Context, definition, workflowID string, input map[string]any) (*WorkflowRun, error) {
	start := time.Now()
	record, err := s.store.GetDefinition(ctx, definition)
	if err != nil {
		return nil, err
	}
	workflow, err := workflowDefinition(record.Program, workflowID)
	if err != nil {
		return nil, err
	}
	stage := first(workflow.Start, firstWorkflowStage(workflow))
	run := WorkflowRun{
		ID:          newID("workflow"),
		Definition:  definition,
		Version:     record.Version,
		Environment: record.Environment,
		WorkflowID:  workflow.ID,
		Stage:       stage,
		Status:      "running",
		Input:       input,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	applyWorkflowStage(&run, workflow, input)
	envelope := s.audit(ctx, "workflow_start", definition, record.Version, record.Environment, record.Digest, input, run, start, nil)
	run.Audit = &envelope
	if err := s.store.SaveWorkflowRun(ctx, workflowRunRecord(run)); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Service) AdvanceWorkflow(ctx context.Context, runID string, input map[string]any) (*WorkflowRun, error) {
	start := time.Now()
	record, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	definition, err := s.store.GetDefinition(ctx, record.Definition)
	if err != nil {
		return nil, err
	}
	workflow, err := workflowDefinition(definition.Program, record.WorkflowID)
	if err != nil {
		return nil, err
	}
	run := workflowRunFromRecord(record)
	if len(input) > 0 {
		run.Input = input
	}
	advanceWorkflowStage(&run, workflow, run.Input)
	run.UpdatedAt = time.Now()
	envelope := s.audit(ctx, "workflow_advance", run.Definition, run.Version, run.Environment, definition.Digest, input, run, start, nil)
	run.Audit = &envelope
	if err := s.store.SaveWorkflowRun(ctx, workflowRunRecord(run)); err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Service) GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	record, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	run := workflowRunFromRecord(record)
	return &run, nil
}

func (s *Service) ListWorkflowRuns(ctx context.Context, opts storage.ListOptions) ([]storage.WorkflowRunRecord, error) {
	return s.store.ListWorkflowRuns(ctx, opts)
}

func (s *Service) runTests(program *bcl.DecisionProgram, defaultDecision, bundle string) *TestReport {
	report := &TestReport{Passed: true}
	for _, test := range program.Tests {
		decision := first(test.Decision, defaultDecision)
		result, err := bcl.EvaluateDecisionScenario(program, &bcl.DecisionScenario{
			Name: test.Name, Decision: decision, Input: test.Input, Expect: test.Expect,
		}, &bcl.Options{AllowTime: true})
		if err != nil {
			report.Passed = false
			report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
			continue
		}
		if !result.Passed {
			report.Passed = false
		}
		report.Scenarios = append(report.Scenarios, *result)
	}
	if bundle != "" {
		gates, err := bcl.EvaluateDecisionGates(program, bundle, &bcl.Options{AllowTime: true})
		report.Gates = gates
		if err != nil {
			report.Passed = false
			report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		}
		if gates != nil && !gates.Passed {
			report.Passed = false
		}
	}
	return report
}

func (s *Service) validatePublish(ctx context.Context, req ValidationRequest) (*ValidationReport, *bcl.DecisionProgram, string, error) {
	source, baseDir, err := sourceFromValidation(req)
	report := &ValidationReport{
		Name:        first(req.Name, "default"),
		Version:     first(req.Version, "1"),
		Environment: first(req.Environment, s.cfg.Environment),
	}
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		return report, nil, "", err
	}
	report.Digest = audit.DigestBytes([]byte(source))
	program, err := compileSource(source, req.Path, baseDir)
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		return report, nil, source, err
	}
	if program != nil {
		report.Name = first(req.Name, firstModule(program), report.Name)
		report.Features = bcl.InspectDecisionPlatform(program)
		for id := range program.Decisions {
			report.Decisions = append(report.Decisions, id)
		}
		sort.Strings(report.Decisions)
		report.Diagnostics = append(report.Diagnostics, program.Diagnostics...)
	}
	if program == nil || len(program.Decisions) == 0 {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: "definition does not contain any decisions"})
	}
	if req.RunTests && program != nil {
		report.Tests = s.runTests(program, firstDecision(program), req.Bundle)
		report.Diagnostics = append(report.Diagnostics, report.Tests.Diagnostics...)
		report.Gates = report.Tests.Gates
	}
	report.Valid = !hasErrorDiagnostics(report.Diagnostics)
	report.Publishable = report.Valid && len(report.Decisions) > 0 && (report.Tests == nil || report.Tests.Passed)
	_ = ctx
	return report, program, source, nil
}

func (s *Service) candidateProgram(req SimulationRequest) (*bcl.DecisionProgram, error) {
	switch {
	case req.CandidatePath != "":
		return bcl.CompileDecisionFile(req.CandidatePath, &bcl.Options{AllowTime: true})
	case req.CandidateSource != "":
		return compileSource(req.CandidateSource, "", req.CandidateBaseDir)
	default:
		return nil, fmt.Errorf("candidate_source or candidate_path is required")
	}
}

func sourceFromValidation(req ValidationRequest) (source, baseDir string, err error) {
	if req.Source != "" {
		return req.Source, req.BaseDir, nil
	}
	if req.Path == "" {
		return "", "", fmt.Errorf("source or path is required")
	}
	payload, err := os.ReadFile(req.Path)
	if err != nil {
		return "", "", err
	}
	return string(payload), filepath.Dir(req.Path), nil
}

func (s *Service) audit(ctx context.Context, operation, definition, version, env, digest string, request, result any, start time.Time, trace []string) audit.Envelope {
	completed := time.Now()
	previous, _ := s.store.LastAuditHash(ctx)
	envelope := audit.Seal(audit.Envelope{
		ID:               newID(operation),
		Operation:        operation,
		Definition:       definition,
		Version:          version,
		Environment:      env,
		Subject:          SubjectFromContext(ctx),
		DefinitionDigest: digest,
		RequestHash:      audit.Fingerprint(request),
		ResultHash:       audit.Fingerprint(result),
		StartedAt:        start,
		CompletedAt:      completed,
		DurationMS:       completed.Sub(start).Milliseconds(),
		TraceSummary:     trimTrace(trace),
	}, previous)
	_ = s.store.AppendAudit(ctx, envelope)
	return envelope
}

func sourceFromPublish(req PublishRequest) (source, baseDir string, err error) {
	if req.Source != "" {
		return req.Source, req.BaseDir, nil
	}
	if req.Path == "" {
		return "", "", fmt.Errorf("source or path is required")
	}
	payload, err := os.ReadFile(req.Path)
	if err != nil {
		return "", "", err
	}
	return string(payload), filepath.Dir(req.Path), nil
}

func hasErrorDiagnostics(diags []bcl.Diagnostic) bool {
	for _, diag := range diags {
		if diag.Severity == "error" {
			return true
		}
	}
	return false
}

func compileSource(source, path, baseDir string) (*bcl.DecisionProgram, error) {
	if path != "" {
		doc, err := bcl.ParsePath(path)
		if err != nil {
			return nil, err
		}
		program, err := bcl.CompileDecisionDocument(doc, &bcl.Options{AllowTime: true, BaseDir: filepath.Dir(path), ResolveImports: true, ResolveModules: true})
		attachWorkflowBlocks(program, doc)
		return program, err
	}
	doc, err := bcl.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	program, err := bcl.CompileDecisionDocument(doc, &bcl.Options{AllowTime: true, BaseDir: baseDir, ResolveImports: true, ResolveModules: true})
	attachWorkflowBlocks(program, doc)
	return program, err
}

func attachWorkflowBlocks(program *bcl.DecisionProgram, doc *bcl.Document) {
	if program == nil || doc == nil {
		return
	}
	payload, err := json.Marshal(doc.Items)
	if err != nil {
		return
	}
	var items []any
	if err := json.Unmarshal(payload, &items); err != nil {
		return
	}
	var workflows []map[string]any
	collectBlocks(items, "workflow", &workflows)
	if len(workflows) == 0 {
		return
	}
	if program.Governance == nil {
		program.Governance = map[string]any{}
	}
	program.Governance["_condition_workflows"] = workflows
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstModule(program *bcl.DecisionProgram) string {
	if program == nil || len(program.Modules) == 0 {
		return ""
	}
	return program.Modules[0]
}

func firstDecision(program *bcl.DecisionProgram) string {
	if program == nil {
		return ""
	}
	keys := make([]string, 0, len(program.Decisions))
	for k := range program.Decisions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func trimTrace(trace []string) []string {
	if len(trace) > 20 {
		return append([]string(nil), trace[:20]...)
	}
	return append([]string(nil), trace...)
}

func workflowFromDecision(report *bcl.DecisionPlatformReport) *WorkflowResult {
	if report == nil || report.Decision == nil {
		return nil
	}
	attrs := report.Decision.Attributes
	if len(attrs) == 0 {
		return nil
	}
	stage := fmt.Sprint(attrs["stage"])
	queue := fmt.Sprint(attrs["queue"])
	if stage == "<nil>" && queue == "<nil>" {
		return nil
	}
	assignment := map[string]any{}
	if queue != "" && queue != "<nil>" {
		assignment["queue"] = queue
	}
	if role := fmt.Sprint(attrs["role"]); role != "" && role != "<nil>" {
		assignment["role"] = role
	}
	return &WorkflowResult{Stage: stage, Assignment: assignment}
}

func newID(prefix string) string {
	return fmt.Sprintf("%s-%d", strings.ReplaceAll(prefix, "_", "-"), time.Now().UnixNano())
}

func ToMap(v any) map[string]any {
	payload, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(payload, &out)
	return out
}
