package condition

import (
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/pkg/audit"
	"github.com/oarkflow/condition/pkg/storage"
)

type Config struct {
	Environment               string        `json:"environment,omitempty"`
	DefaultTenant             string        `json:"default_tenant,omitempty"`
	RequestTimeout            time.Duration `json:"request_timeout,omitempty"`
	MaxRequestBytes           int64         `json:"max_request_bytes,omitempty"`
	StrictValidation          bool          `json:"strict_validation,omitempty"`
	StrictEvaluation          bool          `json:"strict_evaluation,omitempty"`
	RequireActivationApproval bool          `json:"require_activation_approval,omitempty"`
	RequireTests              bool          `json:"require_tests,omitempty"`
	Runtime                   RuntimePolicy `json:"runtime,omitempty"`
}

type RuntimePolicy struct {
	AllowTime              bool          `json:"allow_time,omitempty"`
	FixedTime              string        `json:"fixed_time,omitempty"`
	AllowEnv               bool          `json:"allow_env,omitempty"`
	AllowedDatasetAdapters []string      `json:"allowed_dataset_adapters,omitempty"`
	AllowedHTTPHosts       []string      `json:"allowed_http_hosts,omitempty"`
	AllowedHTTPMethods     []string      `json:"allowed_http_methods,omitempty"`
	ExternalTimeout        time.Duration `json:"external_timeout,omitempty"`
}

type PublishRequest struct {
	TenantID     string         `json:"tenant_id,omitempty"`
	Name         string         `json:"name,omitempty"`
	Version      string         `json:"version,omitempty"`
	Environment  string         `json:"environment,omitempty"`
	Path         string         `json:"path,omitempty"`
	Source       string         `json:"source,omitempty"`
	BaseDir      string         `json:"base_dir,omitempty"`
	RunTests     bool           `json:"run_tests,omitempty"`
	Strict       bool           `json:"strict,omitempty"`
	RequireTests bool           `json:"require_tests,omitempty"`
	Bundle       string         `json:"bundle,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type PublishResponse struct {
	Definition storage.DefinitionRecord `json:"definition"`
	Tests      *TestReport              `json:"tests,omitempty"`
	Gates      *bcl.DecisionGateReport  `json:"gates,omitempty"`
	Audit      audit.Envelope           `json:"audit"`
}

type EvaluateRequest struct {
	TenantID               string         `json:"tenant_id,omitempty"`
	Decision               string         `json:"decision"`
	Input                  map[string]any `json:"input,omitempty"`
	Bundle                 string         `json:"bundle,omitempty"`
	IncludeGates           bool           `json:"include_gates,omitempty"`
	Counterfactuals        bool           `json:"counterfactuals,omitempty"`
	IncludeFeatures        bool           `json:"include_features,omitempty"`
	Strict                 bool           `json:"strict,omitempty"`
	Environment            string         `json:"environment,omitempty"`
	ShadowCandidateSource  string         `json:"shadow_candidate_source,omitempty"`
	ShadowCandidatePath    string         `json:"shadow_candidate_path,omitempty"`
	ShadowCandidateBaseDir string         `json:"shadow_candidate_base_dir,omitempty"`
}

type EvaluateResponse struct {
	Report   *bcl.DecisionPlatformReport `json:"report"`
	Shadow   *bcl.DecisionCompareReport  `json:"shadow,omitempty"`
	Workflow *WorkflowResult             `json:"workflow,omitempty"`
	Audit    audit.Envelope              `json:"audit"`
}

type TestReport struct {
	Passed      bool                         `json:"passed"`
	Scenarios   []bcl.DecisionScenarioResult `json:"scenarios,omitempty"`
	Gates       *bcl.DecisionGateReport      `json:"gates,omitempty"`
	Diagnostics []bcl.Diagnostic             `json:"diagnostics,omitempty"`
	Audit       *audit.Envelope              `json:"audit,omitempty"`
}

type ValidationRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Version      string `json:"version,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Path         string `json:"path,omitempty"`
	Source       string `json:"source,omitempty"`
	BaseDir      string `json:"base_dir,omitempty"`
	RunTests     bool   `json:"run_tests,omitempty"`
	Strict       bool   `json:"strict,omitempty"`
	RequireTests bool   `json:"require_tests,omitempty"`
	Bundle       string `json:"bundle,omitempty"`
}

type ValidationReport struct {
	Valid         bool                         `json:"valid"`
	Publishable   bool                         `json:"publishable"`
	Strict        bool                         `json:"strict,omitempty"`
	TestsRequired bool                         `json:"tests_required,omitempty"`
	Name          string                       `json:"name,omitempty"`
	Version       string                       `json:"version,omitempty"`
	Environment   string                       `json:"environment,omitempty"`
	Digest        string                       `json:"digest,omitempty"`
	Decisions     []string                     `json:"decisions,omitempty"`
	Tests         *TestReport                  `json:"tests,omitempty"`
	Gates         *bcl.DecisionGateReport      `json:"gates,omitempty"`
	Features      bcl.DecisionPlatformFeatures `json:"features,omitempty"`
	Diagnostics   []bcl.Diagnostic             `json:"diagnostics,omitempty"`
}

type ActivationResponse struct {
	Definition storage.DefinitionRecord `json:"definition"`
	Audit      audit.Envelope           `json:"audit"`
}

type ApprovalRequest struct {
	TenantID    string `json:"tenant_id,omitempty"`
	Environment string `json:"environment,omitempty"`
	ApprovedBy  string `json:"approved_by,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type LifecycleResponse struct {
	Definition storage.DefinitionRecord `json:"definition"`
	Audit      audit.Envelope           `json:"audit"`
}

type ProductionReadinessReport struct {
	Ready       bool            `json:"ready"`
	Environment string          `json:"environment,omitempty"`
	Checks      map[string]bool `json:"checks,omitempty"`
	Missing     []string        `json:"missing,omitempty"`
}

type DisableRequest struct {
	TenantID    string `json:"tenant_id,omitempty"`
	Version     string `json:"version,omitempty"`
	Environment string `json:"environment,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

type SimulationRequest struct {
	TenantID         string                  `json:"tenant_id,omitempty"`
	CandidateSource  string                  `json:"candidate_source,omitempty"`
	CandidatePath    string                  `json:"candidate_path,omitempty"`
	CandidateBaseDir string                  `json:"candidate_base_dir,omitempty"`
	Decision         string                  `json:"decision"`
	Dataset          string                  `json:"dataset,omitempty"`
	Cases            []bcl.DecisionBatchCase `json:"cases,omitempty"`
}

type SimulationResponse struct {
	Compare *bcl.DecisionCompareReport `json:"compare"`
	Audit   audit.Envelope             `json:"audit"`
}

type CanaryRequest struct {
	SimulationRequest
	MaxChangedCases int            `json:"max_changed_cases,omitempty"`
	RequireNoErrors bool           `json:"require_no_errors,omitempty"`
	Promote         bool           `json:"promote,omitempty"`
	PromoteVersion  string         `json:"promote_version,omitempty"`
	PromoteMetadata map[string]any `json:"promote_metadata,omitempty"`
}

type CanaryResponse struct {
	Passed       bool                       `json:"passed"`
	ChangedCases int                        `json:"changed_cases"`
	Compare      *bcl.DecisionCompareReport `json:"compare,omitempty"`
	Promotion    *PublishResponse           `json:"promotion,omitempty"`
	Audit        audit.Envelope             `json:"audit"`
}

type ReloadRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	RunTests bool   `json:"run_tests,omitempty"`
}

type ReloadResponse struct {
	Reloaded bool             `json:"reloaded"`
	KeptLast bool             `json:"kept_last_known_good"`
	Publish  *PublishResponse `json:"publish,omitempty"`
	Audit    audit.Envelope   `json:"audit"`
}

type WorkflowResult struct {
	Stage      string         `json:"stage,omitempty"`
	Assignment map[string]any `json:"assignment,omitempty"`
	Events     []string       `json:"events,omitempty"`
}

type WorkflowDefinition struct {
	ID     string          `json:"id"`
	Start  string          `json:"start,omitempty"`
	Stages []WorkflowStage `json:"stages,omitempty"`
}

type WorkflowStage struct {
	ID          string               `json:"id"`
	Assign      map[string]any       `json:"assign,omitempty"`
	SLA         string               `json:"sla,omitempty"`
	OnTimeout   string               `json:"on_timeout,omitempty"`
	Transitions []WorkflowTransition `json:"transitions,omitempty"`
}

type WorkflowTransition struct {
	ID        string         `json:"id,omitempty"`
	NextStage string         `json:"next_stage,omitempty"`
	When      map[string]any `json:"when,omitempty"`
	Events    []string       `json:"events,omitempty"`
}

type WorkflowRun struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id,omitempty"`
	Definition  string          `json:"definition"`
	Version     string          `json:"version,omitempty"`
	Environment string          `json:"environment,omitempty"`
	WorkflowID  string          `json:"workflow_id"`
	Stage       string          `json:"stage,omitempty"`
	Status      string          `json:"status,omitempty"`
	Input       map[string]any  `json:"input,omitempty"`
	Assignment  map[string]any  `json:"assignment,omitempty"`
	Events      []string        `json:"events,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Audit       *audit.Envelope `json:"audit,omitempty"`
}

type WorkflowRequest struct {
	TenantID    string         `json:"tenant_id,omitempty"`
	Input       map[string]any `json:"input,omitempty"`
	Environment string         `json:"environment,omitempty"`
}
