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

	"github.com/oarkflow/condition/pkg/audit"
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
	for _, dir := range []string{s.definitionsDir(), s.reportsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(s.auditPath()), 0o755); err != nil {
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
	if err := readJSONFile(s.definitionPath(name), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
		}
		return DefinitionRecord{}, err
	}
	return record, nil
}

func (s *FileStore) ListDefinitions(ctx context.Context) ([]DefinitionRecord, error) {
	entries, err := os.ReadDir(s.definitionsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []DefinitionRecord
	for _, entry := range entries {
		if !entry.IsDir() {
			if filepath.Ext(entry.Name()) == ".json" {
				var record DefinitionRecord
				if err := readJSONFile(filepath.Join(s.definitionsDir(), entry.Name()), &record); err == nil {
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

func (s *FileStore) SaveDefinitionVersion(_ context.Context, record DefinitionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.Environment == "" {
		record.Environment = "development"
	}
	return writeJSONFile(s.definitionVersionPath(record.Name, record.Version, record.Environment), record)
}

func (s *FileStore) GetDefinitionVersion(_ context.Context, name, version, environment string) (DefinitionRecord, error) {
	var record DefinitionRecord
	if err := readJSONFile(s.definitionVersionPath(name, version, environment), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefinitionRecord{}, fmt.Errorf("unknown definition %q version %q", name, version)
		}
		return DefinitionRecord{}, err
	}
	return record, nil
}

func (s *FileStore) ListDefinitionVersions(_ context.Context, name, environment string) ([]DefinitionRecord, error) {
	dir := filepath.Join(s.definitionsDir(), safeName(name))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if legacy, legacyErr := s.getLegacyDefinition(name); legacyErr == nil {
				return []DefinitionRecord{legacy}, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSONFile(s.activePath(name, environment), active); err != nil {
		return err
	}
	return writeJSONFile(s.definitionPath(name), record)
}

func (s *FileStore) GetActiveDefinition(ctx context.Context, name, environment string) (DefinitionRecord, error) {
	var active ActiveDefinition
	if err := readJSONFile(s.activePath(name, environment), &active); err == nil {
		return s.GetDefinitionVersion(ctx, name, active.Version, active.Environment)
	}
	return s.getLegacyDefinition(name)
}

func (s *FileStore) SaveReport(_ context.Context, report ReportRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(filepath.Join(s.reportsDir(), safeName(report.Kind+"-"+report.ID)+".json"), report)
}

func (s *FileStore) ListReports(_ context.Context, kind string) ([]ReportRecord, error) {
	return s.ListReportsQuery(context.Background(), ListOptions{Kind: kind})
}

func (s *FileStore) ListReportsQuery(_ context.Context, opts ListOptions) ([]ReportRecord, error) {
	entries, err := os.ReadDir(s.reportsDir())
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
		if err := readJSONFile(filepath.Join(s.reportsDir(), entry.Name()), &report); err != nil {
			return nil, err
		}
		if (opts.Kind == "" || report.Kind == opts.Kind) && (opts.Definition == "" || report.Definition == opts.Definition) && inRange(report.CreatedAt, opts) {
			out = append(out, report)
		}
	}
	return ApplyListOptions(out, opts, nil), nil
}

func (s *FileStore) AppendAudit(_ context.Context, envelope audit.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.auditPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

func (s *FileStore) ListAudits(_ context.Context) ([]audit.Envelope, error) {
	return s.ListAuditsQuery(context.Background(), ListOptions{})
}

func (s *FileStore) ListAuditsQuery(_ context.Context, opts ListOptions) ([]audit.Envelope, error) {
	f, err := os.Open(s.auditPath())
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
		if (opts.Operation == "" || envelope.Operation == opts.Operation) &&
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

func (s *FileStore) SaveWorkflowRun(_ context.Context, run WorkflowRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSONFile(filepath.Join(s.workflowsDir(), safeName(run.ID)+".json"), run)
}

func (s *FileStore) GetWorkflowRun(_ context.Context, id string) (WorkflowRunRecord, error) {
	var run WorkflowRunRecord
	if err := readJSONFile(filepath.Join(s.workflowsDir(), safeName(id)+".json"), &run); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WorkflowRunRecord{}, fmt.Errorf("unknown workflow run %q", id)
		}
		return WorkflowRunRecord{}, err
	}
	return run, nil
}

func (s *FileStore) ListWorkflowRuns(_ context.Context, opts ListOptions) ([]WorkflowRunRecord, error) {
	entries, err := os.ReadDir(s.workflowsDir())
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
		if err := readJSONFile(filepath.Join(s.workflowsDir(), entry.Name()), &run); err != nil {
			return nil, err
		}
		if (opts.Definition == "" || run.Definition == opts.Definition) && (opts.Environment == "" || run.Environment == opts.Environment) && inRange(run.UpdatedAt, opts) {
			out = append(out, run)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return ApplyListOptions(out, opts, nil), nil
}

func (s *FileStore) definitionsDir() string { return filepath.Join(s.root, "definitions") }
func (s *FileStore) reportsDir() string     { return filepath.Join(s.root, "reports") }
func (s *FileStore) auditPath() string      { return filepath.Join(s.root, "audit", "audit.jsonl") }
func (s *FileStore) workflowsDir() string   { return filepath.Join(s.root, "workflows") }
func (s *FileStore) definitionPath(name string) string {
	return filepath.Join(s.definitionsDir(), safeName(name)+".json")
}
func (s *FileStore) definitionVersionPath(name, version, environment string) string {
	return filepath.Join(s.definitionsDir(), safeName(name), safeName(firstEnv(environment)+"-"+version)+".json")
}
func (s *FileStore) activePath(name, environment string) string {
	return filepath.Join(s.definitionsDir(), safeName(name), "active-"+safeName(firstEnv(environment))+".json")
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
