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
	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

const definitionName = "anomaly-detection"

type anomalyRuntime struct {
	service *condition.Service
	store   *storage.MemoryStore
}

func main() {
	serve := flag.Bool("serve", false, "run a Fiber HTTP server instead of demo requests")
	addr := flag.String("addr", ":8083", "Fiber server address")
	flag.Parse()

	if *serve {
		runtime := mustRuntime()
		app := newApp(runtime)
		fmt.Printf("anomaly detection Fiber example listening on http://127.0.0.1%s\n", *addr)
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

func mustRuntime() *anomalyRuntime {
	runtime, err := newRuntime()
	if err != nil {
		log.Fatal(err)
	}
	return runtime
}

func newRuntime() (*anomalyRuntime, error) {
	return newRuntimeWithClock(nil)
}

func newRuntimeWithClock(clock func() time.Time) (*anomalyRuntime, error) {
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
					"allow", "healthy", "monitor", "step_up", "challenge", "hold", "reject", "block", "suspend",
					"terminate_session", "notify", "escalate", "open_case", "quarantine", "session_anomaly",
					"geo_violation", "business_rule_violation", "payment_or_order_anomaly", "data_access_anomaly",
					"unexpected_4xx", "api_5xx",
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
	return &anomalyRuntime{service: svc, store: store}, nil
}

func newApp(runtime *anomalyRuntime) *fiber.App {
	app := fiber.New()
	app.Use(conditionLifecycle(runtime))

	app.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name": definitionName,
			"use_cases": []string{
				"session hijacking and step-up enforcement",
				"blocked country and data residency enforcement",
				"payment, order, support, logistics, and data anomalies",
				"unexpected 4xx and 5xx API health escalation",
				"action delivery, incidents, state, route coverage, and threat model inspection",
			},
			"routes": []string{
				"POST /session/continue",
				"POST /payments/authorize",
				"POST /orders/checkout",
				"POST /support/tickets",
				"POST /logistics/shipments",
				"POST /data/query",
				"GET|POST /api/resource",
				"GET /admin/risk-console",
			},
		})
	})

	app.Post("/session/continue", acceptHandler("session", "continued"))
	app.Post("/payments/authorize", acceptHandler("payment", "authorized"))
	app.Post("/orders/checkout", acceptHandler("order", "accepted"))
	app.Post("/support/tickets", acceptHandler("ticket", "created"))
	app.Post("/logistics/shipments", acceptHandler("shipment", "accepted"))
	app.Post("/data/query", acceptHandler("query", "accepted"))
	app.Get("/api/resource", apiResourceHandler)
	app.Post("/api/resource", acceptHandler("resource", "mutated"))
	app.Get("/admin/risk-console", acceptHandler("console", "opened"))

	app.Get("/_rules", func(c fiber.Ctx) error {
		lines := []string{"definition: decision.bcl", "", "files:"}
		for _, file := range policyFiles() {
			lines = append(lines, "  - "+file)
		}
		lines = append(lines, "", "curl examples:")
		lines = append(lines, curlExamples(":8083")...)
		return c.Type("text").SendString(strings.Join(lines, "\n") + "\n")
	})
	app.Get("/_threat-model", func(c fiber.Ctx) error {
		return c.JSON(threatModel())
	})
	app.Get("/_coverage", func(c fiber.Ctx) error {
		report, err := runtime.service.RouteCoverage(c.Context(), definitionName)
		return writeJSON(c, report, err)
	})
	app.Get("/_state", func(c fiber.Ctx) error {
		return writeJSON(c, stateSummary(c.Context(), runtime.store, c.Query("actor")), nil)
	})
	app.Get("/_events", func(c fiber.Ctx) error {
		events, err := runtime.store.QueryChainEvents(c.Context(), storage.ChainEventQuery{Definition: definitionName, EntityKey: c.Query("actor"), IncludeExpired: true, Limit: 100})
		return writeJSON(c, eventSummary(events), err)
	})
	app.Get("/_actions", func(c fiber.Ctx) error {
		records, err := runtime.service.ListActionDeliveries(c.Context(), storage.ActionDeliveryQuery{Definition: definitionName, Limit: 100})
		return writeJSON(c, actionSummary(records), err)
	})
	app.Get("/_incidents", func(c fiber.Ctx) error {
		records, err := runtime.service.ListIncidents(c.Context(), storage.IncidentQuery{Definition: definitionName, Limit: 100})
		return writeJSON(c, records, err)
	})
	app.Get("/_readiness", func(c fiber.Ctx) error {
		return writeJSON(c, runtime.service.ProductionReadiness(c.Context()), nil)
	})
	return app
}

func conditionLifecycle(runtime *anomalyRuntime) fiber.Handler {
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
		setConditionHeaders(c, preResp.Evaluation)
		if env := preResp.Evaluation.Enforcement; env != nil && env.Blocking {
			return writeConditionResponse(c, env)
		}

		err := c.Next()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			_ = c.JSON(fiber.Map{"error": err.Error()})
		}
		status := c.Response().StatusCode()
		responseBody := append([]byte(nil), c.Response().Body()...)
		resp, evalErr := evaluatePost(c, runtime.service, requestFacts, actor, status, responseBody)
		if evalErr != nil {
			c.Set("X-Condition-Error", evalErr.Error())
			return nil
		}
		setConditionHeaders(c, resp.Evaluation)
		if env := resp.Evaluation.Enforcement; env != nil && env.Blocking {
			return writeConditionResponse(c, env)
		}
		return nil
	}
}

func evaluatePre(c fiber.Ctx, svc *condition.Service, requestFacts map[string]any, actor string) (*condition.LifecycleEvaluateResponse, error) {
	return svc.EvaluateLifecycle(c.Context(), definitionName, "http_request", condition.LifecycleEvaluateRequest{
		Phase:   "pre",
		Method:  c.Method(),
		Path:    c.Path(),
		Request: requestFacts,
		Input:   lifecycleInput(actor),
	})
}

func evaluatePost(c fiber.Ctx, svc *condition.Service, requestFacts map[string]any, actor string, status int, responseBody []byte) (*condition.LifecycleEvaluateResponse, error) {
	return svc.EvaluateLifecycle(c.Context(), definitionName, "http_request", condition.LifecycleEvaluateRequest{
		Phase:    "post",
		Method:   c.Method(),
		Path:     c.Path(),
		Request:  requestFacts,
		Response: responseFacts(c, status, responseBody),
		Input:    lifecycleInput(actor),
	})
}

func lifecycleInput(actor string) map[string]any {
	return map[string]any{
		"request": map[string]any{
			"actor_key":       actor,
			"application_key": "anomaly-detection-demo",
		},
	}
}

func requestFacts(c fiber.Ctx) (map[string]any, string) {
	rawBody := append([]byte(nil), c.Body()...)
	body := bodyFact(c.Get(fiber.HeaderContentType), rawBody)
	facts := map[string]any{
		"headers": headersFromFiber(c.GetReqHeaders()),
		"body":    body,
		"format":  bodyFormat(c.Get(fiber.HeaderContentType)),
	}
	for _, section := range []string{"actor", "tenant", "session", "device", "network", "geo", "business", "payment", "order", "support", "logistics", "data"} {
		if value := nestedMap(body, section); len(value) > 0 {
			facts[section] = value
		}
	}
	if _, ok := facts["actor"]; !ok {
		facts["actor"] = map[string]any{"id": actorKey(c, body), "role": c.Get("X-Role")}
	}
	mergeHeaderDefaults(c, facts)
	return facts, actorKey(c, body)
}

func responseFacts(c fiber.Ctx, status int, responseBody []byte) map[string]any {
	return map[string]any{
		"status":  status,
		"headers": headersFromFiber(c.GetRespHeaders()),
		"body":    bodyFact(c.GetRespHeader(fiber.HeaderContentType), responseBody),
		"format":  bodyFormat(c.GetRespHeader(fiber.HeaderContentType)),
	}
}

func mergeHeaderDefaults(c fiber.Ctx, facts map[string]any) {
	putNestedDefault(facts, "tenant", "id", firstNonEmpty(c.Get("X-Tenant"), "tenant-demo"))
	putNestedDefault(facts, "tenant", "region", firstNonEmpty(c.Get("X-Tenant-Region"), "us"))
	putNestedDefault(facts, "tenant", "verified", c.Get("X-Tenant-Verified") != "false")
	putNestedDefault(facts, "geo", "country", firstNonEmpty(c.Get("X-Country"), "US"))
	putNestedDefault(facts, "geo", "region", firstNonEmpty(c.Get("X-Region"), "us"))
	putNestedDefault(facts, "device", "trusted", c.Get("X-Device-Trusted") != "false")
	putNestedDefault(facts, "network", "ip_reputation", 0)
	putNestedDefault(facts, "business", "business_hours", c.Get("X-Business-Hours") != "false")
}

func putNestedDefault(facts map[string]any, section, key string, value any) {
	m := mapFromAny(facts[section])
	if m == nil {
		m = map[string]any{}
		facts[section] = m
	}
	if _, exists := m[key]; !exists {
		m[key] = value
	}
}

func acceptHandler(key, value string) fiber.Handler {
	return func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true, key: value})
	}
}

func apiResourceHandler(c fiber.Ctx) error {
	switch c.Query("mode") {
	case "missing":
		c.Status(http.StatusNotFound)
		return c.JSON(fiber.Map{"error": "resource_not_found"})
	case "fail":
		c.Status(http.StatusInternalServerError)
		return c.JSON(fiber.Map{"error": "forced_failure"})
	default:
		return c.JSON(fiber.Map{"ok": true, "resource": "demo"})
	}
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

func displayAction(e condition.LifecycleEvaluation) string {
	if e.Enforcement != nil && e.Enforcement.Action != "" {
		return e.Enforcement.Action
	}
	if e.FinalAction != "" {
		return e.FinalAction
	}
	for i := len(e.Chains) - 1; i >= 0; i-- {
		if e.Chains[i].FinalAction != "" {
			return e.Chains[i].FinalAction
		}
	}
	for i := len(e.Actions) - 1; i >= 0; i-- {
		if e.Actions[i].Name != "" {
			return e.Actions[i].Name
		}
	}
	return ""
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

func actorKey(c fiber.Ctx, body any) string {
	for _, key := range []string{"X-Actor", "X-User", "X-Principal"} {
		if value := c.Get(key); value != "" {
			return value
		}
	}
	for _, key := range []string{"actor", "user", "customer", "account"} {
		if m := nestedMap(body, key); len(m) > 0 {
			if id := stringAny(m["id"]); id != "" {
				return id
			}
		}
	}
	if fields := mapFromAny(body); len(fields) > 0 {
		if id := stringAny(fields["actor"]); id != "" {
			return id
		}
	}
	return "anonymous"
}

func nestedMap(value any, key string) map[string]any {
	fields := mapFromAny(value)
	if fields == nil {
		return nil
	}
	return mapFromAny(fields[key])
}

func mapFromAny(value any) map[string]any {
	switch x := value.(type) {
	case nil:
		return nil
	case map[string]any:
		return x
	default:
		return nil
	}
}

func stringAny(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func stateSummary(ctx context.Context, store *storage.MemoryStore, actor string) []storage.ChainStateRecord {
	if actor == "" {
		actor = "anonymous"
	}
	var states []storage.ChainStateRecord
	for _, item := range []struct {
		chain string
		watch string
	}{
		{"identity_anomaly_chain", "identity_session"},
		{"geo_policy_chain", "geo_region_policy"},
		{"business_abuse_chain", "business_abuse"},
		{"commerce_risk_chain", "commerce_risk"},
		{"data_exfiltration_chain", "data_exfiltration"},
		{"api_health_chain", "api_health"},
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

func threatModel() map[string]any {
	return map[string]any{
		"assets": []string{"sessions", "payments", "orders", "support workflows", "shipments", "PII datasets", "API availability"},
		"actors": []string{"legitimate users", "compromised accounts", "fraud operators", "malicious insiders", "automation clients"},
		"abuse_paths": []string{
			"session token replay followed by sensitive access",
			"blocked-country access or cross-region data access",
			"discount, refund, support, and reroute workflow abuse",
			"payment velocity and shipping/billing mismatch",
			"bulk PII export and restricted dataset access",
			"API error bursts hiding automation or system failure",
		},
		"detection_signals": []string{"request body facts", "headers", "route metadata", "response status", "chain state", "durable events"},
		"controls":          []string{"step-up", "challenge", "hold", "block", "terminate session", "quarantine", "suspend", "notify", "escalate", "open case"},
	}
}

func policyFiles() []string {
	return []string{
		"decision.bcl",
		"rules/package.bcl",
		"rules/catalogs.bcl",
		"rules/routes.bcl",
		"rules/lifecycle.bcl",
		"rules/decisions/pre_request_guard.bcl",
		"rules/decisions/response_observability.bcl",
		"rules/decisions/session_anomalies.bcl",
		"rules/decisions/geo_policy.bcl",
		"rules/decisions/business_rules.bcl",
		"rules/decisions/commerce_risk.bcl",
		"rules/decisions/data_governance.bcl",
		"rules/chains/identity_anomaly.bcl",
		"rules/chains/geo_policy.bcl",
		"rules/chains/business_abuse.bcl",
		"rules/chains/commerce_risk.bcl",
		"rules/chains/data_exfiltration.bcl",
		"rules/chains/api_health.bcl",
		"tests/lifecycle_tests.bcl",
	}
}

func curlExamples(addr string) []string {
	base := "http://127.0.0.1" + addr
	return []string{
		"curl -s " + base + "/",
		"curl -i -H 'Content-Type: application/json' -d '{\"actor\":{\"id\":\"alice\"},\"session\":{\"impossible_travel\":true},\"device\":{\"trusted\":true},\"network\":{\"ip_reputation\":10}}' " + base + "/session/continue",
		"curl -i -H 'Content-Type: application/json' -d '{\"actor\":{\"id\":\"buyer-1\"},\"geo\":{\"country\":\"IR\",\"region\":\"blocked\"},\"payment\":{\"amount_vs_avg\":1,\"velocity_10m\":1,\"new_method\":false,\"shipping_country\":\"IR\",\"billing_country\":\"IR\"}}' " + base + "/payments/authorize",
		"curl -i -H 'Content-Type: application/json' -d '{\"actor\":{\"id\":\"buyer-2\"},\"geo\":{\"country\":\"US\",\"region\":\"us\"},\"payment\":{\"amount_vs_avg\":6,\"velocity_10m\":1,\"new_method\":false,\"shipping_country\":\"US\",\"billing_country\":\"US\"}}' " + base + "/payments/authorize",
		"curl -i -H 'Content-Type: application/json' -d '{\"actor\":{\"id\":\"tenant-user-1\"},\"tenant\":{\"verified\":false},\"support\":{\"ticket_spam\":true,\"severity\":1}}' " + base + "/support/tickets",
		"curl -i -H 'Content-Type: application/json' -d '{\"actor\":{\"id\":\"analyst-1\"},\"tenant\":{\"region\":\"us\",\"verified\":true},\"geo\":{\"country\":\"DE\",\"region\":\"eu\"},\"data\":{\"pii_export\":true,\"bulk_rows\":25000,\"restricted_dataset\":true,\"cross_region\":true}}' " + base + "/data/query",
		"for i in {1..5}; do curl -i '" + base + "/api/resource?mode=fail'; done",
		"curl -s " + base + "/_threat-model",
		"curl -s '" + base + "/_state?actor=alice'",
		"curl -s '" + base + "/_events?actor=alice'",
		"curl -s " + base + "/_actions",
		"curl -s " + base + "/_incidents",
		"curl -s " + base + "/_coverage",
		"curl -s " + base + "/_readiness",
	}
}

func runDemo(app *fiber.App, advance func(time.Duration)) {
	requests := []struct {
		label  string
		wait   time.Duration
		repeat int
		req    func() *http.Request
	}{
		{label: "session hijacking", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"impossible_travel":true},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil)
		}},
		{label: "blocked during active step-up", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/session/continue", "application/json", `{"actor":{"id":"alice"},"session":{"impossible_travel":false},"device":{"trusted":true},"network":{"ip_reputation":10}}`, nil)
		}},
		{label: "payment blocked country", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/payments/authorize", "application/json", `{"actor":{"id":"buyer-1"},"geo":{"country":"IR","region":"blocked"},"payment":{"amount_vs_avg":1,"velocity_10m":1,"new_method":false,"shipping_country":"IR","billing_country":"IR"}}`, nil)
		}},
		{label: "payment spike challenge", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/payments/authorize", "application/json", `{"actor":{"id":"buyer-2"},"geo":{"country":"US","region":"us"},"payment":{"amount_vs_avg":6,"velocity_10m":1,"new_method":false,"shipping_country":"US","billing_country":"US"}}`, nil)
		}},
		{label: "support abuse repeated", repeat: 2, req: func() *http.Request {
			return mustRequest(http.MethodPost, "/support/tickets", "application/json", `{"actor":{"id":"tenant-user-1"},"tenant":{"verified":false},"support":{"ticket_spam":true,"severity":1}}`, nil)
		}},
		{label: "data exfiltration quarantine", req: func() *http.Request {
			return mustRequest(http.MethodPost, "/data/query", "application/json", `{"actor":{"id":"analyst-1"},"tenant":{"region":"us","verified":true},"geo":{"country":"DE","region":"eu"},"data":{"pii_export":true,"bulk_rows":25000,"restricted_dataset":true,"cross_region":true}}`, nil)
		}},
		{label: "api failure burst", repeat: 5, req: func() *http.Request {
			return mustRequest(http.MethodGet, "/api/resource?mode=fail", "", "", nil)
		}},
		{label: "threat model", req: func() *http.Request { return mustRequest(http.MethodGet, "/_threat-model", "", "", nil) }},
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
			fmt.Printf("%-32s %s %-24s status=%d action=%s state=%s retry=%s route=%s body=%s\n",
				item.label,
				req.Method,
				req.URL.RequestURI(),
				resp.StatusCode,
				resp.Header.Get("X-Condition-Action"),
				resp.Header.Get("X-Condition-State"),
				resp.Header.Get("Retry-After"),
				resp.Header.Get("X-Condition-Route"),
				compactBody(body),
			)
		}
	}
	fmt.Println("Run with --serve --addr :8083 for curlable endpoints.")
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
