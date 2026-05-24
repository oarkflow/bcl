package runner

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/oarkflow/bcl"
)

func Run() {
	_, caller, _, ok := runtime.Caller(1)
	if !ok {
		log.Fatal("cannot locate feature example")
	}
	path := filepath.Join(filepath.Dir(caller), "main.bcl")
	n, err := bcl.CompileFile(path, &bcl.Options{
		AllowEnv:       true,
		AllowTime:      true,
		ResolveImports: true,
		ResolveModules: true,
		Profile:        "prod",
		Env:            env,
		Context:        contextValues(),
		Session:        sessionValues(),
	})
	if err != nil {
		log.Fatal(err)
	}
	out, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(out))
}

func contextValues() map[string]any {
	return map[string]any{
		"app":         "ProcessGate",
		"environment": "prod",
		"region":      "NP",
		"request": map[string]any{
			"id":    "req-001",
			"ip":    "10.1.2.3",
			"path":  "/admin/settings",
			"score": 95,
			"flags": []any{"admin", "interactive"},
		},
		"user": map[string]any{
			"id": "user-001",
		},
		"tenant": map[string]any{
			"id": "tenant-001",
		},
		"network": map[string]any{
			"ip": "10.1.2.3",
		},
	}
}

func sessionValues() map[string]any {
	return map[string]any{
		"id":         "sess-001",
		"created_at": "2026-05-17T10:30:00Z",
		"expires_in": "30m",
		"subject": map[string]any{
			"id": "user-001",
		},
		"attrs": map[string]any{
			"mfa":    true,
			"device": "trusted",
		},
	}
}

func env(key string) (string, bool) {
	values := map[string]string{
		"DATABASE_URL":     "postgres://example/processgate",
		"DEV_DATABASE_URL": "postgres://example/dev",
		"FEATURE_API_KEY":  "feature-token",
		"API_TOKEN":        "api-token",
		"ACCOUNT_DB_DSN":   ":memory:",
		"APP_ENV":          "prod",
		"HOST":             "127.0.0.1",
		"PORT":             "8080",
		"DEBUG":            "false",
		"TIMEOUT":          "5s",
		"MAX_BODY":         "2MB",
		"LABELS":           "feature,example",
	}
	if v, ok := values[key]; ok {
		return v, true
	}
	return os.LookupEnv(key)
}
