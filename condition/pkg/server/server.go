package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oarkflow/authz"
	authzmw "github.com/oarkflow/authz/middleware"
	authzstores "github.com/oarkflow/authz/stores"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

type Server struct {
	service *condition.Service
	authz   *authz.Engine
	maxBody int64
	timeout time.Duration
	metrics *Metrics
	limiter *rateLimiter
	logf    func(format string, args ...any)
}

type Option func(*Server)

func WithAuthzEngine(engine *authz.Engine) Option {
	return func(s *Server) { s.authz = engine }
}

func WithMaxBody(n int64) Option {
	return func(s *Server) { s.maxBody = n }
}

func WithTimeout(d time.Duration) Option {
	return func(s *Server) { s.timeout = d }
}

func WithRateLimit(limit int, window time.Duration) Option {
	return func(s *Server) {
		if limit > 0 && window > 0 {
			s.limiter = newRateLimiter(limit, window)
		}
	}
}

func WithLogger(logf func(format string, args ...any)) Option {
	return func(s *Server) { s.logf = logf }
}

func New(service *condition.Service, opts ...Option) *Server {
	cfg := service.Config()
	s := &Server{service: service, maxBody: cfg.MaxRequestBytes, timeout: cfg.RequestTimeout, metrics: NewMetrics()}
	if s.maxBody == 0 {
		s.maxBody = 1 << 20
	}
	if s.timeout == 0 {
		s.timeout = 5 * time.Second
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.authz == nil {
		s.authz = DefaultAuthzEngine()
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, route := range s.routes() {
		mux.HandleFunc(route.method+" "+route.pattern, route.handler)
	}

	auth := authzmw.HTTPFunc(s.authz,
		authzmw.WithSkipPaths("/healthz"),
		authzmw.WithSubject(subjectFromRequest),
		authzmw.WithResource(s.routeResourceFromRequest),
		authzmw.WithDeniedHandler(func(w http.ResponseWriter, r *http.Request, decision *authz.Decision) {
			writeError(w, http.StatusForbidden, "forbidden", decision.Reason)
		}),
	)
	return s.recover(s.observe(s.rateLimit(s.withTimeout(auth(s.withSubject(mux))))))
}

func DefaultAuthzEngine() *authz.Engine {
	policies := authzstores.NewMemoryPolicyStore()
	roles := authzstores.NewMemoryRoleStore()
	acls := authzstores.NewMemoryACLStore()
	audits := authzstores.NewMemoryAuditStore()
	members := authzstores.NewMemoryRoleMembershipStore()
	engine := authz.NewEngine(policies, roles, acls, audits, authz.WithRoleMembershipStore(members))
	ctx := context.Background()
	for _, role := range defaultRoles() {
		_ = engine.CreateRole(ctx, role)
	}
	return engine
}

func defaultRoles() []*authz.Role {
	perms := func(methods []string, resources ...string) []authz.Permission {
		var out []authz.Permission
		for _, method := range methods {
			for _, resource := range resources {
				out = append(out, authz.Permission{Action: authz.Action(method), Resource: resource})
			}
		}
		return out
	}
	return []*authz.Role{
		{ID: "condition-admin", Name: "Condition Admin", Permissions: perms([]string{"GET", "POST", "PUT", "DELETE"}, "route:*")},
		{ID: "condition-publisher", Name: "Condition Publisher", Permissions: perms([]string{"GET", "POST"}, "route:POST:/v1/definitions", "route:POST:/v1/definitions/validate", "route:GET:/v1/definitions/:name/versions", "route:POST:/v1/definitions/:name/versions/:version/approve", "route:POST:/v1/definitions/:name/versions/:version/activate", "route:POST:/v1/definitions/:name/disable", "route:POST:/v1/definitions/:name/enable", "route:POST:/v1/definitions/:name/rollback", "route:POST:/v1/reload")},
		{ID: "condition-operator", Name: "Condition Operator", Permissions: perms([]string{"GET", "POST"}, "route:GET:/v1/definitions", "route:GET:/v1/definitions/:name", "route:POST:/v1/definitions/:name/evaluate", "route:POST:/v1/definitions/:name/tests", "route:POST:/v1/definitions/:name/gates", "route:POST:/v1/definitions/:name/workflows/:workflow/start", "route:POST:/v1/workflows/:id/advance", "route:GET:/v1/workflows", "route:GET:/v1/workflows/:id")},
		{ID: "condition-simulator", Name: "Condition Simulator", Permissions: perms([]string{"POST"}, "route:POST:/v1/definitions/:name/simulate", "route:POST:/v1/definitions/:name/compare")},
		{ID: "condition-auditor", Name: "Condition Auditor", Permissions: perms([]string{"GET", "POST"}, "route:GET:/v1/readiness", "route:GET:/v1/audits", "route:GET:/v1/audits/:id", "route:POST:/v1/audits/verify", "route:GET:/v1/reports", "route:GET:/v1/metrics")},
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.service.Ready(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) productionReadiness(w http.ResponseWriter, r *http.Request) {
	report := s.service.ProductionReadiness(r.Context())
	status := http.StatusOK
	if !report.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func (s *Server) validate(w http.ResponseWriter, r *http.Request) {
	var req condition.ValidationRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	report, err := s.service.Validate(r.Context(), req)
	if err != nil && report == nil {
		writeError(w, http.StatusBadRequest, "validate_failed", err.Error())
		return
	}
	status := http.StatusOK
	if report != nil && !report.Publishable {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, report)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	var req condition.PublishRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Publish(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "publish_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	records, err := s.service.ListVersions(r.Context(), r.PathValue("name"), r.URL.Query().Get("environment"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "versions_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var req condition.ApprovalRequest
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Approve(r.Context(), r.PathValue("name"), r.PathValue("version"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "approve_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) activate(w http.ResponseWriter, r *http.Request) {
	resp, err := s.service.Activate(r.Context(), r.PathValue("name"), r.PathValue("version"), r.URL.Query().Get("environment"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "activate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) disable(w http.ResponseWriter, r *http.Request) {
	var req condition.DisableRequest
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Disable(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "disable_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) enable(w http.ResponseWriter, r *http.Request) {
	var req condition.DisableRequest
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Enable(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "enable_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version     string `json:"version"`
		Environment string `json:"environment,omitempty"`
	}
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Rollback(r.Context(), r.PathValue("name"), req.Version, req.Environment)
	if err != nil {
		writeError(w, http.StatusBadRequest, "rollback_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listDefinitions(w http.ResponseWriter, r *http.Request) {
	records, err := s.service.ListDefinitions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) getDefinition(w http.ResponseWriter, r *http.Request) {
	record, err := s.service.GetDefinition(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request) {
	var req condition.EvaluateRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Evaluate(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "evaluate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) test(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bundle string `json:"bundle,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Test(r.Context(), r.PathValue("name"), req.Bundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, "tests_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) gates(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bundle string `json:"bundle,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Gates(r.Context(), r.PathValue("name"), req.Bundle)
	if err != nil {
		writeError(w, http.StatusBadRequest, "gates_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) simulate(w http.ResponseWriter, r *http.Request) {
	var req condition.SimulationRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Simulate(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "simulate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	var req condition.SimulationRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Compare(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "compare_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) startWorkflow(w http.ResponseWriter, r *http.Request) {
	var req condition.WorkflowRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.StartWorkflow(r.Context(), r.PathValue("name"), r.PathValue("workflow"), req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "workflow_start_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) advanceWorkflow(w http.ResponseWriter, r *http.Request) {
	var req condition.WorkflowRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.AdvanceWorkflow(r.Context(), r.PathValue("id"), req.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, "workflow_advance_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getWorkflow(w http.ResponseWriter, r *http.Request) {
	resp, err := s.service.GetWorkflowRun(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listWorkflows(w http.ResponseWriter, r *http.Request) {
	resp, err := s.service.ListWorkflowRuns(r.Context(), listOptionsFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "workflow_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) listAudits(w http.ResponseWriter, r *http.Request) {
	records, err := s.service.QueryAudits(r.Context(), listOptionsFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	record, err := s.service.GetAudit(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (s *Server) verifyAudits(w http.ResponseWriter, r *http.Request) {
	if err := s.service.VerifyAudits(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "audit_chain_invalid", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true})
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	reports, err := s.service.QueryReports(r.Context(), listOptionsFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reports_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reports)
}

func (s *Server) metricsEndpoint(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.metrics.Snapshot())
}

func (s *Server) reload(w http.ResponseWriter, r *http.Request) {
	var req condition.ReloadRequest
	if !decodeJSON(w, r, s.maxBody, &req) {
		return
	}
	resp, err := s.service.Reload(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "reload_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.limiter == nil || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		key := firstHeader(r, "X-Subject-ID", "X-Service-ID")
		if key == "" {
			key = r.RemoteAddr
		}
		if !s.limiter.allow(key) {
			s.metrics.IncRateLimited()
			w.Header().Set("Retry-After", strconv.Itoa(int(s.limiter.window.Seconds())))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		s.metrics.Begin()
		defer func() {
			s.metrics.End(r.Method, r.URL.Path, rr.status, time.Since(start))
			if s.logf != nil {
				s.logf("%s %s status=%d duration=%s", r.Method, r.URL.Path, rr.status, time.Since(start))
			}
		}()
		next.ServeHTTP(rr, r)
	})
}

func (s *Server) withSubject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject := firstHeader(r, "X-Subject-ID", "X-Service-ID")
		if subject == "" {
			subject = "anonymous"
		}
		next.ServeHTTP(w, r.WithContext(condition.ContextWithSubject(r.Context(), subject)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type Metrics struct {
	started      time.Time
	requests     atomic.Int64
	errors       atomic.Int64
	rateLimited  atomic.Int64
	inFlight     atomic.Int64
	totalLatency atomic.Int64
	mu           sync.RWMutex
	routeCounts  map[string]int64
	statusCounts map[int]int64
}

type MetricsSnapshot struct {
	StartedAt        time.Time        `json:"started_at"`
	UptimeSeconds    int64            `json:"uptime_seconds"`
	RequestsTotal    int64            `json:"requests_total"`
	ErrorsTotal      int64            `json:"errors_total"`
	RateLimited      int64            `json:"rate_limited_total"`
	InFlight         int64            `json:"in_flight"`
	AverageLatencyMS float64          `json:"average_latency_ms"`
	Routes           map[string]int64 `json:"routes,omitempty"`
	Statuses         map[string]int64 `json:"statuses,omitempty"`
}

func NewMetrics() *Metrics {
	return &Metrics{started: time.Now(), routeCounts: map[string]int64{}, statusCounts: map[int]int64{}}
}

func (m *Metrics) Begin() {
	if m != nil {
		m.inFlight.Add(1)
	}
}

func (m *Metrics) End(method, path string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	m.inFlight.Add(-1)
	m.requests.Add(1)
	m.totalLatency.Add(duration.Milliseconds())
	if status >= 400 {
		m.errors.Add(1)
	}
	m.mu.Lock()
	m.routeCounts[method+" "+path]++
	m.statusCounts[status]++
	m.mu.Unlock()
}

func (m *Metrics) IncRateLimited() {
	if m != nil {
		m.rateLimited.Add(1)
	}
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	if m == nil {
		return MetricsSnapshot{}
	}
	requests := m.requests.Load()
	avg := 0.0
	if requests > 0 {
		avg = float64(m.totalLatency.Load()) / float64(requests)
	}
	snapshot := MetricsSnapshot{
		StartedAt:        m.started,
		UptimeSeconds:    int64(time.Since(m.started).Seconds()),
		RequestsTotal:    requests,
		ErrorsTotal:      m.errors.Load(),
		RateLimited:      m.rateLimited.Load(),
		InFlight:         m.inFlight.Load(),
		AverageLatencyMS: avg,
		Routes:           map[string]int64{},
		Statuses:         map[string]int64{},
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.routeCounts {
		snapshot.Routes[k] = v
	}
	for k, v := range m.statusCounts {
		snapshot.Statuses[strconv.Itoa(k)] = v
	}
	return snapshot
}

type rateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string]rateBucket
}

type rateBucket struct {
	start time.Time
	count int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string]rateBucket{}}
}

func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket := l.hits[key]
	if bucket.start.IsZero() || now.Sub(bucket.start) >= l.window {
		l.hits[key] = rateBucket{start: now, count: 1}
		return true
	}
	if bucket.count >= l.limit {
		return false
	}
	bucket.count++
	l.hits[key] = bucket
	return true
}

func (s *Server) withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				writeError(w, http.StatusInternalServerError, "panic", fmt.Sprint(v))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func subjectFromRequest(r *http.Request) *authz.Subject {
	roles := splitHeader(r.Header.Get("X-Roles"))
	if len(roles) == 0 {
		roles = splitHeader(r.Header.Get("X-Condition-Roles"))
	}
	return &authz.Subject{
		ID:       firstHeader(r, "X-Subject-ID", "X-Service-ID"),
		Type:     "service",
		TenantID: firstHeader(r, "X-Tenant-ID"),
		Roles:    roles,
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBody int64, out any) bool {
	if r.Method != http.MethodGet && r.Body != nil {
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "content type must be application/json")
			return false
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": code, "message": message})
}

func splitHeader(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func listOptionsFromRequest(r *http.Request) storage.ListOptions {
	q := r.URL.Query()
	opts := storage.ListOptions{
		Operation:   q.Get("operation"),
		Kind:        q.Get("kind"),
		Definition:  q.Get("definition"),
		Subject:     q.Get("subject"),
		Environment: q.Get("environment"),
	}
	if limit, err := strconv.Atoi(q.Get("limit")); err == nil {
		opts.Limit = limit
	}
	if offset, err := strconv.Atoi(q.Get("offset")); err == nil {
		opts.Offset = offset
	}
	if since, err := time.Parse(time.RFC3339, q.Get("since")); err == nil {
		opts.Since = &since
	}
	if until, err := time.Parse(time.RFC3339, q.Get("until")); err == nil {
		opts.Until = &until
	}
	return opts
}
