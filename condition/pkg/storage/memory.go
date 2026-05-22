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

func (s *MemoryStore) GetDefinition(_ context.Context, name string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.definitions[name]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
	}
	return record, nil
}

func (s *MemoryStore) ListDefinitions(_ context.Context) ([]DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DefinitionRecord, 0, len(s.definitions))
	for _, record := range s.definitions {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *MemoryStore) SaveDefinitionVersion(_ context.Context, record DefinitionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Environment == "" {
		record.Environment = "development"
	}
	s.versions[definitionVersionKey(record.Name, record.Version, record.Environment)] = record
	if _, ok := s.definitions[record.Name]; !ok {
		s.definitions[record.Name] = record
	}
	return nil
}

func (s *MemoryStore) GetDefinitionVersion(_ context.Context, name, version, environment string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	record, ok := s.versions[definitionVersionKey(name, version, environment)]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q version %q", name, version)
	}
	return record, nil
}

func (s *MemoryStore) ListDefinitionVersions(_ context.Context, name, environment string) ([]DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []DefinitionRecord
	for _, record := range s.versions {
		if record.Name == name && (environment == "" || record.Environment == environment) {
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

func (s *MemoryStore) ActivateDefinition(_ context.Context, name, version, environment string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := definitionVersionKey(name, version, environment)
	record, ok := s.versions[key]
	if !ok {
		return fmt.Errorf("unknown definition %q version %q", name, version)
	}
	s.definitions[name] = record
	s.active[definitionActiveKey(name, environment)] = ActiveDefinition{Name: name, Version: version, Environment: firstEnv(environment), ActivatedAt: nowUTC()}
	return nil
}

func (s *MemoryStore) GetActiveDefinition(_ context.Context, name, environment string) (DefinitionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	active, ok := s.active[definitionActiveKey(name, environment)]
	if ok {
		if record, found := s.versions[definitionVersionKey(name, active.Version, active.Environment)]; found {
			return record, nil
		}
	}
	record, ok := s.definitions[name]
	if !ok {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
	}
	return record, nil
}

func (s *MemoryStore) SaveReport(_ context.Context, report ReportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

func (s *MemoryStore) ListReportsQuery(_ context.Context, opts ListOptions) ([]ReportRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ApplyListOptions(s.reports, opts, func(report ReportRecord) bool {
		return (opts.Kind == "" || report.Kind == opts.Kind) &&
			(opts.Definition == "" || report.Definition == opts.Definition) &&
			inRange(report.CreatedAt, opts)
	}), nil
}

func (s *MemoryStore) AppendAudit(_ context.Context, envelope audit.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, envelope)
	return nil
}

func (s *MemoryStore) LastAuditHash(_ context.Context) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.audits) == 0 {
		return "", nil
	}
	return s.audits[len(s.audits)-1].Hash, nil
}

func (s *MemoryStore) ListAudits(_ context.Context) ([]audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]audit.Envelope(nil), s.audits...)
	return out, nil
}

func (s *MemoryStore) ListAuditsQuery(_ context.Context, opts ListOptions) ([]audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ApplyListOptions(s.audits, opts, func(envelope audit.Envelope) bool {
		return (opts.Operation == "" || envelope.Operation == opts.Operation) &&
			(opts.Definition == "" || envelope.Definition == opts.Definition) &&
			(opts.Subject == "" || envelope.Subject == opts.Subject) &&
			inRange(envelope.CompletedAt, opts)
	}), nil
}

func (s *MemoryStore) GetAudit(_ context.Context, id string) (audit.Envelope, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, envelope := range s.audits {
		if envelope.ID == id {
			return envelope, nil
		}
	}
	return audit.Envelope{}, fmt.Errorf("unknown audit %q", id)
}

func (s *MemoryStore) SaveWorkflowRun(_ context.Context, run WorkflowRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workflows[run.ID] = run
	return nil
}

func (s *MemoryStore) GetWorkflowRun(_ context.Context, id string) (WorkflowRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.workflows[id]
	if !ok {
		return WorkflowRunRecord{}, fmt.Errorf("unknown workflow run %q", id)
	}
	return run, nil
}

func (s *MemoryStore) ListWorkflowRuns(_ context.Context, opts ListOptions) ([]WorkflowRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]WorkflowRunRecord, 0, len(s.workflows))
	for _, run := range s.workflows {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].CreatedAt.Before(runs[j].CreatedAt) })
	return ApplyListOptions(runs, opts, func(run WorkflowRunRecord) bool {
		return (opts.Definition == "" || run.Definition == opts.Definition) &&
			(opts.Environment == "" || run.Environment == opts.Environment) &&
			inRange(run.UpdatedAt, opts)
	}), nil
}
