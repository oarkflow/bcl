package storage

import (
	"context"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/bcl/condition/pkg/audit"
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

type ChainEventRecord struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id,omitempty"`
	Definition     string         `json:"definition,omitempty"`
	Version        string         `json:"version,omitempty"`
	Environment    string         `json:"environment,omitempty"`
	Chain          string         `json:"chain"`
	Watch          string         `json:"watch,omitempty"`
	EntityKey      string         `json:"entity_key"`
	EventType      string         `json:"event_type"`
	SourceDecision string         `json:"source_decision,omitempty"`
	ReasonCode     string         `json:"reason_code,omitempty"`
	Severity       string         `json:"severity,omitempty"`
	Attributes     map[string]any `json:"attributes,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
}

type ChainStateRecord struct {
	TenantID    string         `json:"tenant_id,omitempty"`
	Definition  string         `json:"definition,omitempty"`
	Version     string         `json:"version,omitempty"`
	Environment string         `json:"environment,omitempty"`
	Chain       string         `json:"chain"`
	Watch       string         `json:"watch"`
	EntityKey   string         `json:"entity_key"`
	Counters    map[string]int `json:"counters,omitempty"`
	Step        string         `json:"step,omitempty"`
	Action      string         `json:"action,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	UpdatedAt   time.Time      `json:"updated_at"`
	ExpiresAt   *time.Time     `json:"expires_at,omitempty"`
}

type ActionDeliveryRecord struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id,omitempty"`
	Definition  string         `json:"definition,omitempty"`
	Version     string         `json:"version,omitempty"`
	Environment string         `json:"environment,omitempty"`
	Lifecycle   string         `json:"lifecycle,omitempty"`
	EntityKey   string         `json:"entity_key,omitempty"`
	Action      string         `json:"action"`
	Sink        string         `json:"sink,omitempty"`
	Status      string         `json:"status,omitempty"`
	Handled     bool           `json:"handled,omitempty"`
	Attempts    int            `json:"attempts,omitempty"`
	MaxAttempts int            `json:"max_attempts,omitempty"`
	NextAttempt *time.Time     `json:"next_attempt,omitempty"`
	Error       string         `json:"error,omitempty"`
	ReasonCode  string         `json:"reason_code,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type IncidentRecord struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id,omitempty"`
	Definition  string         `json:"definition,omitempty"`
	Environment string         `json:"environment,omitempty"`
	GroupKey    string         `json:"group_key"`
	Status      string         `json:"status,omitempty"`
	Action      string         `json:"action,omitempty"`
	Severity    string         `json:"severity,omitempty"`
	ReasonCode  string         `json:"reason_code,omitempty"`
	Count       int            `json:"count"`
	FirstSeen   time.Time      `json:"first_seen"`
	LastSeen    time.Time      `json:"last_seen"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type ChainEventQuery struct {
	TenantID       string     `json:"tenant_id,omitempty"`
	Definition     string     `json:"definition,omitempty"`
	Environment    string     `json:"environment,omitempty"`
	Chain          string     `json:"chain,omitempty"`
	Watch          string     `json:"watch,omitempty"`
	EntityKey      string     `json:"entity_key,omitempty"`
	EventType      string     `json:"event_type,omitempty"`
	Since          *time.Time `json:"since,omitempty"`
	Until          *time.Time `json:"until,omitempty"`
	IncludeExpired bool       `json:"include_expired,omitempty"`
	Limit          int        `json:"limit,omitempty"`
}

type ChainStateQuery struct {
	TenantID       string `json:"tenant_id,omitempty"`
	Definition     string `json:"definition,omitempty"`
	Environment    string `json:"environment,omitempty"`
	Chain          string `json:"chain,omitempty"`
	Watch          string `json:"watch,omitempty"`
	EntityKey      string `json:"entity_key,omitempty"`
	IncludeExpired bool   `json:"include_expired,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type ActionDeliveryQuery struct {
	TenantID    string `json:"tenant_id,omitempty"`
	Definition  string `json:"definition,omitempty"`
	Environment string `json:"environment,omitempty"`
	Action      string `json:"action,omitempty"`
	Status      string `json:"status,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type IncidentQuery struct {
	TenantID    string `json:"tenant_id,omitempty"`
	Definition  string `json:"definition,omitempty"`
	Environment string `json:"environment,omitempty"`
	Status      string `json:"status,omitempty"`
	Action      string `json:"action,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type RetentionRequest struct {
	TenantID               string    `json:"tenant_id,omitempty"`
	Definition             string    `json:"definition,omitempty"`
	Environment            string    `json:"environment,omitempty"`
	Before                 time.Time `json:"before"`
	DeleteOpenIncidents    bool      `json:"delete_open_incidents,omitempty"`
	DeleteActiveDeliveries bool      `json:"delete_active_deliveries,omitempty"`
}

type RetentionResult struct {
	ChainEvents      int `json:"chain_events"`
	ChainStates      int `json:"chain_states"`
	ActionDeliveries int `json:"action_deliveries"`
	Incidents        int `json:"incidents"`
}

type StatsRecord struct {
	Definitions      int `json:"definitions"`
	ChainEvents      int `json:"chain_events"`
	ChainStates      int `json:"chain_states"`
	ActionDeliveries int `json:"action_deliveries"`
	Incidents        int `json:"incidents"`
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
	AppendChainEvent(ctx context.Context, event ChainEventRecord) error
	QueryChainEvents(ctx context.Context, query ChainEventQuery) ([]ChainEventRecord, error)
	GetChainState(ctx context.Context, chain, watch, entityKey string) (ChainStateRecord, error)
	ListChainStates(ctx context.Context, query ChainStateQuery) ([]ChainStateRecord, error)
	UpsertChainState(ctx context.Context, state ChainStateRecord) error
	DeleteExpiredChainStates(ctx context.Context, now time.Time) error
	SaveActionDelivery(ctx context.Context, record ActionDeliveryRecord) error
	ListActionDeliveries(ctx context.Context, query ActionDeliveryQuery) ([]ActionDeliveryRecord, error)
	UpsertIncident(ctx context.Context, record IncidentRecord) error
	ListIncidents(ctx context.Context, query IncidentQuery) ([]IncidentRecord, error)
	Compact(ctx context.Context, req RetentionRequest) (RetentionResult, error)
	Stats(ctx context.Context) (StatsRecord, error)
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
