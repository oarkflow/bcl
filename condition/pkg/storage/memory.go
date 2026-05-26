package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"

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
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{definitions: map[string]DefinitionRecord{}, versions: map[string]DefinitionRecord{}, active: map[string]ActiveDefinition{}, workflows: map[string]WorkflowRunRecord{}}
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
