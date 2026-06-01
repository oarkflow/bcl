package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/oarkflow/bcl/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/bcl/condition/pkg/condition"
	"github.com/oarkflow/bcl/condition/pkg/storage"
)

const definitionName = "complete-platform"

var demoCredentials = map[string]string{
	"alice": "correct",
	"bob":   "correct",
}

type completePlatformRuntime struct {
	service *condition.Service
	store   *storage.MemoryStore
}

func main() {
	serve := flag.Bool("serve", false, "run a Fiber HTTP server instead of demo requests")
	addr := flag.String("addr", ":8082", "Fiber server address")
	flag.Parse()

	if *serve {
		runtime := mustRuntime()
		app := newApp(runtime)
		fmt.Printf("complete platform Fiber example listening on http://127.0.0.1%s\n", *addr)
		for _, c := range curlExamples(*addr) {
			fmt.Println("  " + c)
		}
		log.Fatal(app.Listen(*addr, fiber.ListenConfig{DisableStartupMessage: true}))
	}
	now := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	runtime, err := newRuntimeWithClock(func() time.Time { return now })
	if err != nil {
		log.Fatal(err)
	}
	app := newApp(runtime)
	runDemo(app, func(d time.Duration) {
		now = now.Add(d)
		_ = runtime.store.DeleteExpiredChainStates(context.Background(), time.Now().UTC().Add(365*24*time.Hour))
	})
}

func mustRuntime() *completePlatformRuntime {
	runtime, err := newRuntime()
	if err != nil {
		log.Fatal(err)
	}
	return runtime
}

func newRuntime() (*completePlatformRuntime, error) {
	return newRuntimeWithClock(nil)
}

func newRuntimeWithClock(clock func() time.Time) (*completePlatformRuntime, error) {
	store := storage.NewMemoryStore()
	svc := condition.NewService(store, condition.Config{
		Environment:  "example",
		RequireTests: true,
		Clock:        clock,
		Runtime: condition.RuntimePolicy{ActionAllowlists: []condition.ActionAllowlist{
			{
				TenantID:    "default",
				Environment: "example",
				Actions: []string{
					"allow", "failed_login", "successful_login", "admin_denied", "rate_limit", "warning",
					"block", "suspend", "ban", "notify", "escalate", "healthy", "unexpected_4xx", "endpoint_5xx", "app_5xx",
				},
				Sinks: []string{"event", "log"},
			},
		}},
	})
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{
		Name:     definitionName,
		Version:  "1",
		Path:     filepath.Join(examplebase.Dir(), "decision.bcl"),
		RunTests: true,
	}); err != nil {
		return nil, err
	}
	if clock != nil && svc.Config().Clock == nil {
		return nil, fmt.Errorf("condition service clock was not configured")
	}
	return &completePlatformRuntime{service: svc, store: store}, nil
}

func newApp(runtime *completePlatformRuntime) *fiber.App {
	app := fiber.New()
	app.Use(conditionLifecycle(runtime))

	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name": definitionName,
			"test_credentials": fiber.Map{
				"alice": "correct",
				"bob":   "correct",
			},
			"use_cases": []string{
				"JSON and form login observability",
				"failed-login escalation",
				"admin-route denial correlation",
				"document route matching",
				"unexpected 4xx detection",
				"endpoint and app-wide 5xx escalation",
				"action, incident, readiness, and route coverage inspection",
			},
			"routes": []string{"/login", "/admin/reports", "/documents/:document_id", "/fail/endpoint", "/fail/app/:component"},
		})
	})

	app.Post("/login", loginHandler)
	app.Get("/admin/reports", adminReportsHandler)
	app.Get("/documents/:document_id", documentHandler)
	app.Get("/fail/endpoint", func(c fiber.Ctx) error {
		c.Status(http.StatusInternalServerError)
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
		return c.SendString("endpoint failed")
	})
	app.Get("/fail/app/:component", func(c fiber.Ctx) error {
		c.Status(http.StatusBadGateway)
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
		return c.SendString("application component failed: " + c.Params("component"))
	})

	app.Get("/_rules", func(c fiber.Ctx) error {
		lines := []string{
			"definition: decision.bcl",
			"",
			"files:",
		}
		for _, file := range policyFiles() {
			lines = append(lines, "  - "+file)
		}
		lines = append(lines, "", "curl examples:")
		lines = append(lines, curlExamples(":8082")...)
		return c.Type("text").SendString(strings.Join(lines, "\n") + "\n")
	})
	app.Get("/_coverage", func(c fiber.Ctx) error {
		report, err := runtime.service.RouteCoverage(c.Context(), definitionName)
		return writeJSON(c, report, err)
	})
	app.Get("/_state", func(c fiber.Ctx) error {
		return writeJSON(c, stateSummary(c.Context(), runtime.store, c.Query("actor", "alice")), nil)
	})
	app.Get("/_events", func(c fiber.Ctx) error {
		events, err := runtime.store.QueryChainEvents(c.Context(), storage.ChainEventQuery{Definition: definitionName, EntityKey: c.Query("actor"), IncludeExpired: true, Limit: 100})
		return writeJSON(c, eventSummary(events), err)
	})
	app.Get("/_actions", func(c fiber.Ctx) error {
		records, err := runtime.service.ListActionDeliveries(c.Context(), storage.ActionDeliveryQuery{Definition: definitionName, Limit: 50})
		return writeJSON(c, actionSummary(records), err)
	})
	app.Get("/_incidents", func(c fiber.Ctx) error {
		records, err := runtime.service.ListIncidents(c.Context(), storage.IncidentQuery{Definition: definitionName, Limit: 50})
		return writeJSON(c, records, err)
	})
	app.Get("/_readiness", func(c fiber.Ctx) error {
		return writeJSON(c, runtime.service.ProductionReadiness(c.Context()), nil)
	})

	return app
}

func conditionLifecycle(runtime *completePlatformRuntime) fiber.Handler {
	return func(c fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/_") {
			return c.Next()
		}
		requestFacts, actor := requestFacts(c)
		preResp, preErr := evaluatePre(c, runtime.service, requestFacts, actor)
		if preErr != nil {
			c.Status(http.StatusInternalServerError)
			return c.JSON(fiber.Map{"error": preErr.Error()})
		}
		if preResp != nil {
			setConditionHeaders(c, preResp.Evaluation)
		}
		if env := preResp.Evaluation.Enforcement; env != nil && env.Blocking {
			return writeConditionResponse(c, env)
		}

		err := c.Next()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)
			_ = c.JSON(fiber.Map{"error": err.Error()})
		}

		status := c.Response().StatusCode()
		responseBody := append([]byte(nil), c.Response().Body()...)
		resp, evalErr := evaluatePost(c, runtime.service, requestFacts, actor, status, responseBody)
		if evalErr == nil {
			setConditionHeaders(c, resp.Evaluation)
			if env := resp.Evaluation.Enforcement; env != nil && env.Blocking {
				return writeConditionResponse(c, env)
			}
		} else {
			c.Set("X-Condition-Error", evalErr.Error())
		}
		return nil
	}
}

func requestFacts(c fiber.Ctx) (map[string]any, string) {
	requestBody := append([]byte(nil), c.Body()...)
	body := bodyFact(c.Get(fiber.HeaderContentType), requestBody)
	return map[string]any{
		"headers": headersFromFiber(c.GetReqHeaders()),
		"body":    body,
		"format":  bodyFormat(c.Get(fiber.HeaderContentType)),
	}, actorKey(c, body)
}

func responseFacts(c fiber.Ctx, status int, responseBody []byte) map[string]any {
	return map[string]any{
		"status":  status,
		"headers": headersFromFiber(c.GetRespHeaders()),
		"body":    bodyFact(c.GetRespHeader(fiber.HeaderContentType), responseBody),
		"format":  bodyFormat(c.GetRespHeader(fiber.HeaderContentType)),
	}
}

func evaluatePre(c fiber.Ctx, svc *condition.Service, requestFacts map[string]any, actor string) (*condition.LifecycleEvaluateResponse, error) {
	resp, err := svc.EvaluateLifecycle(c.Context(), definitionName, "http_request", condition.LifecycleEvaluateRequest{
		Phase:   "pre",
		Method:  c.Method(),
		Path:    c.Path(),
		Request: requestFacts,
		Input:   lifecycleInput(actor),
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func evaluatePost(c fiber.Ctx, svc *condition.Service, requestFacts map[string]any, actor string, status int, responseBody []byte) (*condition.LifecycleEvaluateResponse, error) {
	return svc.EvaluateLifecycle(c.Context(), definitionName, "http_request", condition.LifecycleEvaluateRequest{
		Phase:    "post",
		Method:   c.Method(),
		Path:     c.Path(),
		Request:  requestFacts,
		Input:    lifecycleInput(actor),
		Response: responseFacts(c, status, responseBody),
	})
}

func lifecycleInput(actor string) map[string]any {
	return map[string]any{
		"request": map[string]any{
			"actor_key":       actor,
			"application_key": "complete-platform-demo",
		},
	}
}

func displayAction(e condition.LifecycleEvaluation) string {
	if e.Enforcement != nil && e.Enforcement.Action != "" {
		return e.Enforcement.Action
	}
	if e.FinalAction != "" {
		return e.FinalAction
	}
	for i := len(e.Chains) - 1; i >= 0; i-- {
		chain := e.Chains[i]
		if chain.FinalAction != "" {
			return chain.FinalAction
		}
		for j := len(chain.Events) - 1; j >= 0; j-- {
			if chain.Events[j].EventType != "" {
				return chain.Events[j].EventType
			}
		}
	}
	for i := len(e.Actions) - 1; i >= 0; i-- {
		if e.Actions[i].Name != "" {
			return e.Actions[i].Name
		}
	}
	return ""
}

func writeConditionResponse(c fiber.Ctx, env *condition.EnforcementEnvelope) error {
	setEnforcementHeaders(c, env)
	status := env.Status
	if status == 0 && env.Blocking {
		status = http.StatusForbidden
	}
	if status == 0 {
		status = http.StatusOK
	}
	c.Status(status)
	if len(env.Body) > 0 {
		return c.JSON(env.Body)
	}
	return c.JSON(fiber.Map{"action": env.Action, "reason": env.Reason})
}

func setConditionHeaders(c fiber.Ctx, e condition.LifecycleEvaluation) {
	action := displayAction(e)
	c.Set("X-Condition-Action", action)
	c.Set("X-Condition-Reason", e.FinalReason)
	c.Set("X-Condition-Route", e.Route.ID)
	if e.Enforcement != nil {
		if e.Enforcement.Reason != "" {
			c.Set("X-Condition-Reason", e.Enforcement.Reason)
		}
		setEnforcementHeaders(c, e.Enforcement)
	}
}

func setEnforcementHeaders(c fiber.Ctx, env *condition.EnforcementEnvelope) {
	if env == nil || env.Action == "" {
		return
	}
	for key, value := range env.Headers {
		c.Set(key, value)
	}
	if state := strings.Join(nonEmptyStrings(env.Chain, env.Watch, env.Step), "/"); state != "" {
		c.Set("X-Condition-State", state)
	}
	c.Set("X-Condition-Severity", env.Severity)
	if env.RetryAfterSeconds > 0 {
		c.Set("Retry-After", fmt.Sprint(env.RetryAfterSeconds))
	}
}

func loginHandler(c fiber.Ctx) error {
	format := bodyFormat(c.Get(fiber.HeaderContentType))
	body := bodyFact(c.Get(fiber.HeaderContentType), c.Body())
	username := stringFromBody(body, "username")
	password := stringFromBody(body, "password")
	if !validCredential(username, password) {
		c.Status(http.StatusUnauthorized)
		if format == "form" {
			c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
			return c.SendString("invalid password")
		}
		return c.JSON(fiber.Map{"error": "invalid_password", "username": username})
	}
	return c.JSON(fiber.Map{"ok": true, "username": username})
}

func validCredential(username, password string) bool {
	expected, ok := demoCredentials[username]
	return ok && password == expected
}

func adminReportsHandler(c fiber.Ctx) error {
	if c.Get("X-Role") != "admin" {
		c.Status(http.StatusForbidden)
		return c.JSON(fiber.Map{"error": "admin role required"})
	}
	return c.JSON(fiber.Map{"reports": []string{"risk", "audit", "access"}})
}

func documentHandler(c fiber.Ctx) error {
	if c.Get("X-Document-Access") != "allow" {
		c.Status(http.StatusNotFound)
		return c.JSON(fiber.Map{"error": "document not found", "document_id": c.Params("document_id")})
	}
	return c.JSON(fiber.Map{"document_id": c.Params("document_id"), "title": "Quarterly access review"})
}

func writeJSON(c fiber.Ctx, value any, err error) error {
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return c.JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(value)
}

func bodyFact(contentType string, body []byte) any {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return ""
	}
	switch bodyFormat(contentType) {
	case "json":
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			return value
		}
	case "form":
		values, err := url.ParseQuery(string(body))
		if err == nil {
			out := map[string]any{}
			for key, vals := range values {
				if len(vals) == 1 {
					out[key] = vals[0]
				} else {
					out[key] = vals
				}
			}
			return out
		}
	}
	return string(body)
}

func bodyFormat(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(contentType)
	}
	switch {
	case strings.Contains(mediaType, "json"):
		return "json"
	case strings.Contains(mediaType, "form"):
		return "form"
	case strings.Contains(mediaType, "xml"):
		return "xml"
	case strings.Contains(mediaType, "html"):
		return "html"
	case strings.HasPrefix(mediaType, "text/"):
		return "text"
	default:
		return "raw"
	}
}

func headersFromFiber(headers map[string][]string) map[string]any {
	out := map[string]any{}
	for key, values := range headers {
		name := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		if len(values) == 1 {
			out[name] = values[0]
		} else {
			out[name] = append([]string(nil), values...)
		}
	}
	return out
}

func stringFromBody(body any, key string) string {
	fields, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	value, _ := fields[key].(string)
	return value
}

func nonEmptyStrings(values ...string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func actorKey(c fiber.Ctx, body any) string {
	for _, key := range []string{"X-Actor", "X-User", "X-Principal"} {
		if value := c.Get(key); value != "" {
			return value
		}
	}
	if username := stringFromBody(body, "username"); username != "" {
		return username
	}
	if c.Path() != "" {
		return c.Path()
	}
	return "anonymous"
}

func stateSummary(ctx context.Context, store *storage.MemoryStore, actor string) []storage.ChainStateRecord {
	var states []storage.ChainStateRecord
	for _, item := range []struct {
		chain string
		watch string
	}{
		{"account_risk_chain", "failed_login_escalation"},
		{"account_risk_chain", "takeover_correlation"},
		{"response_error_chain", "endpoint_errors"},
		{"response_error_chain", "unexpected_4xx"},
		{"response_error_chain", "app_errors"},
	} {
		state, err := store.GetChainState(ctx, item.chain, item.watch, actor)
		if err == nil {
			states = append(states, state)
		}
	}
	return states
}

func eventSummary(events []storage.ChainEventRecord) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		out = append(out, map[string]any{
			"chain":      event.Chain,
			"watch":      event.Watch,
			"entity_key": event.EntityKey,
			"event":      event.EventType,
			"severity":   event.Severity,
			"reason":     event.ReasonCode,
			"created_at": event.CreatedAt,
			"attributes": event.Attributes,
		})
	}
	return out
}

func actionSummary(records []storage.ActionDeliveryRecord) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			"action":      record.Action,
			"status":      record.Status,
			"sink":        record.Sink,
			"handled":     record.Handled,
			"entity_key":  record.EntityKey,
			"reason_code": record.ReasonCode,
			"severity":    record.Severity,
			"created_at":  record.CreatedAt,
		})
	}
	return out
}

func curlExamples(addr string) []string {
	base := "http://127.0.0.1" + addr
	return []string{
		"curl -s " + base + "/",
		"curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"correct\"}' " + base + "/login",
		"curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login",
		"curl -i -H 'Content-Type: application/x-www-form-urlencoded' -d 'grant_type=password&username=alice&password=bad' " + base + "/login",
		"for i in {1..3}; do curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login; done",
		"sleep 121 && for i in {1..3}; do curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login; done",
		"sleep 301 && for i in {1..3}; do curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login; done",
		"sleep 1801 && for i in {1..3}; do curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login; done",
		"sleep 86401 && for i in {1..3}; do curl -i -H 'Content-Type: application/json' -d '{\"username\":\"alice\",\"password\":\"bad\"}' " + base + "/login; done",
		"curl -i -H 'X-Actor: analyst-1' -H 'X-Role: analyst' " + base + "/admin/reports",
		"curl -i -H 'X-Actor: admin-1' -H 'X-Role: admin' " + base + "/admin/reports",
		"curl -i -H 'X-Document-Access: allow' " + base + "/documents/doc-123",
		"curl -i " + base + "/documents/missing",
		"for i in {1..5}; do curl -i " + base + "/fail/endpoint; done",
		"for i in {1..5}; do curl -i " + base + "/fail/app/database; done",
		"curl -s " + base + "/_coverage",
		"curl -s '" + base + "/_state?actor=alice'",
		"curl -s '" + base + "/_events?actor=alice'",
		"curl -s " + base + "/_actions",
		"curl -s " + base + "/_incidents",
		"curl -s " + base + "/_readiness",
	}
}

func policyFiles() []string {
	return []string{
		"decision.bcl",
		"rules/package.bcl",
		"rules/catalogs.bcl",
		"rules/routes.bcl",
		"rules/overlays.bcl",
		"rules/decisions/pre_request_guard.bcl",
		"rules/decisions/response_observability.bcl",
		"rules/endpoints/login.bcl",
		"rules/endpoints/admin_reports.bcl",
		"rules/endpoints/documents.bcl",
		"rules/endpoints/failures.bcl",
		"rules/chains/account_risk.bcl",
		"rules/chains/response_errors.bcl",
		"rules/lifecycle.bcl",
		"tests/lifecycle_tests.bcl",
	}
}

func runDemo(app *fiber.App, advance func(time.Duration)) {
	requests := []struct {
		label  string
		wait   time.Duration
		req    func() *http.Request
		repeat int
	}{
		{label: "bob bad login 1", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"bad"}`, nil)
		}},
		{label: "bob bad login 2", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"bad"}`, nil)
		}},
		{label: "bob successful reset", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"bob","password":"correct"}`, nil)
		}},
		{label: "alice failed login evidence", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil)
		}, repeat: 3},
		{label: "alice blocked during 2m rate limit", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil)
		}},
		{label: "advance past 2m, grace then 5m rate limit", wait: 2*time.Minute + time.Second, req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/x-www-form-urlencoded", "grant_type=password&username=alice&password=bad", nil)
		}, repeat: 3},
		{label: "advance past 5m, grace then 30m block", wait: 5*time.Minute + time.Second, req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil)
		}, repeat: 3},
		{label: "advance past 30m, grace then 24h suspend", wait: 30*time.Minute + time.Second, req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil)
		}, repeat: 3},
		{label: "advance past 24h, grace then lock/ban", wait: 24*time.Hour + time.Second, req: func() *http.Request {
			return mustRequest(http.MethodPost, "/login", "application/json", `{"username":"alice","password":"bad"}`, nil)
		}, repeat: 3},
		{label: "admin denial", req: func() *http.Request {
			return mustRequest(http.MethodGet, "/admin/reports", "", "", map[string]string{"X-Actor": "analyst-1", "X-Role": "analyst"})
		}},
		{label: "admin allowed", req: func() *http.Request {
			return mustRequest(http.MethodGet, "/admin/reports", "", "", map[string]string{"X-Actor": "admin-1", "X-Role": "admin"})
		}},
		{label: "document allowed", req: func() *http.Request {
			return mustRequest(http.MethodGet, "/documents/doc-123", "", "", map[string]string{"X-Document-Access": "allow"})
		}},
		{label: "unexpected 404", req: func() *http.Request { return mustRequest(http.MethodGet, "/documents/missing", "", "", nil) }},
		{label: "endpoint 5xx burst", req: func() *http.Request { return mustRequest(http.MethodGet, "/fail/endpoint", "", "", nil) }, repeat: 5},
		{label: "app 5xx burst", req: func() *http.Request { return mustRequest(http.MethodGet, "/fail/app/database", "", "", nil) }, repeat: 5},
		{label: "state", req: func() *http.Request { return mustRequest(http.MethodGet, "/_state?actor=alice", "", "", nil) }},
		{label: "events", req: func() *http.Request { return mustRequest(http.MethodGet, "/_events?actor=alice", "", "", nil) }},
		{label: "actions", req: func() *http.Request { return mustRequest(http.MethodGet, "/_actions", "", "", nil) }},
	}
	for _, item := range requests {
		if item.wait > 0 {
			advance(item.wait)
		}
		repeat := item.repeat
		if repeat == 0 {
			repeat = 1
		}
		for i := 0; i < repeat; i++ {
			req := item.req()
			resp, err := app.Test(req, fiber.TestConfig{Timeout: 3 * time.Second})
			if err != nil {
				log.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			fmt.Printf("%-42s %s %-18s status=%d action=%s state=%s retry=%s route=%s body=%s\n",
				item.label,
				req.Method,
				req.URL.Path,
				resp.StatusCode,
				resp.Header.Get("X-Condition-Action"),
				resp.Header.Get("X-Condition-State"),
				resp.Header.Get("Retry-After"),
				resp.Header.Get("X-Condition-Route"),
				compactBody(body),
			)
		}
	}
	fmt.Println("Run with --serve --addr :8082 for curlable endpoints.")
}

func mustRequest(method, path, contentType, body string, headers map[string]string) *http.Request {
	req, err := http.NewRequest(method, "http://example.test"+path, strings.NewReader(body))
	if err != nil {
		panic(err)
	}
	if body != "" {
		req.ContentLength = int64(len(body))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func compactBody(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 500 {
		return text[:497] + "..."
	}
	return text
}
