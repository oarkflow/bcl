package condition

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/audit"
	"github.com/oarkflow/condition/pkg/storage"
)

type Service struct {
	store   storage.Store
	cfg     Config
	auditMu sync.Mutex
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
	if s.cfg.RequireActivationApproval && resp != nil && !metadataBool(resp.Definition.Metadata, "approved") {
		return resp, nil
	}
	if _, err := s.Activate(ctx, resp.Definition.Name, resp.Definition.Version, resp.Definition.Environment); err != nil {
		return resp, err
	}
	return resp, nil
}

func (s *Service) PublishVersion(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	start := time.Now()
	report, program, source, err := s.validatePublish(ctx, ValidationRequest{
		Name: req.Name, Version: req.Version, Environment: req.Environment, Path: req.Path, Source: req.Source, BaseDir: req.BaseDir, RunTests: req.RunTests, Strict: req.Strict, RequireTests: req.RequireTests, Bundle: req.Bundle,
	})
	if err != nil {
		return nil, err
	}
	if !report.Publishable {
		err := fmt.Errorf("definition %q is not publishable", report.Name)
		envelope, auditErr := s.audit(ctx, "publish_failed", report.Name, report.Version, report.Environment, report.Digest, req, report, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
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
	if s.cfg.RequireActivationApproval && !metadataBool(record.Metadata, "approved") {
		record.Metadata = copyMetadata(record.Metadata)
		record.Metadata["activation_pending"] = true
	}
	if err := s.store.SaveDefinitionVersion(ctx, record); err != nil {
		return nil, err
	}
	envelope, err := s.audit(ctx, "publish_version", name, version, env, record.Digest, req, map[string]any{"published": true}, start, nil)
	if err != nil {
		return nil, err
	}
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
	record, err := s.store.GetDefinitionVersion(ctx, name, version, env)
	if err != nil {
		envelope, auditErr := s.audit(ctx, "activate_failed", name, version, env, "", map[string]any{"name": name, "version": version, "environment": env}, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ActivationResponse{Audit: envelope}, err
	}
	if s.cfg.RequireActivationApproval && !metadataBool(record.Metadata, "approved") {
		err := fmt.Errorf("definition %q version %q requires approval before activation", name, version)
		envelope, auditErr := s.audit(ctx, "activate_failed", name, version, env, record.Digest, map[string]any{"name": name, "version": version, "environment": env}, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ActivationResponse{Definition: record, Audit: envelope}, err
	}
	if metadataBool(record.Metadata, "activation_pending") {
		record.Metadata = copyMetadata(record.Metadata)
		record.Metadata["activation_pending"] = false
		if err := s.store.SaveDefinitionVersion(ctx, record); err != nil {
			return nil, err
		}
	}
	if err := s.store.ActivateDefinition(ctx, name, version, env); err != nil {
		envelope, auditErr := s.audit(ctx, "activate_failed", name, version, env, record.Digest, map[string]any{"name": name, "version": version, "environment": env}, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ActivationResponse{Definition: record, Audit: envelope}, err
	}
	record, err = s.store.GetActiveDefinition(ctx, name, env)
	if err != nil {
		return nil, err
	}
	envelope, err := s.audit(ctx, "activate", name, version, env, record.Digest, map[string]any{"name": name, "version": version, "environment": env}, map[string]any{"active": true}, start, nil)
	if err != nil {
		return nil, err
	}
	return &ActivationResponse{Definition: record, Audit: envelope}, nil
}

func (s *Service) Approve(ctx context.Context, name, version string, req ApprovalRequest) (*LifecycleResponse, error) {
	start := time.Now()
	env := first(req.Environment, s.cfg.Environment)
	record, err := s.store.GetDefinitionVersion(ctx, name, version, env)
	if err != nil {
		return nil, err
	}
	record.Metadata = copyMetadata(record.Metadata)
	record.Metadata["approved"] = true
	record.Metadata["approved_by"] = first(req.ApprovedBy, SubjectFromContext(ctx), "system")
	record.Metadata["approval_reason"] = req.Reason
	record.Metadata["approved_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.store.SaveDefinitionVersion(ctx, record); err != nil {
		return nil, err
	}
	envelope, err := s.audit(ctx, "approve", name, version, env, record.Digest, req, map[string]any{"approved": true}, start, nil)
	if err != nil {
		return nil, err
	}
	return &LifecycleResponse{Definition: record, Audit: envelope}, nil
}

func (s *Service) Disable(ctx context.Context, name string, req DisableRequest) (*LifecycleResponse, error) {
	return s.setDisabled(ctx, name, req, true)
}

func (s *Service) Enable(ctx context.Context, name string, req DisableRequest) (*LifecycleResponse, error) {
	return s.setDisabled(ctx, name, req, false)
}

func (s *Service) setDisabled(ctx context.Context, name string, req DisableRequest, disabled bool) (*LifecycleResponse, error) {
	start := time.Now()
	env := first(req.Environment, s.cfg.Environment)
	var record storage.DefinitionRecord
	var err error
	if req.Version != "" {
		record, err = s.store.GetDefinitionVersion(ctx, name, req.Version, env)
	} else {
		record, err = s.store.GetActiveDefinition(ctx, name, env)
	}
	if err != nil {
		return nil, err
	}
	record.Metadata = copyMetadata(record.Metadata)
	record.Metadata["disabled"] = disabled
	record.Metadata["disabled_reason"] = req.Reason
	record.Metadata["disabled_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if !disabled {
		record.Metadata["enabled_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := s.store.SaveDefinitionVersion(ctx, record); err != nil {
		return nil, err
	}
	if err := s.store.ActivateDefinition(ctx, record.Name, record.Version, record.Environment); err != nil {
		return nil, err
	}
	operation := "disable"
	if !disabled {
		operation = "enable"
	}
	envelope, err := s.audit(ctx, operation, name, record.Version, env, record.Digest, req, map[string]any{"disabled": disabled}, start, nil)
	if err != nil {
		return nil, err
	}
	return &LifecycleResponse{Definition: record, Audit: envelope}, nil
}

func (s *Service) Rollback(ctx context.Context, name, targetVersion, environment string) (*ActivationResponse, error) {
	start := time.Now()
	resp, err := s.Activate(ctx, name, targetVersion, environment)
	if err != nil {
		return nil, err
	}
	envelope, err := s.audit(ctx, "rollback", name, targetVersion, first(environment, s.cfg.Environment), resp.Definition.Digest, map[string]any{"name": name, "target_version": targetVersion}, map[string]any{"active": true}, start, nil)
	if err != nil {
		return nil, err
	}
	resp.Audit = envelope
	return resp, nil
}

func (s *Service) Evaluate(ctx context.Context, definition string, req EvaluateRequest) (*EvaluateResponse, error) {
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, first(req.Environment, s.cfg.Environment))
	if err != nil {
		return nil, err
	}
	if metadataBool(record.Metadata, "disabled") {
		err := fmt.Errorf("definition %q version %q is disabled", definition, record.Version)
		envelope, auditErr := s.audit(ctx, "evaluate_disabled", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error(), "disabled": true}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &EvaluateResponse{Audit: envelope}, err
	}
	if metadataBool(record.Metadata, "activation_pending") {
		err := fmt.Errorf("definition %q version %q is not activated", definition, record.Version)
		envelope, auditErr := s.audit(ctx, "evaluate_not_active", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error(), "activation_pending": true}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &EvaluateResponse{Audit: envelope}, err
	}
	if req.Decision == "" {
		req.Decision = firstDecision(record.Program)
	}
	input, runtimeContext, runtimeSession := runtimeInputFromContext(ctx, req.Input)
	req.Input = input
	report, err := bcl.EvaluateDecisionPlatform(record.Program, bcl.DecisionPlatformRequest{
		Decision:        req.Decision,
		Input:           input,
		Bundle:          req.Bundle,
		IncludeGates:    req.IncludeGates,
		Counterfactuals: req.Counterfactuals,
		IncludeFeatures: req.IncludeFeatures,
		Strict:          s.strictEvaluation(req.Strict),
	}, &bcl.Options{AllowTime: true, Context: runtimeContext, Session: runtimeSession})
	if err != nil {
		envelope, auditErr := s.audit(ctx, "evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &EvaluateResponse{Report: report, Audit: envelope}, err
	}
	shadow, shadowErr := s.shadowEvaluate(ctx, req, record.Program)
	if shadowErr != nil {
		envelope, auditErr := s.audit(ctx, "evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": shadowErr.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &EvaluateResponse{Report: report, Audit: envelope}, shadowErr
	}
	trace := []string(nil)
	if report != nil && report.Decision != nil {
		trace = report.Decision.Trace
	}
	workflow := workflowFromDecision(report)
	result := map[string]any{"report": report}
	if shadow != nil {
		result["shadow"] = shadow
	}
	envelope, err := s.audit(ctx, "evaluate", definition, record.Version, record.Environment, record.Digest, req, result, start, trace)
	if err != nil {
		return nil, err
	}
	return &EvaluateResponse{Report: report, Shadow: shadow, Workflow: workflow, Audit: envelope}, nil
}

func (s *Service) Test(ctx context.Context, definition, bundle string) (*TestReport, error) {
	start := time.Now()
	record, err := s.store.GetDefinition(ctx, definition)
	if err != nil {
		return nil, err
	}
	report := s.runTests(record.Program, firstDecision(record.Program), bundle, false)
	envelope, err := s.audit(ctx, "test", definition, record.Version, record.Environment, record.Digest, map[string]any{"bundle": bundle}, report, start, nil)
	if err != nil {
		return nil, err
	}
	report.Audit = &envelope
	if err := s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: "test", Definition: definition, CreatedAt: time.Now(), Payload: map[string]any{"report": report}}); err != nil {
		return nil, err
	}
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
	if _, auditErr := s.audit(ctx, "gates", definition, record.Version, record.Environment, record.Digest, map[string]any{"bundle": bundle}, report, start, nil); auditErr != nil {
		return nil, auditErr
	}
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
		envelope, auditErr := s.audit(ctx, operation+"_failed", definition, base.Version, base.Environment, base.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &SimulationResponse{Compare: compare, Audit: envelope}, err
	}
	envelope, err := s.audit(ctx, operation, definition, base.Version, base.Environment, base.Digest, req, compare, start, nil)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: operation, Definition: definition, CreatedAt: time.Now(), Payload: map[string]any{"compare": compare}}); err != nil {
		return nil, err
	}
	return &SimulationResponse{Compare: compare, Audit: envelope}, nil
}

func (s *Service) shadowEvaluate(ctx context.Context, req EvaluateRequest, base *bcl.DecisionProgram) (*bcl.DecisionCompareReport, error) {
	if req.ShadowCandidateSource == "" && req.ShadowCandidatePath == "" {
		return nil, nil
	}
	candidate, err := s.candidateProgram(SimulationRequest{
		CandidateSource:  req.ShadowCandidateSource,
		CandidatePath:    req.ShadowCandidatePath,
		CandidateBaseDir: req.ShadowCandidateBaseDir,
	})
	if err != nil {
		return nil, err
	}
	return bcl.CompareDecisionBatch(base, candidate, req.Decision, []bcl.DecisionBatchCase{{
		ID:    "shadow",
		Input: req.Input,
	}}, &bcl.Options{AllowTime: true, Context: ContextFactsFromContext(ctx), Session: SessionFromContext(ctx)})
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
		envelope, auditErr := s.audit(ctx, "reload_failed", req.Name, last.Version, last.Environment, last.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ReloadResponse{Reloaded: false, KeptLast: true, Publish: resp, Audit: envelope}, nil
	}
	envelope, err := s.audit(ctx, "reload", req.Name, resp.Definition.Version, resp.Definition.Environment, resp.Definition.Digest, req, map[string]any{"reloaded": true}, start, nil)
	if err != nil {
		return nil, err
	}
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
	if err != nil {
		return err
	}
	return s.VerifyAudits(ctx)
}

func runtimeInputFromContext(ctx context.Context, input map[string]any) (map[string]any, map[string]any, map[string]any) {
	merged := cloneMap(input)
	if merged == nil {
		merged = map[string]any{}
	}
	requestContext, _ := merged["context"].(map[string]any)
	requestSession, _ := merged["session"].(map[string]any)
	runtimeContext := mergeMaps(requestContext, ContextFactsFromContext(ctx))
	runtimeSession := mergeMaps(requestSession, SessionFromContext(ctx))
	if len(runtimeContext) > 0 {
		merged["context"] = runtimeContext
	}
	if len(runtimeSession) > 0 {
		merged["session"] = runtimeSession
	}
	return merged, runtimeContext, runtimeSession
}

func (s *Service) ProductionReadiness(ctx context.Context) ProductionReadinessReport {
	report := ProductionReadinessReport{
		Ready:       true,
		Environment: s.cfg.Environment,
		Checks: map[string]bool{
			"store_available":              false,
			"audit_chain_valid":            false,
			"strict_validation_enabled":    s.cfg.StrictValidation,
			"strict_evaluation_enabled":    s.cfg.StrictEvaluation,
			"tests_or_gates_required":      s.cfg.RequireTests,
			"activation_approval_required": s.cfg.RequireActivationApproval,
			"request_timeout_configured":   s.cfg.RequestTimeout > 0,
			"max_request_bytes_configured": s.cfg.MaxRequestBytes > 0,
		},
	}
	if _, err := s.store.ListDefinitions(ctx); err == nil {
		report.Checks["store_available"] = true
	}
	if err := s.VerifyAudits(ctx); err == nil {
		report.Checks["audit_chain_valid"] = true
	}
	for name, ok := range report.Checks {
		if !ok {
			report.Ready = false
			report.Missing = append(report.Missing, name)
		}
	}
	sort.Strings(report.Missing)
	return report
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
	envelope, err := s.audit(ctx, "workflow_start", definition, record.Version, record.Environment, record.Digest, input, run, start, nil)
	if err != nil {
		return nil, err
	}
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
	envelope, err := s.audit(ctx, "workflow_advance", run.Definition, run.Version, run.Environment, definition.Digest, input, run, start, nil)
	if err != nil {
		return nil, err
	}
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

func (s *Service) runTests(program *bcl.DecisionProgram, defaultDecision, bundle string, requireTests bool) *TestReport {
	report := &TestReport{Passed: true}
	if requireTests && len(program.Tests) == 0 && bundle == "" {
		report.Passed = false
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: "definition requires at least one test or decision gate"})
		return report
	}
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
	strict := s.strictValidation(req.Strict)
	requireTests := s.requireTests(req.RequireTests)
	report := &ValidationReport{
		Name:          first(req.Name, "default"),
		Version:       first(req.Version, "1"),
		Environment:   first(req.Environment, s.cfg.Environment),
		Strict:        strict,
		TestsRequired: requireTests,
	}
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		return report, nil, "", err
	}
	report.Digest = audit.DigestBytes([]byte(source))
	program, err := compileSource(source, req.Path, baseDir, strict)
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		return report, nil, source, err
	}
	report.Diagnostics = append(report.Diagnostics, documentDiagnostics(source, req.Path, baseDir, strict)...)
	if program != nil {
		report.Name = first(req.Name, firstModule(program), report.Name)
		report.Features = bcl.InspectDecisionPlatform(program)
		for id := range program.Decisions {
			report.Decisions = append(report.Decisions, id)
		}
		sort.Strings(report.Decisions)
		report.Diagnostics = append(report.Diagnostics, program.Diagnostics...)
	}
	if strict {
		requireVersionDeclaration(&report.Diagnostics)
	}
	if program == nil || len(program.Decisions) == 0 {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: "definition does not contain any decisions"})
	}
	if (req.RunTests || requireTests) && program != nil {
		report.Tests = s.runTests(program, firstDecision(program), req.Bundle, requireTests)
		report.Diagnostics = append(report.Diagnostics, report.Tests.Diagnostics...)
		report.Gates = report.Tests.Gates
	}
	report.Valid = !hasErrorDiagnostics(report.Diagnostics)
	report.Publishable = report.Valid && len(report.Decisions) > 0 && (report.Tests == nil || report.Tests.Passed)
	_ = ctx
	return report, program, source, nil
}

func (s *Service) strictValidation(requested bool) bool {
	return requested || (s != nil && s.cfg.StrictValidation)
}

func (s *Service) strictEvaluation(requested bool) bool {
	return requested || (s != nil && s.cfg.StrictEvaluation)
}

func (s *Service) requireTests(requested bool) bool {
	return requested || (s != nil && s.cfg.RequireTests)
}

func documentDiagnostics(source, path, baseDir string, strict bool) []bcl.Diagnostic {
	doc, opts, err := parseDocumentForValidation(source, path, baseDir, strict)
	if err != nil {
		return []bcl.Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	resolved, resolveDiags := bcl.ResolveDocument(doc, opts)
	diags := append([]bcl.Diagnostic{}, resolveDiags...)
	diags = append(diags, bcl.Validate(resolved, opts)...)
	if !strict {
		return diags
	}
	if !hasBCLVersionDeclaration(doc.Items) {
		diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "missing bcl version declaration", Span: doc.Span})
	}
	return diags
}

func parseDocumentForValidation(source, path, baseDir string, strict bool) (*bcl.Document, *bcl.Options, error) {
	opts := &bcl.Options{AllowTime: true, ResolveImports: true, ResolveModules: true, Strict: strict}
	if path != "" {
		doc, err := bcl.ParsePath(path)
		if err != nil {
			return nil, nil, err
		}
		opts.BaseDir = filepath.Dir(path)
		return doc, opts, nil
	}
	doc, err := bcl.Parse([]byte(source))
	if err != nil {
		return nil, nil, err
	}
	opts.BaseDir = baseDir
	return doc, opts, nil
}

func hasBCLVersionDeclaration(nodes []bcl.Node) bool {
	for _, n := range nodes {
		b, ok := n.(*bcl.Block)
		if !ok || b.Type != "bcl" {
			continue
		}
		for _, child := range b.Body {
			a, ok := child.(*bcl.Assignment)
			if ok && a.Name == "version" {
				return true
			}
		}
	}
	return false
}

func requireVersionDeclaration(diags *[]bcl.Diagnostic) {
	if diags == nil {
		return
	}
	hasError := false
	for _, diag := range *diags {
		if diag.Message == "missing bcl version declaration" && diag.Severity == "error" {
			hasError = true
			break
		}
	}
	if hasError {
		filtered := (*diags)[:0]
		for _, diag := range *diags {
			if diag.Message == "missing bcl version declaration" && diag.Severity != "error" {
				continue
			}
			filtered = append(filtered, diag)
		}
		*diags = filtered
	}
}

func copyMetadata(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func metadataBool(metadata map[string]any, key string) bool {
	switch v := metadata[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || v == "1"
	default:
		return false
	}
}

func (s *Service) candidateProgram(req SimulationRequest) (*bcl.DecisionProgram, error) {
	switch {
	case req.CandidatePath != "":
		return bcl.CompileDecisionFile(req.CandidatePath, &bcl.Options{AllowTime: true})
	case req.CandidateSource != "":
		return compileSource(req.CandidateSource, "", req.CandidateBaseDir, false)
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

func (s *Service) audit(ctx context.Context, operation, definition, version, env, digest string, request, result any, start time.Time, trace []string) (audit.Envelope, error) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	completed := time.Now()
	previous, err := s.store.LastAuditHash(ctx)
	if err != nil {
		return audit.Envelope{}, fmt.Errorf("audit previous hash failed: %w", err)
	}
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
	if err := s.store.AppendAudit(ctx, envelope); err != nil {
		return audit.Envelope{}, fmt.Errorf("audit append failed: %w", err)
	}
	return envelope, nil
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

func compileSource(source, path, baseDir string, strict bool) (*bcl.DecisionProgram, error) {
	if path != "" {
		doc, err := bcl.ParsePath(path)
		if err != nil {
			return nil, err
		}
		program, err := bcl.CompileDecisionDocument(doc, &bcl.Options{AllowTime: true, BaseDir: filepath.Dir(path), ResolveImports: true, ResolveModules: true, Strict: strict})
		attachWorkflowBlocks(program, doc)
		return program, err
	}
	doc, err := bcl.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	program, err := bcl.CompileDecisionDocument(doc, &bcl.Options{AllowTime: true, BaseDir: baseDir, ResolveImports: true, ResolveModules: true, Strict: strict})
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
