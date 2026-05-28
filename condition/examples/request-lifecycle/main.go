package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/oarkflow/condition/examples/internal/examplebase"
	condition "github.com/oarkflow/condition/pkg/condition"
	"github.com/oarkflow/condition/pkg/storage"
)

func main() {
	serve := flag.Bool("serve", false, "run an HTTP server instead of demo requests")
	addr := flag.String("addr", ":8081", "HTTP server address")
	flag.Parse()

	svc := condition.NewService(storage.NewMemoryStore(), condition.Config{
		Environment:  "example",
		RequireTests: true,
		Runtime: condition.RuntimePolicy{ActionAllowlists: []condition.ActionAllowlist{
			{TenantID: "default", Environment: "example", Actions: []string{"log", "notify", "escalate", "endpoint_5xx", "app_5xx", "unexpected_4xx", "healthy"}, Sinks: []string{"event", "log"}},
		}},
	})
	if _, err := svc.Publish(context.Background(), condition.PublishRequest{Name: "request-lifecycle", Version: "1", Path: filepath.Join(examplebase.Dir(), "decision.bcl"), RunTests: true}); err != nil {
		log.Fatal(err)
	}
	handler := lifecycleMiddleware(svc, routes(svc))
	if *serve {
		fmt.Printf("request lifecycle listening on http://127.0.0.1%s\n", *addr)
		for _, c := range curlExamples(*addr) {
			fmt.Println("  " + c)
		}
		log.Fatal(http.ListenAndServe(*addr, handler))
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	for _, path := range []string{"/ok", "/bad-request", "/unauthorized", "/forbidden", "/teapot", "/endpoint-error", "/endpoint-error", "/endpoint-error", "/missing", "/app-error/db", "/app-error/db", "/app-error/db"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			log.Fatal(err)
		}
		resp.Body.Close()
		fmt.Printf("%s status=%d action=%s reason=%s\n", path, resp.StatusCode, resp.Header.Get("X-Lifecycle-Action"), resp.Header.Get("X-Lifecycle-Reason"))
	}
}

func curlExamples(addr string) []string {
	base := "http://127.0.0.1" + addr
	return []string{
		"curl -i " + base + "/ok",
		"curl -i " + base + "/bad-request",
		"curl -i " + base + "/unauthorized",
		"curl -i " + base + "/forbidden",
		"curl -i " + base + "/teapot",
		"curl -i " + base + "/missing",
		"for i in {1..5}; do curl -i " + base + "/endpoint-error; done",
		"for i in {1..5}; do curl -i " + base + "/app-error/db; done",
		"curl -s " + base + "/_coverage",
		"curl -s " + base + "/_actions",
		"curl -s " + base + "/_incidents",
		"curl -s " + base + "/_readiness",
	}
}

func routes(svc *condition.Service) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_coverage", func(w http.ResponseWriter, r *http.Request) {
		report, err := svc.RouteCoverage(r.Context(), "request-lifecycle")
		writeExampleJSON(w, report, err)
	})
	mux.HandleFunc("/_actions", func(w http.ResponseWriter, r *http.Request) {
		records, err := svc.ListActionDeliveries(r.Context(), storage.ActionDeliveryQuery{Limit: 50})
		writeExampleJSON(w, records, err)
	})
	mux.HandleFunc("/_incidents", func(w http.ResponseWriter, r *http.Request) {
		records, err := svc.ListIncidents(r.Context(), storage.IncidentQuery{Limit: 50})
		writeExampleJSON(w, records, err)
	})
	mux.HandleFunc("/_readiness", func(w http.ResponseWriter, r *http.Request) {
		writeExampleJSON(w, svc.ProductionReadiness(r.Context()), nil)
	})
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/bad-request", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request is expected", http.StatusBadRequest)
	})
	mux.HandleFunc("/unauthorized", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "auth required is expected", http.StatusUnauthorized)
	})
	mux.HandleFunc("/forbidden", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden is expected", http.StatusForbidden)
	})
	mux.HandleFunc("/teapot", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unexpected client error", http.StatusTeapot)
	})
	mux.HandleFunc("/endpoint-error", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "endpoint failed", http.StatusInternalServerError)
	})
	mux.HandleFunc("/app-error/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "app failed "+strings.TrimPrefix(r.URL.Path, "/app-error/"), http.StatusBadGateway)
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

func lifecycleMiddleware(svc *condition.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		appKey := "request-lifecycle-demo"
		if rec.Code >= 400 && rec.Code < 500 && rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			appKey = "request-lifecycle-client-errors"
		}
		resp, err := svc.EvaluateLifecycle(r.Context(), "request-lifecycle", "http_request", condition.LifecycleEvaluateRequest{
			Phase:  "post",
			Method: r.Method,
			Path:   r.URL.Path,
			Request: map[string]any{
				"headers": headersFromHTTP(r.Header),
				"body":    map[string]any{},
				"format":  requestFormat(r.Header),
			},
			Input: map[string]any{
				"request": map[string]any{"actor_key": r.URL.Path, "application_key": appKey},
			},
			Response: map[string]any{
				"status":  rec.Code,
				"headers": headersFromHTTP(rec.Header()),
				"body":    map[string]any{"bytes": rec.Body.Len()},
				"format":  responseFormat(rec.Header()),
			},
		})
		if err == nil {
			rec.Header().Set("X-Lifecycle-Action", resp.Evaluation.FinalAction)
			rec.Header().Set("X-Lifecycle-Reason", resp.Evaluation.FinalReason)
		}
		for key, values := range rec.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
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
