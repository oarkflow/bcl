package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/oarkflow/condition/pkg/audit"
)

type MemoryStore struct {
	mu          sync.RWMutex
	definitions map[string]DefinitionRecord
	versions    map[string]DefinitionRecord
	active      map[string]ActiveDefinition
	reports     []ReportRecord
	audits      []audit.Envelope
	workflows   map[string]WorkflowRunRecord
	chainEvents []ChainEventRecord
	chainStates map[string]ChainStateRecord
	actions     map[string]ActionDeliveryRecord
	incidents   map[string]IncidentRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{definitions: map[string]DefinitionRecord{}, versions: map[string]DefinitionRecord{}, active: map[string]ActiveDefinition{}, workflows: map[string]WorkflowRunRecord{}, chainStates: map[string]ChainStateRecord{}, actions: map[string]ActionDeliveryRecord{}, incidents: map[string]IncidentRecord{}}
}

func (s *MemoryStore) SaveDefinition(ctx context.Context, record DefinitionRecord) error {
	if err := s.SaveDefinitionVersion(ctx, record); err != nil {
		return err
	}
	return s.ActivateDefinition(ctx, record.Name, record.Version, record.Environment)
}

func (s *MemoryStore) GetDefinition(ctx context.Context, name string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	record, ok := s.definitions[definitionActiveKey(tenant, name, "")]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
	}
	return record, nil
}

func (s *MemoryStore) ListDefinitions(ctx context.Context) ([]DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	out := make([]DefinitionRecord, 0, len(s.definitions))
	for _, record := range s.definitions {
		if firstTenant(record.TenantID) == tenant {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *MemoryStore) SaveDefinitionVersion(ctx context.Context, record DefinitionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Environment == "" {
		record.Environment = "development"
	}
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	s.versions[definitionVersionKey(record.TenantID, record.Name, record.Version, record.Environment)] = record
	if _, ok := s.definitions[definitionActiveKey(record.TenantID, record.Name, record.Environment)]; !ok {
		s.definitions[definitionActiveKey(record.TenantID, record.Name, record.Environment)] = record
	}
	return nil
}

func (s *MemoryStore) GetDefinitionVersion(ctx context.Context, name, version, environment string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	record, ok := s.versions[definitionVersionKey(tenant, name, version, environment)]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q version %q", name, version)
	}
	return record, nil
}

func (s *MemoryStore) ListDefinitionVersions(ctx context.Context, name, environment string) ([]DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	var out []DefinitionRecord
	for _, record := range s.versions {
		if firstTenant(record.TenantID) == tenant && record.Name == name && (environment == "" || record.Environment == environment) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PublishedAt.Equal(out[j].PublishedAt) {
			return out[i].Version < out[j].Version
		}
		return out[i].PublishedAt.Before(out[j].PublishedAt)
	})
	return out, nil
}

func (s *MemoryStore) ActivateDefinition(ctx context.Context, name, version, environment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := TenantFromContext(ctx)
	key := definitionVersionKey(tenant, name, version, environment)
	record, ok := s.versions[key]
	if !ok {
		return fmt.Errorf("unknown definition %q version %q", name, version)
	}
	s.definitions[definitionActiveKey(record.TenantID, name, environment)] = record
	s.active[definitionActiveKey(record.TenantID, name, environment)] = ActiveDefinition{TenantID: record.TenantID, Name: name, Version: version, Environment: firstEnv(environment), ActivatedAt: nowUTC()}
	return nil
}

func (s *MemoryStore) GetActiveDefinition(ctx context.Context, name, environment string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	active, ok := s.active[definitionActiveKey(tenant, name, environment)]
	if ok {
		if record, found := s.versions[definitionVersionKey(active.TenantID, name, active.Version, active.Environment)]; found {
			return record, nil
		}
	}
	record, ok := s.definitions[definitionActiveKey(tenant, name, environment)]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
	}
	return record, nil
}

func (s *MemoryStore) SaveReport(ctx context.Context, report ReportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	report.TenantID = firstTenant(firstNonEmpty(report.TenantID, TenantFromContext(ctx)))
	s.reports = append(s.reports, report)
	return nil
}

func (s *MemoryStore) ListReports(_ context.Context, kind string) ([]ReportRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []ReportRecord
	for _, report := range s.reports {
		if kind == "" || report.Kind == kind {
			out = append(out, report)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListReportsQuery(ctx context.Context, opts ListOptions) ([]ReportRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	return ApplyListOptions(s.reports, opts, func(report ReportRecord) bool {
		return firstTenant(report.TenantID) == tenant &&
			(opts.Kind == "" || report.Kind == opts.Kind) &&
			(opts.Definition == "" || report.Definition == opts.Definition) &&
			inRange(report.CreatedAt, opts)
	}), nil
}

func (s *MemoryStore) AppendAudit(ctx context.Context, envelope audit.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	envelope.TenantID = firstTenant(firstNonEmpty(envelope.TenantID, TenantFromContext(ctx)))
	s.audits = append(s.audits, envelope)
	return nil
}

func (s *MemoryStore) LastAuditHash(ctx context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	for i := len(s.audits) - 1; i >= 0; i-- {
		if firstTenant(s.audits[i].TenantID) == tenant {
			return s.audits[i].Hash, nil
		}
	}
	return "", nil
}

func (s *MemoryStore) ListAudits(ctx context.Context) ([]audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	var out []audit.Envelope
	for _, envelope := range s.audits {
		if firstTenant(envelope.TenantID) == tenant {
			out = append(out, envelope)
		}
	}
	return out, nil
}

func (s *MemoryStore) ListAuditsQuery(ctx context.Context, opts ListOptions) ([]audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	return ApplyListOptions(s.audits, opts, func(envelope audit.Envelope) bool {
		return firstTenant(envelope.TenantID) == tenant &&
			(opts.Operation == "" || envelope.Operation == opts.Operation) &&
			(opts.Definition == "" || envelope.Definition == opts.Definition) &&
			(opts.Subject == "" || envelope.Subject == opts.Subject) &&
			inRange(envelope.CompletedAt, opts)
	}), nil
}

func (s *MemoryStore) GetAudit(ctx context.Context, id string) (audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	for _, envelope := range s.audits {
		if envelope.ID == id && firstTenant(envelope.TenantID) == tenant {
			return envelope, nil
		}
	}
	return audit.Envelope{}, fmt.Errorf("unknown audit %q", id)
}

func (s *MemoryStore) SaveWorkflowRun(ctx context.Context, run WorkflowRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.TenantID = firstTenant(firstNonEmpty(run.TenantID, TenantFromContext(ctx)))
	s.workflows[run.ID] = run
	return nil
}

func (s *MemoryStore) GetWorkflowRun(ctx context.Context, id string) (WorkflowRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	run, ok := s.workflows[id]
	if !ok || firstTenant(run.TenantID) != tenant {
		return WorkflowRunRecord{}, fmt.Errorf("unknown workflow run %q", id)
	}
	return run, nil
}

func (s *MemoryStore) ListWorkflowRuns(ctx context.Context, opts ListOptions) ([]WorkflowRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	runs := make([]WorkflowRunRecord, 0, len(s.workflows))
	for _, run := range s.workflows {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.Before(runs[j].CreatedAt) })
	return ApplyListOptions(runs, opts, func(run WorkflowRunRecord) bool {
		return firstTenant(run.TenantID) == tenant &&
			(opts.Definition == "" || run.Definition == opts.Definition) &&
			(opts.Environment == "" || run.Environment == opts.Environment) &&
			inRange(run.UpdatedAt, opts)
	}), nil
}

func (s *MemoryStore) AppendChainEvent(ctx context.Context, event ChainEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.TenantID = firstTenant(firstNonEmpty(event.TenantID, TenantFromContext(ctx)))
	if event.CreatedAt.IsZero() {
		event.CreatedAt = nowUTC()
	}
	s.chainEvents = append(s.chainEvents, event)
	return nil
}

func (s *MemoryStore) QueryChainEvents(ctx context.Context, query ChainEventQuery) ([]ChainEventRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	now := nowUTC()
	var out []ChainEventRecord
	for _, event := range s.chainEvents {
		if firstTenant(event.TenantID) != tenant ||
			(query.Definition != "" && event.Definition != query.Definition) ||
			(query.Environment != "" && event.Environment != query.Environment) ||
			(query.Chain != "" && event.Chain != query.Chain) ||
			(query.Watch != "" && event.Watch != query.Watch) ||
			(query.EntityKey != "" && event.EntityKey != query.EntityKey) ||
			(query.EventType != "" && event.EventType != query.EventType) ||
			(query.Since != nil && event.CreatedAt.Before(*query.Since)) ||
			(query.Until != nil && event.CreatedAt.After(*query.Until)) {
			continue
		}
		if !query.IncludeExpired && event.ExpiresAt != nil && !event.ExpiresAt.After(now) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	return out, nil
}

func (s *MemoryStore) GetChainState(ctx context.Context, chain, watch, entityKey string) (ChainStateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	state, ok := s.chainStates[chainStateKey(tenant, chain, watch, entityKey)]
	if !ok || firstTenant(state.TenantID) != tenant {
		return ChainStateRecord{}, fmt.Errorf("unknown watch state %q/%q/%q", chain, watch, entityKey)
	}
	if state.ExpiresAt != nil && !state.ExpiresAt.After(nowUTC()) {
		return ChainStateRecord{}, fmt.Errorf("unknown watch state %q/%q/%q", chain, watch, entityKey)
	}
	return state, nil
}

func (s *MemoryStore) ListChainStates(ctx context.Context, query ChainStateQuery) ([]ChainStateRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	now := nowUTC()
	var out []ChainStateRecord
	for _, state := range s.chainStates {
		if firstTenant(state.TenantID) != tenant ||
			(query.Definition != "" && state.Definition != query.Definition) ||
			(query.Environment != "" && state.Environment != query.Environment) ||
			(query.Chain != "" && state.Chain != query.Chain) ||
			(query.Watch != "" && state.Watch != query.Watch) ||
			(query.EntityKey != "" && state.EntityKey != query.EntityKey) {
			continue
		}
		if !query.IncludeExpired && state.ExpiresAt != nil && !state.ExpiresAt.After(now) {
			continue
		}
		out = append(out, state)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].Chain+"/"+out[i].Watch < out[j].Chain+"/"+out[j].Watch
		}
		return out[i].UpdatedAt.Before(out[j].UpdatedAt)
	})
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	return out, nil
}

func (s *MemoryStore) UpsertChainState(ctx context.Context, state ChainStateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.TenantID = firstTenant(firstNonEmpty(state.TenantID, TenantFromContext(ctx)))
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = nowUTC()
	}
	s.chainStates[chainStateKey(state.TenantID, state.Chain, state.Watch, state.EntityKey)] = state
	return nil
}

func (s *MemoryStore) DeleteExpiredChainStates(ctx context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := TenantFromContext(ctx)
	for key, state := range s.chainStates {
		if firstTenant(state.TenantID) == tenant && state.ExpiresAt != nil && !state.ExpiresAt.After(now) {
			delete(s.chainStates, key)
		}
	}
	return nil
}

func (s *MemoryStore) SaveActionDelivery(ctx context.Context, record ActionDeliveryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	if record.CreatedAt.IsZero() {
		record.CreatedAt = nowUTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	s.actions[record.ID] = record
	return nil
}

func (s *MemoryStore) ListActionDeliveries(ctx context.Context, query ActionDeliveryQuery) ([]ActionDeliveryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	out := make([]ActionDeliveryRecord, 0, len(s.actions))
	for _, record := range s.actions {
		if firstTenant(record.TenantID) == tenant &&
			(query.Definition == "" || record.Definition == query.Definition) &&
			(query.Environment == "" || record.Environment == query.Environment) &&
			(query.Action == "" || record.Action == query.Action) &&
			(query.Status == "" || record.Status == query.Status) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	return out, nil
}

func (s *MemoryStore) UpsertIncident(ctx context.Context, record IncidentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	if record.Status == "" {
		record.Status = "open"
	}
	if record.FirstSeen.IsZero() {
		record.FirstSeen = nowUTC()
	}
	if record.LastSeen.IsZero() {
		record.LastSeen = record.FirstSeen
	}
	if record.Count == 0 {
		record.Count = 1
	}
	s.incidents[record.ID] = record
	return nil
}

func (s *MemoryStore) ListIncidents(ctx context.Context, query IncidentQuery) ([]IncidentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	out := make([]IncidentRecord, 0, len(s.incidents))
	for _, record := range s.incidents {
		if firstTenant(record.TenantID) == tenant &&
			(query.Definition == "" || record.Definition == query.Definition) &&
			(query.Environment == "" || record.Environment == query.Environment) &&
			(query.Status == "" || record.Status == query.Status) &&
			(query.Action == "" || record.Action == query.Action) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.Before(out[j].LastSeen) })
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	return out, nil
}

func (s *MemoryStore) Compact(ctx context.Context, req RetentionRequest) (RetentionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := firstTenant(firstNonEmpty(req.TenantID, TenantFromContext(ctx)))
	before := req.Before.UTC()
	result := RetentionResult{}
	for key, state := range s.chainStates {
		if firstTenant(state.TenantID) != tenant ||
			(req.Definition != "" && state.Definition != req.Definition) ||
			(req.Environment != "" && state.Environment != req.Environment) {
			continue
		}
		if state.ExpiresAt != nil && !state.ExpiresAt.After(before) {
			delete(s.chainStates, key)
			result.ChainStates++
		}
	}
	events := s.chainEvents[:0]
	for _, event := range s.chainEvents {
		if firstTenant(event.TenantID) == tenant &&
			(req.Definition == "" || event.Definition == req.Definition) &&
			(req.Environment == "" || event.Environment == req.Environment) &&
			!event.CreatedAt.After(before) {
			result.ChainEvents++
			continue
		}
		events = append(events, event)
	}
	s.chainEvents = events
	for key, record := range s.actions {
		if firstTenant(record.TenantID) != tenant ||
			(req.Definition != "" && record.Definition != req.Definition) ||
			(req.Environment != "" && record.Environment != req.Environment) ||
			record.CreatedAt.After(before) {
			continue
		}
		if !req.DeleteActiveDeliveries && (record.Status == "retry_scheduled" || record.Status == "pending") {
			continue
		}
		delete(s.actions, key)
		result.ActionDeliveries++
	}
	for key, record := range s.incidents {
		if firstTenant(record.TenantID) != tenant ||
			(req.Definition != "" && record.Definition != req.Definition) ||
			(req.Environment != "" && record.Environment != req.Environment) ||
			record.LastSeen.After(before) {
			continue
		}
		if !req.DeleteOpenIncidents && record.Status == "open" {
			continue
		}
		delete(s.incidents, key)
		result.Incidents++
	}
	return result, nil
}

func (s *MemoryStore) Stats(ctx context.Context) (StatsRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tenant := TenantFromContext(ctx)
	stats := StatsRecord{}
	for _, record := range s.definitions {
		if firstTenant(record.TenantID) == tenant {
			stats.Definitions++
		}
	}
	for _, event := range s.chainEvents {
		if firstTenant(event.TenantID) == tenant {
			stats.ChainEvents++
		}
	}
	for _, state := range s.chainStates {
		if firstTenant(state.TenantID) == tenant {
			stats.ChainStates++
		}
	}
	for _, action := range s.actions {
		if firstTenant(action.TenantID) == tenant {
			stats.ActionDeliveries++
		}
	}
	for _, incident := range s.incidents {
		if firstTenant(incident.TenantID) == tenant {
			stats.Incidents++
		}
	}
	return stats, nil
}
