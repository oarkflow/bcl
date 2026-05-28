package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oarkflow/bcl"
	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

type Principal struct {
	ID            string
	Role          string
	TenantID      string
	Department    string
	Active        bool
	DeviceTrusted bool
	ACLDocuments  []string
}

type EndpointPolicy struct {
	Name       string
	Pattern    string
	MatchKind  string
	PolicyType string
	APIScope   string
	TenantID   string
	Department string
	ResourceID string
	Params     map[string]string
}

type RequestSignals struct {
	RequestsPerMinute int
	BurstCount        int
	Banned            bool
}

type AuthGuard struct {
	service         *condition.Service
	maintenanceMode bool
	now             time.Time
	meter           *TrafficMeter
}

type RequestCase struct {
	Name          string
	Method        string
	Path          string
	Token         string
	Repeat        int
	MaintenanceOn bool
}

type TrafficMeter struct {
	mu      sync.Mutex
	windows map[string]*TrafficWindow
}

type TrafficWindow struct {
	StartedAt time.Time
	Count     int
	Burst     int
	BannedAt  time.Time
}

func main() {
	serve := flag.Bool("serve", false, "run an HTTP server instead of the demo requests")
	addr := flag.String("addr", ":8080", "HTTP server address")
	maintenance := flag.Bool("maintenance", false, "enable global write-blocking maintenance mode")
	flag.Parse()

	ctx := context.Background()
	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{Environment: "example", RequireTests: true})
	if _, err := svc.Publish(ctx, condition.PublishRequest{Name: "http-auth-guard", Version: "1", Path: filepath.Join(examplebase.Dir(), "decision.bcl"), RunTests: true}); err != nil {
		log.Fatal(err)
	}

	if *serve {
		runServer(svc, *addr, *maintenance)
		return
	}
	runDemo(svc)
}

func runServer(svc *condition.Service, addr string, maintenance bool) {
	guard := AuthGuard{service: svc, maintenanceMode: maintenance, now: time.Now(), meter: NewTrafficMeter()}
	fmt.Printf("HTTP auth guard listening on %s\n", serverBaseURL(addr))
	fmt.Printf("maintenance=%v\n\n", maintenance)
	fmt.Println("Try:")
	for _, c := range curlExamples(addr) {
		fmt.Println("  " + c)
	}
	fmt.Println()
	log.Fatal(http.ListenAndServe(addr, guard.Middleware(routes(svc))))
}

func runDemo(svc *condition.Service) {
	for _, c := range requestCases() {
		guard := AuthGuard{service: svc, maintenanceMode: c.MaintenanceOn, now: time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC), meter: NewTrafficMeter()}
		server := httptest.NewServer(guard.Middleware(routes(svc)))
		var resp *http.Response
		var body []byte
		repeat := c.Repeat
		if repeat == 0 {
			repeat = 1
		}
		for i := 0; i < repeat; i++ {
			req, err := http.NewRequest(c.Method, server.URL+c.Path, nil)
			if err != nil {
				log.Fatal(err)
			}
			if c.Token != "" {
				req.Header.Set("Authorization", "Bearer "+c.Token)
			}
			resp, err = http.DefaultClient.Do(req)
			if err != nil {
				log.Fatal(err)
			}
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				log.Fatal(err)
			}
		}
		server.Close()

		fmt.Printf("\n%s %s %s\n", c.Name, c.Method, c.Path)
		fmt.Printf("  token=%s maintenance=%v repeat=%d\n", tokenLabel(c.Token), c.MaintenanceOn, repeat)
		fmt.Printf("  guard: action=%s reason=%s retry_after=%s\n", resp.Header.Get("X-Guard-Action"), resp.Header.Get("X-Guard-Reason"), resp.Header.Get("Retry-After"))
		fmt.Printf("  response: status=%d policy=%s reason=%s body=%s\n", resp.StatusCode, resp.Header.Get("X-Auth-Policy"), resp.Header.Get("X-Auth-Reason"), strings.TrimSpace(string(body)))
	}
}

func curlExamples(addr string) []string {
	base := serverBaseURL(addr)
	return []string{
		`curl -i ` + base + `/_rules`,
		`curl -s ` + base + `/_coverage`,
		`curl -s ` + base + `/_actions`,
		`curl -s ` + base + `/_incidents`,
		`curl -s ` + base + `/_readiness`,
		`curl -i ` + base + `/public/status`,
		`curl -i -X POST -H "Authorization: Bearer admin-token" ` + base + `/documents/alpha`,
		`curl -i ` + base + `/documents/alpha`,
		`curl -i -H "Authorization: Bearer admin-token" ` + base + `/admin/reports`,
		`curl -i -H "Authorization: Bearer admin-token" ` + base + `/admin/settings/security`,
		`curl -i -H "Authorization: Bearer analyst-token" ` + base + `/admin/reports`,
		`for i in {1..3}; do curl -i -H "Authorization: Bearer analyst-token" ` + base + `/admin/reports; done`,
		`curl -i -H "Authorization: Bearer analyst-token" ` + base + `/projects/acme/export`,
		`curl -i -H "Authorization: Bearer analyst-token" ` + base + `/projects/other/export`,
		`for i in {1..5}; do curl -i -H "Authorization: Bearer analyst-token" ` + base + `/projects/acme/export; done`,
		`curl -i -H "Authorization: Bearer viewer-token" ` + base + `/documents/alpha`,
		`curl -i -H "Authorization: Bearer viewer-token" ` + base + `/documents/beta`,
		`for i in {1..5}; do curl -i -H "Authorization: Bearer viewer-token" ` + base + `/documents/alpha; done`,
		`curl -i -H "Authorization: Bearer analyst-token" ` + base + `/api/v1/tenants/acme/users/u-analyst`,
		`curl -i -X PATCH -H "Authorization: Bearer analyst-token" ` + base + `/api/v1/tenants/acme/users/u-analyst`,
		`curl -i -X PATCH -H "Authorization: Bearer admin-token" ` + base + `/api/v1/tenants/acme/users/u-analyst`,
		`for i in {1..7}; do curl -i -H "Authorization: Bearer analyst-token" ` + base + `/api/v1/tenants/acme/users/u-analyst; done`,
		`for i in {1..8}; do curl -i -H "Authorization: Bearer admin-token" ` + base + `/admin/reports; done`,
		`for i in {1..5}; do curl -i ` + base + `/fail/endpoint; done`,
		`for i in {1..5}; do curl -i ` + base + `/fail/app/database; done`,
	}
}

func serverBaseURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

func routes(svc *condition.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_rules", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "definition: decision.bcl")
		fmt.Fprintln(w, "chain: http_request_chain")
		fmt.Fprintln(w, "files:")
		for _, file := range []string{
			"rules/catalogs.bcl",
			"rules/routes.bcl",
			"rules/security/abuse.bcl",
			"rules/security/observability.bcl",
			"rules/lifecycle.bcl",
			"rules/global.bcl",
			"rules/endpoints/admin.bcl",
			"rules/endpoints/projects.bcl",
			"rules/endpoints/documents.bcl",
			"rules/endpoints/tenant_users.bcl",
			"tests/decision_tests.bcl",
		} {
			fmt.Fprintln(w, "  - "+file)
		}
		fmt.Fprintln(w, "decisions:")
		for _, decision := range []string{
			"global_endpoint_guard",
			"admin_endpoint_guard",
			"project_endpoint_guard",
			"document_endpoint_guard",
			"tenant_user_endpoint_guard",
			"global_auth_guard",
			"admin_auth_guard",
			"project_auth_guard",
			"document_auth_guard",
			"tenant_user_auth_guard",
			"response_observability",
		} {
			fmt.Fprintln(w, "  - "+decision)
		}
		fmt.Fprintln(w, "curl:")
		for _, c := range curlExamples(r.Host) {
			fmt.Fprintln(w, c)
		}
	})
	mux.HandleFunc("/_coverage", func(w http.ResponseWriter, r *http.Request) {
		report, err := svc.RouteCoverage(r.Context(), "http-auth-guard")
		writeExampleJSON(w, report, err)
	})
	mux.HandleFunc("/_actions", func(w http.ResponseWriter, r *http.Request) {
		records, err := svc.ListActionDeliveries(r.Context(), storage.ActionDeliveryQuery{Limit: 100})
		writeExampleJSON(w, records, err)
	})
	mux.HandleFunc("/_incidents", func(w http.ResponseWriter, r *http.Request) {
		records, err := svc.ListIncidents(r.Context(), storage.IncidentQuery{Limit: 100})
		writeExampleJSON(w, records, err)
	})
	mux.HandleFunc("/_readiness", func(w http.ResponseWriter, r *http.Request) {
		writeExampleJSON(w, svc.ProductionReadiness(r.Context()), nil)
	})
	mux.HandleFunc("/public/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "public status ok")
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "admin endpoint %s", strings.TrimPrefix(r.URL.Path, "/admin/"))
	})
	mux.HandleFunc("/projects/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "project endpoint %s", strings.TrimPrefix(r.URL.Path, "/projects/"))
	})
	mux.HandleFunc("/documents/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "document endpoint %s", strings.TrimPrefix(r.URL.Path, "/documents/"))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "api endpoint %s", strings.TrimPrefix(r.URL.Path, "/api/"))
	})
	mux.HandleFunc("/fail/endpoint", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "endpoint failure", http.StatusInternalServerError)
	})
	mux.HandleFunc("/fail/app/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "application failure", http.StatusBadGateway)
	})
	return mux
}

func writeExampleJSON(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(value)
}

func (g AuthGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := principalFromRequest(r)
		endpoint := endpointPolicy(r.URL.Path)
		facts := g.facts(r, principal, endpoint)

		preResp, err := g.service.EvaluateLifecycle(r.Context(), "http-auth-guard", "http_request", condition.LifecycleEvaluateRequest{
			Phase:  "pre",
			Method: r.Method,
			Path:   r.URL.Path,
			Request: map[string]any{
				"headers": headersFromHTTP(r.Header),
				"body":    map[string]any{},
				"format":  requestFormat(r.Header),
			},
			Input: facts,
		})
		if err != nil {
			http.Error(w, "guard evaluation failed", http.StatusInternalServerError)
			return
		}
		chainResp := firstLifecycleChain(preResp)
		if chainResp == nil {
			http.Error(w, "lifecycle evaluation did not return expected chain", http.StatusInternalServerError)
			return
		}
		guardDecision := firstChainDecision(chainResp, []string{"global_endpoint_guard", "admin_endpoint_guard", "project_endpoint_guard", "document_endpoint_guard", "tenant_user_endpoint_guard"}, true)
		authDecision := firstChainDecision(chainResp, []string{"global_auth_guard", "admin_auth_guard", "project_auth_guard", "document_auth_guard", "tenant_user_auth_guard"}, true)
		if guardDecision == nil || authDecision == nil {
			http.Error(w, "chain evaluation did not return expected decisions", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Guard-Action", fmt.Sprint(guardDecision.Attributes["action"]))
		w.Header().Set("X-Guard-Reason", guardDecision.ReasonCode)
		w.Header().Set("X-Chain-Action", chainResp.Evaluation.FinalAction)
		w.Header().Set("X-Chain-Category", chainCategory(chainResp.Evaluation.StateAfter))
		if warning := guardDecision.Attributes["warning"]; warning != nil {
			w.Header().Set("X-Guard-Warning", fmt.Sprint(warning))
		}
		if chainAction, chainStatus := chainEnforcement(chainResp.Evaluation.StateAfter); chainAction != "" {
			w.Header().Set("X-Guard-Action", chainAction)
			w.Header().Set("X-Guard-Reason", chainResp.Evaluation.FinalReason)
			if retryAfter := chainRetryAfter(chainResp.Evaluation.StateAfter); retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			http.Error(w, chainResp.Evaluation.FinalReason, chainStatus)
			return
		}
		if guardDecision.Effect != "allow" {
			if retryAfter := guardDecision.Attributes["retry_after_seconds"]; retryAfter != nil {
				w.Header().Set("Retry-After", fmt.Sprint(retryAfter))
			}
			status := statusFromDecision(guardDecision.Attributes["status"])
			http.Error(w, guardDecision.Reason, status)
			return
		}

		w.Header().Set("X-Auth-Policy", fmt.Sprint(authDecision.Attributes["policy"]))
		w.Header().Set("X-Auth-Reason", authDecision.ReasonCode)
		if authDecision.Effect != "allow" {
			status := statusFromDecision(authDecision.Attributes["status"])
			http.Error(w, authDecision.Reason, status)
			return
		}
		rec := &exampleStatusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		postResp, err := g.service.EvaluateLifecycle(r.Context(), "http-auth-guard", "http_request", condition.LifecycleEvaluateRequest{
			Phase:  "post",
			Method: r.Method,
			Path:   r.URL.Path,
			Request: map[string]any{
				"headers": headersFromHTTP(r.Header),
				"body":    map[string]any{},
				"format":  requestFormat(r.Header),
			},
			Input: facts,
			Response: map[string]any{
				"status":      rec.status,
				"duration_ms": 0,
				"headers":     headersFromHTTP(rec.Header()),
				"body":        map[string]any{"bytes": 0},
				"format":      responseFormat(rec.Header()),
			},
		})
		if err == nil && len(postResp.Evaluation.Actions) > 0 {
			w.Header().Set("X-Post-Action", postResp.Evaluation.FinalAction)
			w.Header().Set("X-Post-Reason", postResp.Evaluation.FinalReason)
		}
	})
}

func headersFromHTTP(headers http.Header) map[string]any {
	out := map[string]any{}
	for key, values := range headers {
		name := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		if len(values) == 1 {
			out[name] = values[0]
		} else {
			out[name] = values
		}
	}
	return out
}

func requestFormat(headers http.Header) string {
	if strings.Contains(headers.Get("Content-Type"), "json") {
		return "json"
	}
	return "http"
}

func responseFormat(headers http.Header) string {
	if strings.Contains(headers.Get("Content-Type"), "json") {
		return "json"
	}
	return "text"
}

type exampleStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *exampleStatusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func firstLifecycleChain(resp *condition.LifecycleEvaluateResponse) *condition.ChainEvaluateResponse {
	if resp == nil || len(resp.Evaluation.Chains) == 0 {
		return nil
	}
	return &condition.ChainEvaluateResponse{Evaluation: resp.Evaluation.Chains[0]}
}

func firstChainDecision(resp *condition.ChainEvaluateResponse, decisions []string, preferNonAllow bool) *bcl.DecisionResult {
	if resp == nil {
		return nil
	}
	var fallback *bcl.DecisionResult
	for _, item := range resp.Evaluation.Decisions {
		if !contains(decisions, item.Decision) || item.Report == nil || item.Report.Decision == nil {
			continue
		}
		if fallback == nil {
			fallback = item.Report.Decision
		}
		if preferNonAllow && item.Report.Decision.Effect != "allow" {
			return item.Report.Decision
		}
		if item.Report.Decision.ReasonCode != "" {
			fallback = item.Report.Decision
		}
	}
	return fallback
}

func (g AuthGuard) facts(r *http.Request, p Principal, e EndpointPolicy) map[string]any {
	signals := g.requestSignals(r, e)
	return map[string]any{
		"global": map[string]any{
			"maintenance_mode": g.maintenanceMode,
		},
		"request": map[string]any{
			"method":              r.Method,
			"path":                r.URL.Path,
			"actor_key":           firstNonEmpty(p.ID, r.RemoteAddr, "anonymous"),
			"safe_method":         r.Method == http.MethodGet || r.Method == http.MethodHead,
			"write_method":        r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch || r.Method == http.MethodDelete,
			"business_hours":      g.now.Hour() >= 8 && g.now.Hour() < 18,
			"requests_per_minute": signals.RequestsPerMinute,
			"burst_count":         signals.BurstCount,
			"banned":              signals.Banned,
		},
		"principal": map[string]any{
			"id":                 p.ID,
			"authenticated":      p.ID != "",
			"active":             p.Active,
			"role":               p.Role,
			"admin_role":         p.Role == "admin",
			"same_tenant":        p.TenantID == e.TenantID && e.TenantID != "",
			"department_matches": p.Department == e.Department && e.Department != "",
			"device_trusted":     p.DeviceTrusted,
			"acl_granted":        contains(p.ACLDocuments, e.ResourceID),
		},
		"endpoint": map[string]any{
			"name":           e.Name,
			"pattern":        e.Pattern,
			"match_kind":     e.MatchKind,
			"policy_type":    e.PolicyType,
			"api_scope":      e.APIScope,
			"resource_id":    e.ResourceID,
			"tenant_id":      e.TenantID,
			"department":     e.Department,
			"tenant_param":   e.Params["tenant"],
			"document_param": e.Params["document_id"],
			"user_param":     e.Params["user_id"],
			"params":         e.Params,
		},
	}
}

func chainRetryAfter(states []storage.ChainStateRecord) string {
	for _, action := range []string{"suspend", "ban", "rate_limit"} {
		for _, state := range states {
			if state.Step == "" || state.Action != action {
				continue
			}
			if retryAfter := state.Attributes["retry_after_seconds"]; retryAfter != nil {
				return fmt.Sprint(retryAfter)
			}
		}
	}
	return ""
}

func chainEnforcement(states []storage.ChainStateRecord) (string, int) {
	for _, action := range []string{"suspend", "ban", "rate_limit"} {
		for _, state := range states {
			if state.Step == "" || state.Action != action {
				continue
			}
			status := statusFromDecision(state.Attributes["status"])
			if action == "rate_limit" && status == http.StatusForbidden {
				status = http.StatusTooManyRequests
			}
			return action, status
		}
	}
	return "", 0
}

func chainCategory(states []storage.ChainStateRecord) string {
	for _, state := range states {
		if category := fmt.Sprint(state.Metadata["category"]); category != "" && category != "<nil>" {
			return category
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func requestCases() []RequestCase {
	return []RequestCase{
		{Name: "Public status allowed", Method: http.MethodGet, Path: "/public/status"},
		{Name: "RBAC admin allowed", Method: http.MethodGet, Path: "/admin/reports", Token: "admin-token"},
		{Name: "RBAC admin wildcard allowed", Method: http.MethodGet, Path: "/admin/settings/security", Token: "admin-token"},
		{Name: "RBAC analyst denied", Method: http.MethodGet, Path: "/admin/reports", Token: "analyst-token"},
		{Name: "ABAC project export allowed", Method: http.MethodGet, Path: "/projects/acme/export", Token: "analyst-token"},
		{Name: "ABAC dynamic tenant denied", Method: http.MethodGet, Path: "/projects/other/export", Token: "analyst-token"},
		{Name: "ABAC untrusted device denied", Method: http.MethodGet, Path: "/projects/acme/export", Token: "contractor-token"},
		{Name: "ACL document allowed", Method: http.MethodGet, Path: "/documents/alpha", Token: "viewer-token"},
		{Name: "ACL dynamic document denied", Method: http.MethodGet, Path: "/documents/beta", Token: "viewer-token"},
		{Name: "API tenant read allowed", Method: http.MethodGet, Path: "/api/v1/tenants/acme/users/u-analyst", Token: "analyst-token"},
		{Name: "API tenant write denied for analyst", Method: http.MethodPatch, Path: "/api/v1/tenants/acme/users/u-analyst", Token: "analyst-token"},
		{Name: "API tenant write allowed for admin", Method: http.MethodPatch, Path: "/api/v1/tenants/acme/users/u-analyst", Token: "admin-token"},
		{Name: "API guard rate limits tenant users endpoint", Method: http.MethodGet, Path: "/api/v1/tenants/acme/users/u-analyst", Token: "analyst-token", Repeat: 7},
		{Name: "API guard bans busy admin endpoint", Method: http.MethodGet, Path: "/admin/reports", Token: "admin-token", Repeat: 8},
		{Name: "API guard soft warns document endpoint", Method: http.MethodGet, Path: "/documents/alpha", Token: "viewer-token", Repeat: 5},
		{Name: "Global auth required", Method: http.MethodGet, Path: "/documents/alpha"},
		{Name: "Global maintenance blocks write", Method: http.MethodPost, Path: "/documents/alpha", Token: "maintenance-admin-token", MaintenanceOn: true},
	}
}

func principalFromRequest(r *http.Request) Principal {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch token {
	case "admin-token":
		return Principal{ID: "u-admin", Role: "admin", TenantID: "acme", Department: "finance", Active: true, DeviceTrusted: true}
	case "maintenance-admin-token":
		return Principal{ID: "u-maint-admin", Role: "admin", TenantID: "acme", Department: "finance", Active: true, DeviceTrusted: true}
	case "analyst-token":
		return Principal{ID: "u-analyst", Role: "analyst", TenantID: "acme", Department: "finance", Active: true, DeviceTrusted: true}
	case "contractor-token":
		return Principal{ID: "u-contractor", Role: "contractor", TenantID: "acme", Department: "finance", Active: true, DeviceTrusted: false}
	case "viewer-token":
		return Principal{ID: "u-viewer", Role: "viewer", TenantID: "partner", Department: "legal", Active: true, DeviceTrusted: true, ACLDocuments: []string{"doc-alpha"}}
	default:
		return Principal{}
	}
}

func endpointPolicy(path string) EndpointPolicy {
	for _, route := range routePolicies() {
		params, ok := matchPattern(route.Pattern, path)
		if !ok {
			continue
		}
		route.Params = params
		route.MatchKind = matchKind(route.Pattern)
		if tenantID := params["tenant"]; tenantID != "" {
			route.TenantID = tenantID
			route.ResourceID = "project-" + tenantID
		}
		if documentID := params["document_id"]; documentID != "" {
			route.ResourceID = "doc-" + documentID
		}
		if userID := params["user_id"]; userID != "" {
			route.ResourceID = "user-" + userID
		}
		return route
	}
	return EndpointPolicy{Name: "public", Pattern: "*", MatchKind: "wildcard", PolicyType: "public", Params: map[string]string{}}
}

func routePolicies() []EndpointPolicy {
	return []EndpointPolicy{
		{Name: "admin area", Pattern: "/admin/*", PolicyType: "rbac"},
		{Name: "project export", Pattern: "/projects/{tenant}/export", PolicyType: "abac", Department: "finance"},
		{Name: "document", Pattern: "/documents/{document_id}", PolicyType: "acl"},
		{Name: "tenant user API", Pattern: "/api/v1/tenants/{tenant}/users/{user_id}", PolicyType: "api_endpoint", APIScope: "tenant-user"},
	}
}

func matchPattern(pattern, path string) (map[string]string, bool) {
	params := map[string]string{}
	if pattern == "*" {
		return params, true
	}
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)
	for i, part := range patternParts {
		if part == "*" {
			return params, true
		}
		if i >= len(pathParts) {
			return nil, false
		}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			params[name] = pathParts[i]
			continue
		}
		if part != pathParts[i] {
			return nil, false
		}
	}
	return params, len(patternParts) == len(pathParts)
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func matchKind(pattern string) string {
	switch {
	case strings.Contains(pattern, "{"):
		return "dynamic"
	case strings.Contains(pattern, "*"):
		return "wildcard"
	default:
		return "exact"
	}
}

func statusFromDecision(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return http.StatusForbidden
	}
}

func NewTrafficMeter() *TrafficMeter {
	return &TrafficMeter{windows: make(map[string]*TrafficWindow)}
}

func (g AuthGuard) requestSignals(r *http.Request, e EndpointPolicy) RequestSignals {
	if g.meter == nil {
		return RequestSignals{RequestsPerMinute: 1, BurstCount: 1}
	}
	return g.meter.Record(clientAddress(r), e.Pattern, time.Now())
}

func (m *TrafficMeter) Record(client, pattern string, now time.Time) RequestSignals {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := client + "|" + pattern
	window := m.windows[key]
	if window == nil || now.Sub(window.StartedAt) >= time.Minute {
		window = &TrafficWindow{StartedAt: now}
		m.windows[key] = window
	}
	window.Count++
	window.Burst++
	if now.Sub(window.BannedAt) > time.Minute {
		window.BannedAt = time.Time{}
	}
	if pattern == "/admin/*" && window.Burst >= 8 {
		window.BannedAt = now
	}
	return RequestSignals{
		RequestsPerMinute: window.Count,
		BurstCount:        window.Burst,
		Banned:            !window.BannedAt.IsZero(),
	}
}

func clientAddress(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		return strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > -1 {
		return host[:idx]
	}
	return host
}

func tokenLabel(token string) string {
	if token == "" {
		return "none"
	}
	return token
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
