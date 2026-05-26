package storage

import (
	"context"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/audit"
)

type DefinitionRecord struct {
	TenantID    string               `json:"tenant_id,omitempty"`
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Environment string               `json:"environment,omitempty"`
	Source      string               `json:"source,omitempty"`
	SourcePath  string               `json:"source_path,omitempty"`
	Digest      string               `json:"digest"`
	Program     *bcl.DecisionProgram `json:"program,omitempty"`
	PublishedAt time.Time            `json:"published_at"`
	Metadata    map[string]any       `json:"metadata,omitempty"`
}

type ActiveDefinition struct {
	TenantID    string    `json:"tenant_id,omitempty"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Environment string    `json:"environment,omitempty"`
	ActivatedAt time.Time `json:"activated_at"`
}

type ReportRecord struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id,omitempty"`
	Kind       string         `json:"kind"`
	Definition string         `json:"definition,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type ListOptions struct {
	Limit       int        `json:"limit,omitempty"`
	Offset      int        `json:"offset,omitempty"`
	Since       *time.Time `json:"since,omitempty"`
	Until       *time.Time `json:"until,omitempty"`
	Operation   string     `json:"operation,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	Definition  string     `json:"definition,omitempty"`
	Subject     string     `json:"subject,omitempty"`
	Environment string     `json:"environment,omitempty"`
	TenantID    string     `json:"tenant_id,omitempty"`
}

type WorkflowRunRecord struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id,omitempty"`
	Definition  string         `json:"definition"`
	Version     string         `json:"version,omitempty"`
	Environment string         `json:"environment,omitempty"`
	WorkflowID  string         `json:"workflow_id"`
	Stage       string         `json:"stage,omitempty"`
	Status      string         `json:"status,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Assignment  map[string]any `json:"assignment,omitempty"`
	Events      []string       `json:"events,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Store interface {
	SaveDefinition(ctx context.Context, record DefinitionRecord) error
	GetDefinition(ctx context.Context, name string) (DefinitionRecord, error)
	ListDefinitions(ctx context.Context) ([]DefinitionRecord, error)
	SaveDefinitionVersion(ctx context.Context, record DefinitionRecord) error
	GetDefinitionVersion(ctx context.Context, name, version, environment string) (DefinitionRecord, error)
	ListDefinitionVersions(ctx context.Context, name, environment string) ([]DefinitionRecord, error)
	ActivateDefinition(ctx context.Context, name, version, environment string) error
	GetActiveDefinition(ctx context.Context, name, environment string) (DefinitionRecord, error)
	SaveReport(ctx context.Context, report ReportRecord) error
	ListReports(ctx context.Context, kind string) ([]ReportRecord, error)
	ListReportsQuery(ctx context.Context, opts ListOptions) ([]ReportRecord, error)
	AppendAudit(ctx context.Context, envelope audit.Envelope) error
	LastAuditHash(ctx context.Context) (string, error)
	ListAudits(ctx context.Context) ([]audit.Envelope, error)
	ListAuditsQuery(ctx context.Context, opts ListOptions) ([]audit.Envelope, error)
	GetAudit(ctx context.Context, id string) (audit.Envelope, error)
	SaveWorkflowRun(ctx context.Context, run WorkflowRunRecord) error
	GetWorkflowRun(ctx context.Context, id string) (WorkflowRunRecord, error)
	ListWorkflowRuns(ctx context.Context, opts ListOptions) ([]WorkflowRunRecord, error)
}

func ApplyListOptions[T any](items []T, opts ListOptions, include func(T) bool) []T {
	var filtered []T
	for _, item := range items {
		if include == nil || include(item) {
			filtered = append(filtered, item)
		}
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil
	}
	filtered = filtered[offset:]
	if opts.Limit > 0 && opts.Limit < len(filtered) {
		filtered = filtered[:opts.Limit]
	}
	return filtered
}
