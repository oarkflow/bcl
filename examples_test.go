package bcl

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExamplesParse(t *testing.T) {
	err := filepath.WalkDir("example", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isExampleSourceFile(path) {
			return nil
		}
		if _, err := ParsePath(path); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExamplesCompileEntryPoints(t *testing.T) {
	env := func(key string) (string, bool) {
		values := map[string]string{
			"DATABASE_URL":       "postgres://example/processgate",
			"TLS_KEY":            "/etc/processgate/tls.key",
			"WORKERS":            "12",
			"DB_NAME":            "processgate",
			"DB_USER":            "processgate",
			"IDENTITY_API_TOKEN": "example-token",
			"FEATURE_API_KEY":    "feature-token",
			"API_TOKEN":          "api-token",
			"ACCOUNT_DB_DSN":     ":memory:",
			"APP_ENV":            "prod",
		}
		v, ok := values[key]
		return v, ok
	}
	for _, path := range []string{"example/main.bcl", "example/app.bcl", "example/workflows.bcl", "example/expressions.bcl"} {
		t.Run(path, func(t *testing.T) {
			if _, err := CompileFile(path, &Options{
				AllowEnv:       true,
				ResolveImports: true,
				ResolveModules: true,
				Env:            env,
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFeatureExamplesCompile(t *testing.T) {
	env := func(key string) (string, bool) {
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
		v, ok := values[key]
		return v, ok
	}
	err := filepath.WalkDir("example/features", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isExampleSourceFile(path) {
			return nil
		}
		t.Run(path, func(t *testing.T) {
			if _, err := CompileFile(path, &Options{
				AllowEnv:       true,
				AllowTime:      true,
				ResolveImports: true,
				ResolveModules: true,
				Profile:        "prod",
				Env:            env,
				Context:        exampleContext(),
				Session:        exampleSession(),
			}); err != nil {
				t.Fatal(err)
			}
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isExampleSourceFile(path string) bool {
	switch filepath.Ext(path) {
	case ".bcl", ".schema":
		return true
	default:
		return false
	}
}

func TestVSCodeExtensionAssociatesSchemaFilesWithBCL(t *testing.T) {
	data, err := os.ReadFile("editors/vscode/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Contributes struct {
			Languages []struct {
				ID               string   `json:"id"`
				Extensions       []string `json:"extensions"`
				FilenamePatterns []string `json:"filenamePatterns"`
			} `json:"languages"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, lang := range manifest.Contributes.Languages {
		if lang.ID != "bcl" {
			continue
		}
		if !containsString(lang.Extensions, ".schema") {
			t.Fatalf("bcl language extension list should include .schema: %#v", lang.Extensions)
		}
		if !containsString(lang.FilenamePatterns, "*.schema") {
			t.Fatalf("bcl language filename patterns should include *.schema: %#v", lang.FilenamePatterns)
		}
		return
	}
	t.Fatal("missing bcl language contribution")
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func exampleContext() map[string]any {
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

func exampleSession() map[string]any {
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

func TestFeatureExampleProgramsRun(t *testing.T) {
	dirs, err := filepath.Glob("example/features/*/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no runnable feature examples found")
	}
	for _, mainPath := range dirs {
		dir := filepath.Dir(mainPath)
		if strings.Contains(dir, "/internal/") {
			continue
		}
		t.Run(dir, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./"+dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("feature example failed: %v\n%s", err, out)
			}
		})
	}
}

func TestDecisionPlatformUseCasesCompile(t *testing.T) {
	paths, err := filepath.Glob("examples/bcl_decision_platform/use_cases/*/decision.bcl")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no decision platform use cases found")
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			if _, err := CompileDecisionFile(path, &Options{AllowTime: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDecisionPlatformUseCaseProgramsRun(t *testing.T) {
	paths, err := filepath.Glob("examples/bcl_decision_platform/use_cases/*/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no runnable decision platform use cases found")
	}
	for _, mainPath := range paths {
		dir := filepath.Dir(mainPath)
		if strings.Contains(dir, "/internal/") {
			continue
		}
		t.Run(dir, func(t *testing.T) {
			cmd := exec.Command("go", "run", "./"+dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("decision platform use case failed: %v\n%s", err, out)
			}
		})
	}
}

func TestCommunicationsProviderRouting(t *testing.T) {
	program, err := CompileDecisionFile("examples/bcl_decision_platform/use_cases/communications-provider-routing/decision.bcl", &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	engine := NewDecisionEngine(program, nil)

	cases := []struct {
		name   string
		input  map[string]any
		effect string
		rankID string
	}{
		{
			name:   "unchecked campaign denies before ranking",
			effect: "deny",
			input: map[string]any{
				"user":    map[string]any{"id": "u-1", "country": "NP", "tier": "standard"},
				"message": map[string]any{"channel": "sms", "type": "marketing", "compliance_checked": false, "quality_floor": 0.98},
			},
		},
		{
			name:   "sms route selects best active country provider",
			effect: "allow",
			rankID: "sms-np-primary",
			input: map[string]any{
				"user":    map[string]any{"id": "u-2", "country": "NP", "tier": "premium"},
				"message": map[string]any{"channel": "sms", "type": "otp", "compliance_checked": true, "quality_floor": 0.98},
			},
		},
		{
			name:   "email route selects email capable provider",
			effect: "allow",
			rankID: "email-us-primary",
			input: map[string]any{
				"user":    map[string]any{"id": "u-3", "country": "US", "tier": "standard"},
				"message": map[string]any{"channel": "email", "type": "transactional", "compliance_checked": true, "quality_floor": 0.99},
			},
		},
		{
			name:   "no eligible provider reviews fallback",
			effect: "require_review",
			input: map[string]any{
				"user":    map[string]any{"id": "u-4", "country": "IR", "tier": "standard"},
				"message": map[string]any{"channel": "sms", "type": "otp", "compliance_checked": true, "quality_floor": 0.98},
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.Evaluate("communications_provider_routing", tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Effect != tt.effect {
				t.Fatalf("effect = %s, want %s: %#v", result.Effect, tt.effect, result)
			}
			if tt.rankID != "" && (result.Rank == nil || result.Rank.ID != tt.rankID) {
				t.Fatalf("rank = %#v, want %s", result.Rank, tt.rankID)
			}
			if tt.rankID == "" && result.Effect != "allow" && result.Rank != nil {
				t.Fatalf("non-routing result should not require a selected provider: %#v", result.Rank)
			}
		})
	}
}

func TestNextWaveFeatureExampleTestsRun(t *testing.T) {
	result, err := TestFile("example/features/17-next-wave-dsl.bcl", &Options{
		AllowEnv:       true,
		ResolveImports: true,
		ResolveModules: true,
		Env: func(key string) (string, bool) {
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed || len(result.Tests) != 2 {
		t.Fatalf("next-wave feature tests failed: %#v", result)
	}
}

func TestCompleteDecisionEssentialsExample(t *testing.T) {
	prog, err := CompileDecisionFile("examples/bcl/packages/complete_decision_essentials.bcl", &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := EvaluateDecision(prog, "complete_review", map[string]any{
		"time": map[string]any{"now": "2026-05-20T00:00:00Z"},
		"customer": map[string]any{
			"verified":    true,
			"blacklisted": false,
			"segment":     "prime",
		},
		"request": map[string]any{
			"kind":    "loan",
			"amount":  int64(60000),
			"channel": "mobile",
			"tags":    []any{"prime", "existing_customer"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Effect != "allow" || len(result.Obligations) != 1 || len(result.Advice) != 1 {
		t.Fatalf("complete decision result = %#v", result)
	}
	report, err := EvaluateDecisionDataset(prog, "complete_review", "complete_review_batch", nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.EffectCounts["allow"] != 1 || report.EffectCounts["deny"] != 1 || report.EffectCounts["require_review"] != 1 {
		t.Fatalf("complete decision batch report = %#v", report)
	}
	gates, err := EvaluateDecisionGates(prog, "complete_review_bundle", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !gates.Passed || len(gates.Results) != 1 {
		t.Fatalf("complete decision gates = %#v", gates)
	}
	suite, err := TestFile("examples/bcl/packages/complete_decision_essentials.bcl", &Options{AllowTime: true})
	if err != nil {
		t.Fatal(err)
	}
	if !suite.Passed || len(suite.Tests) != 2 {
		t.Fatalf("complete decision test matrix = %#v", suite)
	}
}

func TestExpressionExampleFastPathPredicates(t *testing.T) {
	doc := mustParsePath(t, "example/expressions.bcl")
	diags := Validate(doc, &Options{Strict: true})
	for _, d := range diags {
		if d.Severity == "error" {
			t.Fatalf("expression example validation error: %#v", d)
		}
	}
	vars := map[string]any{
		"subject": map[string]any{
			"id":         "u1",
			"roles":      []any{"member", "admin"},
			"status":     "active",
			"department": "Engineering",
		},
		"resource": map[string]any{
			"owner_id": "u1",
		},
		"request": map[string]any{
			"id":    "r1",
			"path":  "/admin/settings",
			"score": 95,
		},
	}
	for _, expr := range []string{
		`subject.roles has_any ["admin", "superadmin"]`,
		`subject.status != "blocked"`,
		`resource.owner_id == subject.id`,
		`request.path matches regex("^/admin/.*")`,
		`request.id exists`,
		`request.optional empty`,
	} {
		prog, err := CompileExpression(expr)
		if err != nil {
			t.Fatalf("compile %q: %v", expr, err)
		}
		got, err := prog.Eval(vars, nil)
		if err != nil {
			t.Fatalf("eval %q: %v", expr, err)
		}
		if !truthy(got) {
			t.Fatalf("expected %q to be true", expr)
		}
	}
}

func TestWorkflowExampleDefinesGraph(t *testing.T) {
	n, err := CompileFile("example/workflows.bcl", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Blocks) == 0 {
		t.Fatal("workflow example did not compile any blocks")
	}
	pipeline := n.Blocks[0]["body"].(map[string]any)
	steps, ok := pipeline["step"].([]any)
	if !ok || len(steps) < 4 {
		t.Fatalf("expected workflow steps, got %#v", pipeline["step"])
	}
	connections, ok := pipeline["connection"].([]any)
	if !ok || len(connections) < 4 {
		t.Fatalf("expected workflow connections, got %#v", pipeline["connection"])
	}
	diags := Validate(mustParsePath(t, "example/workflows.bcl"), nil)
	for _, d := range diags {
		if d.Severity == "error" {
			t.Fatalf("workflow validation error: %#v", d)
		}
	}
}

func TestGoExampleRuns(t *testing.T) {
	cmd := exec.Command("go", "run", "./example")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go example failed: %v\n%s", err, out)
	}
}
