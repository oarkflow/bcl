package condition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	if cfg.DefaultTenant == "" {
		cfg.DefaultTenant = "default"
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

func (s *Service) ListActionDeliveries(ctx context.Context, query storage.ActionDeliveryQuery) ([]storage.ActionDeliveryRecord, error) {
	ctx = s.requestContext(ctx, query.TenantID)
	return s.store.ListActionDeliveries(ctx, query)
}

func (s *Service) ListIncidents(ctx context.Context, query storage.IncidentQuery) ([]storage.IncidentRecord, error) {
	ctx = s.requestContext(ctx, query.TenantID)
	return s.store.ListIncidents(ctx, query)
}

func (s *Service) Compact(ctx context.Context, req storage.RetentionRequest) (storage.RetentionResult, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	if req.Before.IsZero() {
		req.Before = s.now()
	}
	return s.store.Compact(ctx, req)
}

func (s *Service) now() time.Time {
	if s != nil {
		if s.cfg.Clock != nil {
			return s.cfg.Clock().UTC()
		}
		if fixed := strings.TrimSpace(s.cfg.Runtime.FixedTime); fixed != "" {
			if t, err := time.Parse(time.RFC3339Nano, fixed); err == nil {
				return t.UTC()
			}
		}
	}
	return time.Now().UTC()
}

func (s *Service) Publish(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
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
	ctx = s.requestContext(ctx, req.TenantID)
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
		TenantID:    TenantFromContext(ctx),
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
	ctx = s.requestContext(ctx, "")
	return s.store.ListDefinitions(ctx)
}

func (s *Service) GetDefinition(ctx context.Context, name string) (storage.DefinitionRecord, error) {
	ctx = s.requestContext(ctx, "")
	return s.store.GetActiveDefinition(ctx, name, s.cfg.Environment)
}

func (s *Service) Validate(ctx context.Context, req ValidationRequest) (*ValidationReport, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	report, _, _, err := s.validatePublish(ctx, req)
	if err != nil && report == nil {
		return nil, err
	}
	return report, err
}

func (s *Service) ListVersions(ctx context.Context, name, environment string) ([]storage.DefinitionRecord, error) {
	ctx = s.requestContext(ctx, "")
	return s.store.ListDefinitionVersions(ctx, name, first(environment, s.cfg.Environment))
}

func (s *Service) Activate(ctx context.Context, name, version, environment string) (*ActivationResponse, error) {
	ctx = s.requestContext(ctx, "")
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
	ctx = s.requestContext(ctx, req.TenantID)
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
	ctx = s.requestContext(ctx, req.TenantID)
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
	ctx = s.requestContext(ctx, "")
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
	ctx = s.requestContext(ctx, req.TenantID)
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
	opts := s.bclOptions(ctx, s.strictEvaluation(req.Strict))
	opts.Context = runtimeContext
	opts.Session = runtimeSession
	if err := s.validateExternalPolicy(record.Program); err != nil {
		envelope, auditErr := s.audit(ctx, "evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &EvaluateResponse{Audit: envelope}, err
	}
	report, err := bcl.EvaluateDecisionPlatform(record.Program, bcl.DecisionPlatformRequest{
		Decision:        req.Decision,
		Input:           input,
		Bundle:          req.Bundle,
		IncludeGates:    req.IncludeGates,
		Counterfactuals: req.Counterfactuals,
		IncludeFeatures: req.IncludeFeatures,
		Strict:          s.strictEvaluation(req.Strict),
	}, opts)
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

func (s *Service) EvaluateChain(ctx context.Context, definition, chainID string, req ChainEvaluateRequest) (*ChainEvaluateResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	chain, err := chainDefinition(record.Program, chainID)
	if err != nil {
		envelope, auditErr := s.audit(ctx, "chain_evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ChainEvaluateResponse{Audit: envelope}, err
	}
	if err := s.validateExternalPolicy(record.Program); err != nil {
		envelope, auditErr := s.audit(ctx, "chain_evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ChainEvaluateResponse{Audit: envelope}, err
	}
	input, runtimeContext, runtimeSession := runtimeInputFromContext(ctx, req.Input)
	entityKey := strings.TrimSpace(req.EntityKey)
	if entityKey == "" {
		entityKey = strings.TrimSpace(fmt.Sprint(lookupInputPath(input, chain.EntityKeyPath)))
	}
	if entityKey == "" || entityKey == "<nil>" {
		err := fmt.Errorf("chain %q could not resolve entity_key %q", chain.ID, chain.EntityKeyPath)
		envelope, auditErr := s.audit(ctx, "chain_evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &ChainEvaluateResponse{Audit: envelope}, err
	}
	now := s.now()
	_ = s.store.DeleteExpiredChainStates(ctx, now)
	statesBefore := s.loadChainStates(ctx, chain, entityKey)
	allEvents, err := s.store.QueryChainEvents(ctx, storage.ChainEventQuery{Definition: definition, Environment: record.Environment, Chain: chain.ID, EntityKey: entityKey, IncludeExpired: true})
	if err != nil {
		return nil, err
	}
	evaluation := ChainEvaluation{Chain: chain.ID, EntityKey: entityKey, StateBefore: cloneChainStates(statesBefore)}
	pendingEvents := []storage.ChainEventRecord(nil)
	if req.Event != "" {
		pendingEvents = append(pendingEvents, s.newChainEvent(record, chain.ID, "", entityKey, req.Event, "", "", "", nil, nil, nil))
	}
	opts := s.bclOptions(ctx, s.strictEvaluation(req.Strict))
	opts.Context = runtimeContext
	opts.Session = runtimeSession
	workingInput := mergeMaps(input, map[string]any{
		"chain_state":  chainStateFacts(statesBefore),
		"chain_events": chainEventFacts(allEvents),
	})
	for _, decisionID := range chain.Decisions {
		report, evalErr := bcl.EvaluateDecisionPlatform(record.Program, bcl.DecisionPlatformRequest{
			Decision: decisionID,
			Input:    workingInput,
			Strict:   s.strictEvaluation(req.Strict),
		}, opts)
		evaluation.Decisions = append(evaluation.Decisions, ChainDecisionResult{Decision: decisionID, Report: report})
		if evalErr != nil {
			evaluation.Diagnostics = append(evaluation.Diagnostics, bcl.Diagnostic{Severity: "error", Message: evalErr.Error()})
			continue
		}
		if report != nil && report.Decision != nil {
			hydrateDecisionResult(record.Program, report.Decision)
			pendingEvents = append(pendingEvents, s.eventsFromDecision(record, chain.ID, entityKey, report.Decision)...)
			evaluation.FinalDecision = chooseFinalDecision(evaluation.FinalDecision, report.Decision)
		}
	}
	stateByWatch := chainStateMap(statesBefore)
	for _, event := range pendingEvents {
		if event.ID == "" {
			event.ID = newID("watch-event")
		}
		if err := s.store.AppendChainEvent(ctx, event); err != nil {
			return nil, err
		}
		evaluation.Events = append(evaluation.Events, event)
		allEvents = append(allEvents, event)
	}
	for _, watch := range chain.Watches {
		state, generated := s.applyWatch(ctx, record, chain.ID, entityKey, watch, allEvents, stateByWatch[watch.ID], now)
		if state.Watch != "" {
			if err := s.store.UpsertChainState(ctx, state); err != nil {
				return nil, err
			}
			stateByWatch[watch.ID] = state
			if state.Action != "" && state.Step != "" {
				evaluation.FinalAction, evaluation.FinalEffect, evaluation.FinalReason, evaluation.FinalSeverity = chooseFinalAction(evaluation.FinalAction, evaluation.FinalEffect, evaluation.FinalReason, evaluation.FinalSeverity, state.Action, actionEffect(state.Action), fmt.Sprintf("%s triggered %s", watch.ID, state.Step), state.Severity)
			}
		}
		for _, event := range generated {
			if err := s.store.AppendChainEvent(ctx, event); err != nil {
				return nil, err
			}
			evaluation.Events = append(evaluation.Events, event)
			allEvents = append(allEvents, event)
		}
	}
	evaluation.StateAfter = chainStateSlice(stateByWatch)
	if evaluation.FinalEffect == "" && evaluation.FinalDecision != nil {
		evaluation.FinalEffect = evaluation.FinalDecision.Effect
		evaluation.FinalReason = evaluation.FinalDecision.Reason
		evaluation.FinalAction = fmt.Sprint(evaluation.FinalDecision.Attributes["action"])
		if evaluation.FinalAction == "<nil>" {
			evaluation.FinalAction = ""
		}
		evaluation.FinalSeverity = fmt.Sprint(evaluation.FinalDecision.Metadata["severity"])
		if evaluation.FinalSeverity == "<nil>" {
			evaluation.FinalSeverity = ""
		}
	}
	envelope, err := s.audit(ctx, "chain_evaluate", definition, record.Version, record.Environment, record.Digest, req, evaluation, start, nil)
	if err != nil {
		return nil, err
	}
	return &ChainEvaluateResponse{Evaluation: evaluation, Audit: envelope}, nil
}

func (s *Service) EvaluateLifecycle(ctx context.Context, definition, lifecycleID string, req LifecycleEvaluateRequest) (*LifecycleEvaluateResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	lifecycle, err := lifecycleDefinition(record.Program, lifecycleID)
	if err != nil {
		envelope, auditErr := s.audit(ctx, "lifecycle_evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &LifecycleEvaluateResponse{Audit: envelope}, err
	}
	phase := lifecyclePhase(lifecycle, req.Phase)
	if phase == nil {
		err := fmt.Errorf("unknown lifecycle phase %q", req.Phase)
		envelope, auditErr := s.audit(ctx, "lifecycle_evaluate_failed", definition, record.Version, record.Environment, record.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &LifecycleEvaluateResponse{Audit: envelope}, err
	}
	if err := s.validateExternalPolicy(record.Program); err != nil {
		return nil, err
	}
	match := s.matchLifecycleRoute(record.Program, lifecycle, req.Method, req.Path)
	input, runtimeContext, runtimeSession := runtimeInputFromContext(ctx, req.Input)
	match, overlayFacts := applyPolicyOverlays(record.Program, match, TenantFromContext(ctx), record.Environment, req.Path)
	routeFacts := lifecycleRouteFacts(match, req.Method, req.Path)
	responseFacts := lifecycleResponseFacts(record.Program, req.Response)
	requestFacts := lifecycleRequestFacts(mergeMaps(mapFromAny(input["request"]), req.Request), req.Method, req.Path)
	augmented := mergeMaps(input, map[string]any{
		"phase":           phase.ID,
		"route":           routeFacts,
		"endpoint":        mergeMaps(routeFacts, mapFromAny(input["endpoint"])),
		"response":        responseFacts,
		"policy_overlays": overlayFacts,
	})
	if len(requestFacts) > 0 || req.Method != "" || req.Path != "" {
		augmented["request"] = requestFacts
	}
	now := s.now()
	_ = s.store.DeleteExpiredChainStates(ctx, now)
	chainStates, chainEvents := s.lifecycleChainContext(ctx, definition, record, lifecycle, augmented)
	if len(chainStates) > 0 {
		augmented["chain_state"] = mergeMaps(mapFromAny(augmented["chain_state"]), chainStateFacts(chainStates))
	}
	if len(chainEvents) > 0 {
		augmented["chain_events"] = chainEventFacts(chainEvents)
	}
	entityKey := strings.TrimSpace(req.EntityKey)
	if entityKey == "" {
		entityKey = strings.TrimSpace(fmt.Sprint(lookupInputPath(augmented, lifecycle.EntityKeyPath)))
	}
	if entityKey == "" || entityKey == "<nil>" {
		entityKey = first(req.Path, req.Method, "lifecycle")
	}
	evaluation := LifecycleEvaluation{Lifecycle: lifecycle.ID, Phase: phase.ID, TraceID: lifecycleTraceID(ctx), EntityKey: entityKey, Route: match}
	opts := s.bclOptions(ctx, s.strictEvaluation(req.Strict))
	opts.Context = runtimeContext
	opts.Session = runtimeSession
	for _, decisionID := range phase.Decisions {
		report, evalErr := bcl.EvaluateDecisionPlatform(record.Program, bcl.DecisionPlatformRequest{
			Decision: decisionID,
			Input:    augmented,
			Strict:   s.strictEvaluation(req.Strict),
		}, opts)
		evaluation.Decisions = append(evaluation.Decisions, ChainDecisionResult{Decision: decisionID, Report: report})
		if evalErr != nil {
			evaluation.Diagnostics = append(evaluation.Diagnostics, bcl.Diagnostic{Severity: "error", Message: evalErr.Error()})
			continue
		}
		if report != nil && report.Decision != nil {
			hydrateDecisionResult(record.Program, report.Decision)
			evaluation.FinalDecision(report.Decision)
			evaluation.AddEnforcement(enforcementWithActiveState(enforcementFromDecision(report.Decision), chainStates, now))
			evaluation.Actions = append(evaluation.Actions, s.lifecycleActionsFromDecision(ctx, record, lifecycle.ID, entityKey, report.Decision, req.DryRun)...)
		}
	}
	chainEvent := req.Event
	if chainEvent == "" {
		chainEvent = evaluation.FinalAction
	}
	if chainEvent == "" {
		for _, action := range evaluation.Actions {
			if action.Name != "" {
				chainEvent = action.Name
				break
			}
		}
	}
	finalSeverity := ""
	for _, chainID := range phase.Chains {
		chainEntityKey := ""
		if req.EntityKey != "" {
			chainEntityKey = entityKey
		}
		resp, err := s.EvaluateChain(ctx, definition, chainID, ChainEvaluateRequest{Input: augmented, Event: chainEvent, EntityKey: chainEntityKey, Strict: req.Strict})
		if err != nil {
			evaluation.Diagnostics = append(evaluation.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
			continue
		}
		evaluation.Chains = append(evaluation.Chains, resp.Evaluation)
		evaluation.FinalAction, evaluation.FinalEffect, evaluation.FinalReason, finalSeverity = chooseFinalAction(evaluation.FinalAction, evaluation.FinalEffect, evaluation.FinalReason, finalSeverity, resp.Evaluation.FinalAction, resp.Evaluation.FinalEffect, resp.Evaluation.FinalReason, resp.Evaluation.FinalSeverity)
		for _, state := range resp.Evaluation.StateAfter {
			evaluation.AddEnforcement(enforcementFromState(state, resp.Evaluation.FinalReason, now))
		}
		for _, event := range resp.Evaluation.Events {
			action := LifecycleAction{Name: event.EventType, ReasonCode: event.ReasonCode, Severity: event.Severity, Attributes: cloneMap(event.Attributes), Metadata: cloneMap(event.Metadata)}
			handled, result := false, &ActionResult{Handled: false, Status: "dry_run"}
			if !req.DryRun {
				handled, result = s.dispatchLifecycleAction(ctx, record, action)
			}
			action.Handled = handled
			action.Result = result
			delivery := s.actionDeliveryRecord(record, lifecycle.ID, entityKey, action)
			if delivery.Status == "" {
				delivery.Status = "event_persisted"
			}
			if err := s.store.SaveActionDelivery(ctx, delivery); err == nil {
				action.DeliveryID = delivery.ID
			}
			if incident, ok := s.incidentFromAction(ctx, record, lifecycle.ID, entityKey, action); ok {
				if err := s.store.UpsertIncident(ctx, incident); err == nil {
					action.IncidentID = incident.ID
				}
			}
			evaluation.Actions = append(evaluation.Actions, action)
		}
	}
	envelope, err := s.audit(ctx, "lifecycle_evaluate", definition, record.Version, record.Environment, record.Digest, req, evaluation, start, nil)
	if err != nil {
		return nil, err
	}
	evaluation.AuditID = envelope.ID
	return &LifecycleEvaluateResponse{Evaluation: evaluation, Audit: envelope}, nil
}

func (s *Service) lifecycleChainContext(ctx context.Context, definition string, record storage.DefinitionRecord, lifecycle *LifecycleDefinition, input map[string]any) ([]storage.ChainStateRecord, []storage.ChainEventRecord) {
	if lifecycle == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	var states []storage.ChainStateRecord
	var events []storage.ChainEventRecord
	for _, phase := range lifecycle.Phases {
		for _, chainID := range phase.Chains {
			if seen[chainID] {
				continue
			}
			seen[chainID] = true
			chain, err := chainDefinition(record.Program, chainID)
			if err != nil {
				continue
			}
			entityKey := strings.TrimSpace(fmt.Sprint(lookupInputPath(input, chain.EntityKeyPath)))
			if entityKey == "" || entityKey == "<nil>" {
				continue
			}
			states = append(states, s.loadChainStates(ctx, chain, entityKey)...)
			chainEvents, err := s.store.QueryChainEvents(ctx, storage.ChainEventQuery{Definition: definition, Environment: record.Environment, Chain: chain.ID, EntityKey: entityKey, IncludeExpired: true})
			if err == nil {
				events = append(events, chainEvents...)
			}
		}
	}
	return states, events
}

func lifecycleTraceID(ctx context.Context) string {
	if facts := ContextFactsFromContext(ctx); facts != nil {
		if request, ok := facts["request"].(map[string]any); ok {
			if id := strings.TrimSpace(fmt.Sprint(request["id"])); id != "" && id != "<nil>" {
				return id
			}
		}
	}
	return newID("trace")
}

func (s *Service) Test(ctx context.Context, definition, bundle string) (*TestReport, error) {
	ctx = s.requestContext(ctx, "")
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	report := s.runTests(record.Program, firstDecision(record.Program), bundle, false, record.Name, record.Version, record.Environment)
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
	ctx = s.requestContext(ctx, "")
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	report, err := bcl.EvaluateDecisionGates(record.Program, bundle, s.bclOptions(ctx, false))
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

func (s *Service) ExplainPackage(ctx context.Context, definition string, req PackageExplainRequest) (*PackageExplainResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	start := time.Now()
	base, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
	if err != nil {
		return nil, err
	}
	candidate, err := s.candidateProgram(SimulationRequest{CandidateSource: req.CandidateSource, CandidatePath: req.CandidatePath, CandidateBaseDir: req.CandidateBaseDir})
	if err != nil {
		envelope, auditErr := s.audit(ctx, "package_explain_failed", definition, base.Version, base.Environment, base.Digest, req, map[string]any{"error": err.Error()}, start, nil)
		if auditErr != nil {
			return nil, auditErr
		}
		return &PackageExplainResponse{Audit: envelope}, err
	}
	report := packageExplain(definition, base.Version, base.Program, candidate)
	envelope, err := s.audit(ctx, "package_explain", definition, base.Version, base.Environment, base.Digest, req, report, start, nil)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveReport(ctx, storage.ReportRecord{ID: envelope.ID, Kind: "package_explain", Definition: definition, CreatedAt: time.Now(), Payload: map[string]any{"report": report}}); err != nil {
		return nil, err
	}
	return &PackageExplainResponse{Report: report, Audit: envelope}, nil
}

func (s *Service) Canary(ctx context.Context, definition string, req CanaryRequest) (*CanaryResponse, error) {
	resp, err := s.comparePrograms(ctx, "canary", definition, req.SimulationRequest)
	if resp == nil {
		return nil, err
	}
	changed := 0
	hasErrors := false
	if resp.Compare != nil {
		changed = len(resp.Compare.ChangedCases)
		hasErrors = hasErrorDiagnostics(resp.Compare.Diagnostics)
	}
	passed := changed <= req.MaxChangedCases
	if req.RequireNoErrors && hasErrors {
		passed = false
	}
	out := &CanaryResponse{Passed: passed, ChangedCases: changed, Compare: resp.Compare, Audit: resp.Audit}
	if err != nil {
		return out, err
	}
	if !passed {
		return out, fmt.Errorf("canary failed: %d changed cases", changed)
	}
	if req.Promote {
		promotion, promoteErr := s.Publish(ctx, PublishRequest{
			TenantID:     req.TenantID,
			Name:         definition,
			Version:      first(req.PromoteVersion, newID("canary-version")),
			Source:       req.CandidateSource,
			Path:         req.CandidatePath,
			BaseDir:      req.CandidateBaseDir,
			RunTests:     true,
			Strict:       s.cfg.StrictValidation,
			RequireTests: s.cfg.RequireTests,
			Metadata:     req.PromoteMetadata,
		})
		out.Promotion = promotion
		if promoteErr != nil {
			return out, promoteErr
		}
	}
	return out, nil
}

func (s *Service) comparePrograms(ctx context.Context, operation, definition string, req SimulationRequest) (*SimulationResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	start := time.Now()
	base, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
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
		compare, err = bcl.CompareDecisionDataset(base.Program, candidate, decision, req.Dataset, s.bclOptions(ctx, false))
	} else {
		compare, err = bcl.CompareDecisionBatch(base.Program, candidate, decision, req.Cases, s.bclOptions(ctx, false))
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
	opts := s.bclOptions(ctx, false)
	opts.Context = ContextFactsFromContext(ctx)
	opts.Session = SessionFromContext(ctx)
	return bcl.CompareDecisionBatch(base, candidate, req.Decision, []bcl.DecisionBatchCase{{
		ID:    "shadow",
		Input: req.Input,
	}}, opts)
}

func (s *Service) Reload(ctx context.Context, req ReloadRequest) (*ReloadResponse, error) {
	ctx = s.requestContext(ctx, req.TenantID)
	start := time.Now()
	last, err := s.store.GetActiveDefinition(ctx, req.Name, s.cfg.Environment)
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
	ctx = s.requestContext(ctx, "")
	return s.store.ListAudits(ctx)
}

func (s *Service) QueryAudits(ctx context.Context, opts storage.ListOptions) ([]audit.Envelope, error) {
	ctx = s.requestContext(ctx, opts.TenantID)
	return s.store.ListAuditsQuery(ctx, opts)
}

func (s *Service) QueryReports(ctx context.Context, opts storage.ListOptions) ([]storage.ReportRecord, error) {
	ctx = s.requestContext(ctx, opts.TenantID)
	return s.store.ListReportsQuery(ctx, opts)
}

func (s *Service) GetAudit(ctx context.Context, id string) (audit.Envelope, error) {
	ctx = s.requestContext(ctx, "")
	return s.store.GetAudit(ctx, id)
}

func (s *Service) VerifyAudits(ctx context.Context) error {
	ctx = s.requestContext(ctx, "")
	records, err := s.store.ListAudits(ctx)
	if err != nil {
		return err
	}
	return audit.VerifyChain(records)
}

func (s *Service) Ready(ctx context.Context) error {
	ctx = s.requestContext(ctx, "")
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
	ctx = s.requestContext(ctx, "")
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
			"tenant_isolation_enabled":     true,
			"deterministic_runtime":        true,
			"external_policy_restricted":   len(s.cfg.Runtime.AllowedDatasetAdapters) == 0 || s.cfg.Runtime.ExternalTimeout > 0,
			"failed_actions_clear":         false,
			"open_incidents_clear":         false,
			"state_cardinality_ok":         false,
		},
		Counts: map[string]int{},
	}
	if _, err := s.store.ListDefinitions(ctx); err == nil {
		report.Checks["store_available"] = true
	}
	if err := s.VerifyAudits(ctx); err == nil {
		report.Checks["audit_chain_valid"] = true
	}
	if records, err := s.store.ListActionDeliveries(ctx, storage.ActionDeliveryQuery{Status: "dead_letter"}); err == nil {
		report.Counts["dead_letter_actions"] = len(records)
		report.Checks["failed_actions_clear"] = len(records) == 0
	}
	if records, err := s.store.ListIncidents(ctx, storage.IncidentQuery{Status: "open"}); err == nil {
		report.Counts["open_incidents"] = len(records)
		report.Checks["open_incidents_clear"] = len(records) == 0
	}
	if stats, err := s.store.Stats(ctx); err == nil {
		report.Counts["definitions"] = stats.Definitions
		report.Counts["chain_events"] = stats.ChainEvents
		report.Counts["chain_states"] = stats.ChainStates
		report.Counts["action_deliveries"] = stats.ActionDeliveries
		report.Counts["incidents"] = stats.Incidents
		limit := s.cfg.Runtime.MaxStateRecords
		if limit <= 0 {
			limit = 100000
		}
		report.Checks["state_cardinality_ok"] = stats.ChainEvents+stats.ChainStates+stats.ActionDeliveries+stats.Incidents <= limit
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
	ctx = s.requestContext(ctx, "")
	start := time.Now()
	record, err := s.store.GetActiveDefinition(ctx, definition, s.cfg.Environment)
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
		TenantID:    TenantFromContext(ctx),
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
	ctx = s.requestContext(ctx, "")
	start := time.Now()
	record, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	definition, err := s.store.GetActiveDefinition(ctx, record.Definition, record.Environment)
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
	ctx = s.requestContext(ctx, "")
	record, err := s.store.GetWorkflowRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	run := workflowRunFromRecord(record)
	return &run, nil
}

func (s *Service) ListWorkflowRuns(ctx context.Context, opts storage.ListOptions) ([]storage.WorkflowRunRecord, error) {
	ctx = s.requestContext(ctx, opts.TenantID)
	return s.store.ListWorkflowRuns(ctx, opts)
}

func (s *Service) runTests(program *bcl.DecisionProgram, defaultDecision, bundle string, requireTests bool, definition, version, environment string) *TestReport {
	report := &TestReport{Passed: true}
	lifecycleResults := s.runLifecycleScenarios(program, definition, version, environment)
	if requireTests && len(program.Tests) == 0 && len(lifecycleResults) == 0 && bundle == "" {
		report.Passed = false
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: "definition requires at least one test or decision gate"})
		return report
	}
	for _, result := range lifecycleResults {
		if !result.Passed {
			report.Passed = false
			report.Diagnostics = append(report.Diagnostics, result.Diagnostics...)
		}
		report.LifecycleScenarios = append(report.LifecycleScenarios, result)
	}
	for _, test := range program.Tests {
		decision := first(test.Decision, defaultDecision)
		result, err := bcl.EvaluateDecisionScenario(program, &bcl.DecisionScenario{
			Name: test.Name, Decision: decision, Input: test.Input, Expect: test.Expect,
		}, s.bclOptions(context.Background(), false))
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
		gates, err := bcl.EvaluateDecisionGates(program, bundle, s.bclOptions(context.Background(), false))
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
	program, err := compileSource(source, req.Path, baseDir, s.bclOptions(ctx, strict))
	if err != nil {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: err.Error()})
		return report, nil, source, err
	}
	report.Diagnostics = append(report.Diagnostics, documentDiagnostics(source, req.Path, baseDir, s.bclOptions(ctx, strict))...)
	if program != nil {
		report.Name = first(req.Name, firstModule(program), report.Name)
		report.Features = bcl.InspectDecisionPlatform(program)
		for id := range program.Decisions {
			report.Decisions = append(report.Decisions, id)
		}
		sort.Strings(report.Decisions)
		report.Diagnostics = append(report.Diagnostics, program.Diagnostics...)
		report.Diagnostics = append(report.Diagnostics, validateChains(program)...)
		report.Diagnostics = append(report.Diagnostics, validateRoutesAndLifecycles(program)...)
		report.Diagnostics = append(report.Diagnostics, validatePolicyContracts(program)...)
		report.Diagnostics = append(report.Diagnostics, resultReferenceDiagnostics(program)...)
	}
	if strict {
		requireVersionDeclaration(&report.Diagnostics)
	}
	if program == nil || len(program.Decisions) == 0 {
		report.Diagnostics = append(report.Diagnostics, bcl.Diagnostic{Severity: "error", Message: "definition does not contain any decisions"})
	}
	if (req.RunTests || requireTests) && program != nil {
		report.Tests = s.runTests(program, firstDecision(program), req.Bundle, requireTests, report.Name, report.Version, report.Environment)
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

func (s *Service) requestContext(ctx context.Context, tenant string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	current := TenantFromContext(ctx)
	if current != "default" {
		return ctx
	}
	if tenant == "" && s != nil {
		tenant = s.cfg.DefaultTenant
	}
	return ContextWithTenant(ctx, first(tenant, "default"))
}

func (s *Service) bclOptions(ctx context.Context, strict bool) *bcl.Options {
	policy := RuntimePolicy{}
	if s != nil {
		policy = s.cfg.Runtime
	}
	opts := &bcl.Options{
		Strict:                 strict,
		AllowTime:              policy.AllowTime || strings.TrimSpace(policy.FixedTime) != "",
		AllowEnv:               policy.AllowEnv,
		ResolveImports:         true,
		ResolveModules:         true,
		Context:                ContextFactsFromContext(ctx),
		Session:                SessionFromContext(ctx),
		AllowedDatasetAdapters: policy.AllowedDatasetAdapters,
		AllowedHTTPHosts:       policy.AllowedHTTPHosts,
		AllowedHTTPMethods:     policy.AllowedHTTPMethods,
		ExternalTimeout:        policy.ExternalTimeout,
	}
	if policy.FixedTime != "" {
		fixed, err := time.Parse(time.RFC3339Nano, policy.FixedTime)
		if err == nil {
			opts.Now = func() time.Time { return fixed.UTC() }
		}
	}
	if policy.ExternalTimeout > 0 {
		opts.HTTPClient = &http.Client{Timeout: policy.ExternalTimeout}
	}
	return opts
}

func (s *Service) validateExternalPolicy(program *bcl.DecisionProgram) error {
	if program == nil {
		return nil
	}
	allowedAdapters := stringSet(s.cfg.Runtime.AllowedDatasetAdapters)
	allowedHosts := stringSet(s.cfg.Runtime.AllowedHTTPHosts)
	allowedMethods := stringSet(s.cfg.Runtime.AllowedHTTPMethods)
	for id, dataset := range program.Datasets {
		adapter := strings.ToLower(strings.TrimSpace(dataset.Source.Adapter))
		if adapter == "" || adapter == "inline" {
			continue
		}
		if !allowedAdapters[adapter] {
			return fmt.Errorf("dataset %q uses disallowed adapter %q", id, adapter)
		}
		if adapter == "http" || adapter == "https" {
			rawURL := first(scalarString(dataset.Source.Config["url"]), scalarString(dataset.Source.Config["endpoint"]), scalarString(dataset.Source.Config["base_url"]))
			parsed, err := url.Parse(rawURL)
			if err != nil || parsed.Hostname() == "" {
				return fmt.Errorf("dataset %q has invalid http url", id)
			}
			if !allowedHosts[strings.ToLower(parsed.Hostname())] {
				return fmt.Errorf("dataset %q uses disallowed host %q", id, parsed.Hostname())
			}
			method := strings.ToUpper(first(scalarString(dataset.Source.Config["method"]), "GET"))
			if len(allowedMethods) == 0 || !allowedMethods[method] {
				return fmt.Errorf("dataset %q uses disallowed method %q", id, method)
			}
			if s.cfg.Runtime.ExternalTimeout <= 0 {
				return fmt.Errorf("dataset %q requires external_timeout", id)
			}
		}
	}
	return nil
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[strings.ToLower(value)] = true
			out[strings.ToUpper(value)] = true
		}
	}
	return out
}

func scalarString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func documentDiagnostics(source, path, baseDir string, opts *bcl.Options) []bcl.Diagnostic {
	doc, opts, err := parseDocumentForValidation(source, path, baseDir, opts)
	if err != nil {
		return []bcl.Diagnostic{{Severity: "error", Message: err.Error()}}
	}
	resolved, resolveDiags := bcl.ResolveDocument(doc, opts)
	diags := append([]bcl.Diagnostic{}, resolveDiags...)
	diags = append(diags, bcl.Validate(resolved, opts)...)
	if opts == nil || !opts.Strict {
		return diags
	}
	if !hasBCLVersionDeclaration(doc.Items) {
		diags = append(diags, bcl.Diagnostic{Severity: "error", Message: "missing bcl version declaration", Span: doc.Span})
	}
	return diags
}

func parseDocumentForValidation(source, path, baseDir string, opts *bcl.Options) (*bcl.Document, *bcl.Options, error) {
	if opts == nil {
		opts = &bcl.Options{}
	}
	opts.ResolveImports = true
	opts.ResolveModules = true
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
		return bcl.CompileDecisionFile(req.CandidatePath, s.bclOptions(context.Background(), false))
	case req.CandidateSource != "":
		return compileSource(req.CandidateSource, "", req.CandidateBaseDir, s.bclOptions(context.Background(), false))
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
		TenantID:         TenantFromContext(ctx),
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
		Metadata:         s.auditMetadata(request, result, completed.Sub(start)),
	}, previous)
	if err := s.store.AppendAudit(ctx, envelope); err != nil {
		return audit.Envelope{}, fmt.Errorf("audit append failed: %w", err)
	}
	return envelope, nil
}

func (s *Service) auditMetadata(request, result any, latency time.Duration) map[string]any {
	meta := map[string]any{
		"latency_ms":                 latency.Milliseconds(),
		"runtime_policy_fingerprint": audit.Fingerprint(s.cfg.Runtime),
		"input_hash":                 audit.Fingerprint(request),
	}
	var report *bcl.DecisionPlatformReport
	switch v := result.(type) {
	case map[string]any:
		report, _ = v["report"].(*bcl.DecisionPlatformReport)
	case *bcl.DecisionPlatformReport:
		report = v
	}
	if report != nil {
		meta["diagnostics_count"] = len(report.Diagnostics)
		if report.Decision != nil {
			meta["decision_id"] = report.Decision.DecisionID
			meta["effect"] = report.Decision.Effect
			meta["reason_code"] = report.Decision.ReasonCode
			matched, skipped, selected := traceCounts(report.Decision.Explain)
			meta["matched_rules"] = matched
			meta["skipped_rules"] = skipped
			meta["selected_rules"] = selected
		}
		if len(report.DatasetSources) > 0 {
			meta["dataset_source_fingerprints"] = fingerprintDatasetSources(report.DatasetSources)
		}
	}
	return meta
}

func traceCounts(trace []bcl.DecisionTrace) (matched, skipped, selected int) {
	for _, item := range trace {
		switch item.Status {
		case "matched":
			matched++
		case "skipped", "skipped_effective_window":
			skipped++
		case "selected":
			selected++
		}
	}
	return matched, skipped, selected
}

func fingerprintDatasetSources(sources map[string]bcl.DatasetSource) map[string]string {
	out := map[string]string{}
	for id, source := range sources {
		out[id] = audit.Fingerprint(source)
	}
	return out
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

func compileSource(source, path, baseDir string, opts *bcl.Options) (*bcl.DecisionProgram, error) {
	if opts == nil {
		opts = &bcl.Options{}
	}
	if path != "" {
		doc, err := bcl.ParsePath(path)
		if err != nil {
			return nil, err
		}
		opts.BaseDir = filepath.Dir(path)
		opts.ResolveImports = true
		opts.ResolveModules = true
		resolveOpts := *opts
		resolveOpts.ResolveModules = false
		resolved, resolveDiags := bcl.ResolveDocument(doc, &resolveOpts)
		program, err := bcl.CompileDecisionDocument(resolved, opts)
		if program != nil {
			program.Diagnostics = append(resolveDiags, program.Diagnostics...)
		}
		attachWorkflowBlocks(program, resolved)
		attachChainBlocks(program, resolved)
		attachRouteAndLifecycleBlocks(program, resolved)
		attachPolicyContractBlocks(program, resolved)
		return program, err
	}
	doc, err := bcl.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	opts.BaseDir = baseDir
	opts.ResolveImports = true
	opts.ResolveModules = true
	resolveOpts := *opts
	resolveOpts.ResolveModules = false
	resolved, resolveDiags := bcl.ResolveDocument(doc, &resolveOpts)
	program, err := bcl.CompileDecisionDocument(resolved, opts)
	if program != nil {
		program.Diagnostics = append(resolveDiags, program.Diagnostics...)
	}
	attachWorkflowBlocks(program, resolved)
	attachChainBlocks(program, resolved)
	attachRouteAndLifecycleBlocks(program, resolved)
	attachPolicyContractBlocks(program, resolved)
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

func attachChainBlocks(program *bcl.DecisionProgram, doc *bcl.Document) {
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
	var chains []map[string]any
	collectBlocks(items, "chain", &chains)
	if len(chains) == 0 {
		return
	}
	if program.Governance == nil {
		program.Governance = map[string]any{}
	}
	program.Governance["_condition_chains"] = chains
}

func attachRouteAndLifecycleBlocks(program *bcl.DecisionProgram, doc *bcl.Document) {
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
	if program.Governance == nil {
		program.Governance = map[string]any{}
	}
	var routes []map[string]any
	collectBlocks(items, "routes", &routes)
	if len(routes) > 0 {
		program.Governance["_condition_routes"] = routes
	}
	var lifecycles []map[string]any
	collectBlocks(items, "lifecycle", &lifecycles)
	if len(lifecycles) > 0 {
		program.Governance["_condition_lifecycles"] = lifecycles
	}
}

func attachPolicyContractBlocks(program *bcl.DecisionProgram, doc *bcl.Document) {
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
	if program.Governance == nil {
		program.Governance = map[string]any{}
	}
	for _, spec := range []struct {
		block string
		key   string
	}{
		{"policy_package", "_condition_policy_packages"},
		{"action_catalog", "_condition_action_catalogs"},
		{"output_contract", "_condition_output_contracts"},
		{"standard_facts", "_condition_standard_facts"},
		{"response_classifier", "_condition_response_classifiers"},
		{"policy_overlay", "_condition_policy_overlays"},
		{"lifecycle_test", "_condition_lifecycle_tests"},
	} {
		var blocks []map[string]any
		collectBlocks(items, spec.block, &blocks)
		if len(blocks) > 0 {
			program.Governance[spec.key] = blocks
		}
	}
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
