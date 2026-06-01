package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oarkflow/bcl/condition/pkg/audit"
)

type FileStore struct {
	root string
	mu   sync.Mutex
}

func NewFileStore(root string) (*FileStore, error) {
	if strings.TrimSpace(root) == "" {
		root = ".condition"
	}
	s := &FileStore{root: root}
	for _, dir := range []string{s.definitionsDir("default"), s.reportsDir("default")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.auditPath("default")), 0o755); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) SaveDefinition(ctx context.Context, record DefinitionRecord) error {
	if err := s.SaveDefinitionVersion(ctx, record); err != nil {
		return err
	}
	return s.ActivateDefinition(ctx, record.Name, record.Version, record.Environment)
}

func (s *FileStore) GetDefinition(ctx context.Context, name string) (DefinitionRecord, error) {
	return s.GetActiveDefinition(ctx, name, "")
}

func (s *FileStore) getLegacyDefinition(name string) (DefinitionRecord, error) {
	var record DefinitionRecord
	if err := readJSONFile(filepath.Join(s.legacyDefinitionsDir(), safeName(name)+".json"), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
		}
		return DefinitionRecord{}, err
	}
	return record, nil
}

func (s *FileStore) ListDefinitions(ctx context.Context) ([]DefinitionRecord, error) {
	tenant := TenantFromContext(ctx)
	entries, err := os.ReadDir(s.definitionsDir(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if tenant == "default" {
				entries, err = os.ReadDir(s.legacyDefinitionsDir())
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						return nil, nil
					}
					return nil, err
				}
			} else {
				return nil, nil
			}
		}
		if err != nil {
			return nil, err
		}
	}
	var out []DefinitionRecord
	for _, entry := range entries {
		if !entry.IsDir() {
			if filepath.Ext(entry.Name()) == ".json" {
				var record DefinitionRecord
				if err := readJSONFile(filepath.Join(s.definitionsDir(tenant), entry.Name()), &record); err == nil {
					record.TenantID = firstTenant(record.TenantID)
					out = append(out, record)
				}
			}
			continue
		}
		record, err := s.GetActiveDefinition(ctx, entry.Name(), "")
		if err == nil {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *FileStore) SaveDefinitionVersion(ctx context.Context, record DefinitionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Environment == "" {
		record.Environment = "development"
	}
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	return writeJSONFile(s.definitionVersionPath(record.TenantID, record.Name, record.Version, record.Environment), record)
}

func (s *FileStore) GetDefinitionVersion(ctx context.Context, name, version, environment string) (DefinitionRecord, error) {
	var record DefinitionRecord
	tenant := TenantFromContext(ctx)
	if err := readJSONFile(s.definitionVersionPath(tenant, name, version, environment), &record); err != nil {
		if tenant == "default" {
			if legacyErr := readJSONFile(s.legacyDefinitionVersionPath(name, version, environment), &record); legacyErr == nil {
				record.TenantID = "default"
				return record, nil
			}
		}
		if errors.Is(err, os.ErrNotExist) {
			return DefinitionRecord{}, fmt.Errorf("unknown definition %q version %q", name, version)
		}
		return DefinitionRecord{}, err
	}
	return record, nil
}

func (s *FileStore) ListDefinitionVersions(ctx context.Context, name, environment string) ([]DefinitionRecord, error) {
	tenant := TenantFromContext(ctx)
	dir := filepath.Join(s.definitionsDir(tenant), safeName(name))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if tenant == "default" {
				if legacy, legacyErr := s.getLegacyDefinition(name); legacyErr == nil {
					legacy.TenantID = "default"
					return []DefinitionRecord{legacy}, nil
				}
			}
			return nil, nil
		}
		return nil, err
	}
	var out []DefinitionRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.HasPrefix(entry.Name(), "active-") {
			continue
		}
		var record DefinitionRecord
		if err := readJSONFile(filepath.Join(dir, entry.Name()), &record); err != nil {
			return nil, err
		}
		record.TenantID = firstTenant(record.TenantID)
		if environment == "" || record.Environment == firstEnv(environment) {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PublishedAt.Before(out[j].PublishedAt) })
	return out, nil
}

func (s *FileStore) ActivateDefinition(ctx context.Context, name, version, environment string) error {
	record, err := s.GetDefinitionVersion(ctx, name, version, environment)
	if err != nil {
		return err
	}
	active := ActiveDefinition{Name: name, Version: version, Environment: firstEnv(environment), ActivatedAt: nowUTC()}
	active.TenantID = TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSONFile(s.activePath(active.TenantID, name, environment), active); err != nil {
		return err
	}
	return writeJSONFile(s.definitionPath(active.TenantID, name), record)
}

func (s *FileStore) GetActiveDefinition(ctx context.Context, name, environment string) (DefinitionRecord, error) {
	var active ActiveDefinition
	tenant := TenantFromContext(ctx)
	if err := readJSONFile(s.activePath(tenant, name, environment), &active); err == nil {
		ctx = ContextWithTenant(ctx, active.TenantID)
		return s.GetDefinitionVersion(ctx, name, active.Version, active.Environment)
	}
	if tenant == "default" {
		record, err := s.getLegacyDefinition(name)
		record.TenantID = "default"
		return record, err
	}
	return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
}

func (s *FileStore) SaveReport(ctx context.Context, report ReportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	report.TenantID = firstTenant(firstNonEmpty(report.TenantID, TenantFromContext(ctx)))
	return writeJSONFile(filepath.Join(s.reportsDir(report.TenantID), safeName(report.Kind+"-"+report.ID)+".json"), report)
}

func (s *FileStore) ListReports(_ context.Context, kind string) ([]ReportRecord, error) {
	return s.ListReportsQuery(context.Background(), ListOptions{Kind: kind})
}

func (s *FileStore) ListReportsQuery(ctx context.Context, opts ListOptions) ([]ReportRecord, error) {
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	entries, err := os.ReadDir(s.reportsDir(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []ReportRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var report ReportRecord
		if err := readJSONFile(filepath.Join(s.reportsDir(tenant), entry.Name()), &report); err != nil {
			return nil, err
		}
		if firstTenant(report.TenantID) == tenant && (opts.Kind == "" || report.Kind == opts.Kind) && (opts.Definition == "" || report.Definition == opts.Definition) && inRange(report.CreatedAt, opts) {
			out = append(out, report)
		}
	}
	return ApplyListOptions(out, opts, nil), nil
}

func (s *FileStore) AppendAudit(ctx context.Context, envelope audit.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	envelope.TenantID = firstTenant(firstNonEmpty(envelope.TenantID, TenantFromContext(ctx)))
	f, err := os.OpenFile(s.auditPath(envelope.TenantID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) LastAuditHash(ctx context.Context) (string, error) {
	records, err := s.ListAudits(ctx)
	if err != nil || len(records) == 0 {
		return "", err
	}
	return records[len(records)-1].Hash, nil
}

func (s *FileStore) ListAudits(ctx context.Context) ([]audit.Envelope, error) {
	return s.ListAuditsQuery(ctx, ListOptions{})
}

func (s *FileStore) ListAuditsQuery(ctx context.Context, opts ListOptions) ([]audit.Envelope, error) {
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	f, err := os.Open(s.auditPath(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []audit.Envelope
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var envelope audit.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			return nil, err
		}
		if firstTenant(envelope.TenantID) == tenant &&
			(opts.Operation == "" || envelope.Operation == opts.Operation) &&
			(opts.Definition == "" || envelope.Definition == opts.Definition) &&
			(opts.Subject == "" || envelope.Subject == opts.Subject) &&
			inRange(envelope.CompletedAt, opts) {
			out = append(out, envelope)
		}
	}
	return ApplyListOptions(out, opts, nil), scanner.Err()
}

func (s *FileStore) GetAudit(ctx context.Context, id string) (audit.Envelope, error) {
	records, err := s.ListAudits(ctx)
	if err != nil {
		return audit.Envelope{}, err
	}
	for _, record := range records {
		if record.ID == id {
			return record, nil
		}
	}
	return audit.Envelope{}, fmt.Errorf("unknown audit %q", id)
}

func (s *FileStore) SaveWorkflowRun(ctx context.Context, run WorkflowRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.TenantID = firstTenant(firstNonEmpty(run.TenantID, TenantFromContext(ctx)))
	return writeJSONFile(filepath.Join(s.workflowsDir(run.TenantID), safeName(run.ID)+".json"), run)
}

func (s *FileStore) GetWorkflowRun(ctx context.Context, id string) (WorkflowRunRecord, error) {
	var run WorkflowRunRecord
	tenant := TenantFromContext(ctx)
	if err := readJSONFile(filepath.Join(s.workflowsDir(tenant), safeName(id)+".json"), &run); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WorkflowRunRecord{}, fmt.Errorf("unknown workflow run %q", id)
		}
		return WorkflowRunRecord{}, err
	}
	return run, nil
}

func (s *FileStore) ListWorkflowRuns(ctx context.Context, opts ListOptions) ([]WorkflowRunRecord, error) {
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	entries, err := os.ReadDir(s.workflowsDir(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []WorkflowRunRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var run WorkflowRunRecord
		if err := readJSONFile(filepath.Join(s.workflowsDir(tenant), entry.Name()), &run); err != nil {
			return nil, err
		}
		if firstTenant(run.TenantID) == tenant && (opts.Definition == "" || run.Definition == opts.Definition) && (opts.Environment == "" || run.Environment == opts.Environment) && inRange(run.UpdatedAt, opts) {
			out = append(out, run)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return ApplyListOptions(out, opts, nil), nil
}

func (s *FileStore) AppendChainEvent(ctx context.Context, event ChainEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.TenantID = firstTenant(firstNonEmpty(event.TenantID, TenantFromContext(ctx)))
	if event.CreatedAt.IsZero() {
		event.CreatedAt = nowUTC()
	}
	f, err := os.OpenFile(s.chainEventsPath(event.TenantID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		if mkErr := os.MkdirAll(filepath.Dir(s.chainEventsPath(event.TenantID)), 0o755); mkErr != nil {
			return mkErr
		}
		f, err = os.OpenFile(s.chainEventsPath(event.TenantID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
	}
	defer f.Close()
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *FileStore) QueryChainEvents(ctx context.Context, query ChainEventQuery) ([]ChainEventRecord, error) {
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	f, err := os.Open(s.chainEventsPath(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	now := nowUTC()
	var out []ChainEventRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event ChainEventRecord
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		if firstTenant(event.TenantID) != tenant ||
			(query.Definition != "" && event.Definition != query.Definition) ||
			(query.Environment != "" && event.Environment != query.Environment) ||
			(query.Chain != "" && event.Chain != query.Chain) ||
			(query.Watch != "" && event.Watch != query.Watch) ||
			(query.EntityKey != "" && event.EntityKey != query.EntityKey) ||
			(query.EventType != "" && event.EventType != query.EventType) ||
			(query.Since != nil && event.CreatedAt.Before(*query.Since)) ||
			(query.Until != nil && event.CreatedAt.After(*query.Until)) ||
			(!query.IncludeExpired && event.ExpiresAt != nil && !event.ExpiresAt.After(now)) {
			continue
		}
		out = append(out, event)
	}
	if scanner.Err() != nil {
		return nil, scanner.Err()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if query.Limit > 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	}
	return out, nil
}

func (s *FileStore) GetChainState(ctx context.Context, chain, watch, entityKey string) (ChainStateRecord, error) {
	var state ChainStateRecord
	tenant := TenantFromContext(ctx)
	if err := readJSONFile(s.chainStatePath(tenant, chain, watch, entityKey), &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ChainStateRecord{}, fmt.Errorf("unknown watch state %q/%q/%q", chain, watch, entityKey)
		}
		return ChainStateRecord{}, err
	}
	if state.ExpiresAt != nil && !state.ExpiresAt.After(nowUTC()) {
		return ChainStateRecord{}, fmt.Errorf("unknown watch state %q/%q/%q", chain, watch, entityKey)
	}
	return state, nil
}

func (s *FileStore) ListChainStates(ctx context.Context, query ChainStateQuery) ([]ChainStateRecord, error) {
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	root := s.chainStatesDir(tenant)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	now := nowUTC()
	var out []ChainStateRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var state ChainStateRecord
		if err := readJSONFile(filepath.Join(root, entry.Name()), &state); err != nil {
			return nil, err
		}
		if firstTenant(state.TenantID) != tenant ||
			(query.Definition != "" && state.Definition != query.Definition) ||
			(query.Environment != "" && state.Environment != query.Environment) ||
			(query.Chain != "" && state.Chain != query.Chain) ||
			(query.Watch != "" && state.Watch != query.Watch) ||
			(query.EntityKey != "" && state.EntityKey != query.EntityKey) ||
			(!query.IncludeExpired && state.ExpiresAt != nil && !state.ExpiresAt.After(now)) {
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

func (s *FileStore) UpsertChainState(ctx context.Context, state ChainStateRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.TenantID = firstTenant(firstNonEmpty(state.TenantID, TenantFromContext(ctx)))
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = nowUTC()
	}
	return writeJSONFile(s.chainStatePath(state.TenantID, state.Chain, state.Watch, state.EntityKey), state)
}

func (s *FileStore) DeleteExpiredChainStates(ctx context.Context, now time.Time) error {
	tenant := TenantFromContext(ctx)
	root := s.chainStatesDir(tenant)
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(root, entry.Name())
		var state ChainStateRecord
		if err := readJSONFile(path, &state); err != nil {
			return err
		}
		if state.ExpiresAt != nil && !state.ExpiresAt.After(now) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func (s *FileStore) SaveActionDelivery(ctx context.Context, record ActionDeliveryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	if record.CreatedAt.IsZero() {
		record.CreatedAt = nowUTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	return writeJSONFile(filepath.Join(s.actionsDir(record.TenantID), safeName(record.ID)+".json"), record)
}

func (s *FileStore) ListActionDeliveries(ctx context.Context, query ActionDeliveryQuery) ([]ActionDeliveryRecord, error) {
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	entries, err := os.ReadDir(s.actionsDir(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []ActionDeliveryRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record ActionDeliveryRecord
		if err := readJSONFile(filepath.Join(s.actionsDir(tenant), entry.Name()), &record); err != nil {
			return nil, err
		}
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

func (s *FileStore) UpsertIncident(ctx context.Context, record IncidentRecord) error {
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
	return writeJSONFile(filepath.Join(s.incidentsDir(record.TenantID), safeName(record.ID)+".json"), record)
}

func (s *FileStore) ListIncidents(ctx context.Context, query IncidentQuery) ([]IncidentRecord, error) {
	tenant := firstTenant(firstNonEmpty(query.TenantID, TenantFromContext(ctx)))
	entries, err := os.ReadDir(s.incidentsDir(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []IncidentRecord
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var record IncidentRecord
		if err := readJSONFile(filepath.Join(s.incidentsDir(tenant), entry.Name()), &record); err != nil {
			return nil, err
		}
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

func (s *FileStore) Compact(ctx context.Context, req RetentionRequest) (RetentionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := firstTenant(firstNonEmpty(req.TenantID, TenantFromContext(ctx)))
	before := req.Before.UTC()
	result := RetentionResult{}
	stateEntries, err := os.ReadDir(s.chainStatesDir(tenant))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	for _, entry := range stateEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.chainStatesDir(tenant), entry.Name())
		var state ChainStateRecord
		if err := readJSONFile(path, &state); err != nil {
			return result, err
		}
		if firstTenant(state.TenantID) != tenant ||
			(req.Definition != "" && state.Definition != req.Definition) ||
			(req.Environment != "" && state.Environment != req.Environment) ||
			state.ExpiresAt == nil || state.ExpiresAt.After(before) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return result, err
		}
		result.ChainStates++
	}
	events, err := s.readAllChainEvents(tenant)
	if err != nil {
		return result, err
	}
	keptEvents := events[:0]
	for _, event := range events {
		if firstTenant(event.TenantID) == tenant &&
			(req.Definition == "" || event.Definition == req.Definition) &&
			(req.Environment == "" || event.Environment == req.Environment) &&
			!event.CreatedAt.After(before) {
			result.ChainEvents++
			continue
		}
		keptEvents = append(keptEvents, event)
	}
	if result.ChainEvents > 0 {
		if err := s.writeAllChainEvents(tenant, keptEvents); err != nil {
			return result, err
		}
	}
	if count, err := s.compactJSONRecords(s.actionsDir(tenant), func(path string) (bool, error) {
		var record ActionDeliveryRecord
		if err := readJSONFile(path, &record); err != nil {
			return false, err
		}
		if firstTenant(record.TenantID) != tenant ||
			(req.Definition != "" && record.Definition != req.Definition) ||
			(req.Environment != "" && record.Environment != req.Environment) ||
			record.CreatedAt.After(before) {
			return false, nil
		}
		if !req.DeleteActiveDeliveries && (record.Status == "retry_scheduled" || record.Status == "pending") {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return result, err
	} else {
		result.ActionDeliveries = count
	}
	if count, err := s.compactJSONRecords(s.incidentsDir(tenant), func(path string) (bool, error) {
		var record IncidentRecord
		if err := readJSONFile(path, &record); err != nil {
			return false, err
		}
		if firstTenant(record.TenantID) != tenant ||
			(req.Definition != "" && record.Definition != req.Definition) ||
			(req.Environment != "" && record.Environment != req.Environment) ||
			record.LastSeen.After(before) {
			return false, nil
		}
		if !req.DeleteOpenIncidents && record.Status == "open" {
			return false, nil
		}
		return true, nil
	}); err != nil {
		return result, err
	} else {
		result.Incidents = count
	}
	return result, nil
}

func (s *FileStore) readAllChainEvents(tenant string) ([]ChainEventRecord, error) {
	f, err := os.Open(s.chainEventsPath(tenant))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []ChainEventRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var event ChainEventRecord
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, scanner.Err()
}

func (s *FileStore) writeAllChainEvents(tenant string, events []ChainEventRecord) error {
	path := s.chainEventsPath(tenant)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, event := range events {
		payload, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(payload, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (s *FileStore) compactJSONRecords(dir string, remove func(path string) (bool, error)) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		ok, err := remove(path)
		if err != nil {
			return count, err
		}
		if !ok {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return count, err
		}
		count++
	}
	return count, nil
}

func (s *FileStore) Stats(ctx context.Context) (StatsRecord, error) {
	tenant := TenantFromContext(ctx)
	stats := StatsRecord{}
	stats.Definitions = countJSONFiles(s.definitionsDir(tenant))
	events, err := s.readAllChainEvents(tenant)
	if err != nil {
		return stats, err
	}
	stats.ChainEvents = len(events)
	stats.ChainStates = countJSONFiles(s.chainStatesDir(tenant))
	stats.ActionDeliveries = countJSONFiles(s.actionsDir(tenant))
	stats.Incidents = countJSONFiles(s.incidentsDir(tenant))
	return stats, nil
}

func countJSONFiles(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count
}

func (s *FileStore) legacyDefinitionsDir() string { return filepath.Join(s.root, "definitions") }
func (s *FileStore) definitionsDir(tenant string) string {
	return filepath.Join(s.root, "definitions", safeName(firstTenant(tenant)))
}
func (s *FileStore) reportsDir(tenant string) string {
	return filepath.Join(s.root, "reports", safeName(firstTenant(tenant)))
}
func (s *FileStore) auditPath(tenant string) string {
	return filepath.Join(s.root, "audit", safeName(firstTenant(tenant))+".jsonl")
}
func (s *FileStore) workflowsDir(tenant string) string {
	return filepath.Join(s.root, "workflows", safeName(firstTenant(tenant)))
}
func (s *FileStore) chainEventsPath(tenant string) string {
	return filepath.Join(s.root, "chain-events", safeName(firstTenant(tenant))+".jsonl")
}
func (s *FileStore) chainStatesDir(tenant string) string {
	return filepath.Join(s.root, "chain-states", safeName(firstTenant(tenant)))
}
func (s *FileStore) chainStatePath(tenant, chain, watch, entityKey string) string {
	return filepath.Join(s.chainStatesDir(tenant), safeName(chain+"-"+watch+"-"+entityKey)+".json")
}
func (s *FileStore) actionsDir(tenant string) string {
	return filepath.Join(s.root, "actions", safeName(firstTenant(tenant)))
}
func (s *FileStore) incidentsDir(tenant string) string {
	return filepath.Join(s.root, "incidents", safeName(firstTenant(tenant)))
}
func (s *FileStore) definitionPath(tenant, name string) string {
	return filepath.Join(s.definitionsDir(tenant), safeName(name)+".json")
}
func (s *FileStore) definitionVersionPath(tenant, name, version, environment string) string {
	return filepath.Join(s.definitionsDir(tenant), safeName(name), safeName(firstEnv(environment)+"-"+version)+".json")
}
func (s *FileStore) legacyDefinitionVersionPath(name, version, environment string) string {
	return filepath.Join(s.legacyDefinitionsDir(), safeName(name), safeName(firstEnv(environment)+"-"+version)+".json")
}
func (s *FileStore) activePath(tenant, name, environment string) string {
	return filepath.Join(s.definitionsDir(tenant), safeName(name), "active-"+safeName(firstEnv(environment))+".json")
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o644)
}

func readJSONFile(path string, v any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, v)
}

func safeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unnamed"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
