package bcl

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRequirementsSyntaxAndDefaults(t *testing.T) {
	src := []byte(`
bcl {
  version "1.0"
  strict true
}

type Action = string

schema action {
  required name Action
  optional enabled bool default true
}

name "ProcessGate"
log_path "/var/log/${app.name}.log"

headers {
  "Accept" "application/json"
}

action "notify" {
  name "notify-security"
}

request {
  body json {
    subject_id subject.id
  }
}
`)
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Compile(doc, &Options{})
	if err != nil {
		t.Fatal(err)
	}
	if n.Types["Action"] != "string" {
		t.Fatalf("missing alias: %#v", n.Types)
	}
	if n.Body["log_path"] != "/var/log/ProcessGate.log" {
		t.Fatalf("interpolation failed: %#v", n.Body["log_path"])
	}
	headers := n.Body["headers"].(map[string]any)
	if headers["Accept"] != "application/json" {
		t.Fatalf("quoted key failed: %#v", headers)
	}
	actionBody := n.Blocks[0]["body"].(map[string]any)
	if actionBody["enabled"] != true {
		t.Fatalf("schema default missing: %#v", actionBody)
	}
}

func TestLockfileEnforcement(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "common.bcl"), `name "common"`)
	root := filepath.Join(dir, "main.bcl")
	mustWrite(t, root, `import "./common.bcl"`)
	doc := mustParsePath(t, root)
	lock, err := GenerateLockfile(doc, dir)
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "bcl.lock")
	if err := WriteLockfile(lockPath, lock); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileFileWithLock(root, lockPath, &Options{Strict: true, AllowEnv: true}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "common.bcl"), `name "changed"`)
	if _, err := CompileFileWithLock(root, lockPath, &Options{Strict: true, AllowEnv: true}); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestIntegrationValidation(t *testing.T) {
	doc, err := Parse([]byte(`
http "bad" {
  base_url "http://127.0.0.1:8080"
}

command "bad" {
  args ["rm", "-rf", "/"]
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) < 2 {
		t.Fatalf("expected integration diagnostics: %#v", diags)
	}
}

func TestSimulateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.bcl")
	mustWrite(t, path, `
evaluation {
  strategy deny_overrides
  default deny
}

rule "admin" {
  effect allow
  priority 10

  when {
    subject.roles has_any ["admin"]
  }
}
`)
	result, err := SimulateFile(path, map[string]any{
		"subject": map[string]any{"roles": []any{"admin"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matched) != 1 {
		t.Fatalf("expected match: %#v", result)
	}
	if result.Decision["effect"] != "allow" {
		t.Fatalf("expected allow decision: %#v", result.Decision)
	}
}

func TestHTTPValidationDetails(t *testing.T) {
	doc, err := Parse([]byte(`
http "api" {
  base_url "https://api.example.com"

  auth {
    type bearer
  }
}

action "bad" {
  type http

  request {
    method TRACE

    body xml {
      value "bad"
    }
  }

  response {
    expect_status "ok"
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) < 4 {
		t.Fatalf("expected detailed HTTP validation diagnostics: %#v", diags)
	}
}

func TestCLISmoke(t *testing.T) {
	input := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(input, []byte(`{"subject":{"roles":["admin"]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"run", "./cmd/bcl", "validate", "--strict", "example/workflows.bcl"},
		{"run", "./cmd/bcl", "compile", "--allow-env", "example/workflows.bcl"},
		{"run", "./cmd/bcl", "explain", "--input", input, "example/workflows.bcl"},
		{"run", "./cmd/bcl", "simulate", "--input", input, "example/workflows.bcl"},
	} {
		cmd := exec.Command("go", args...)
		cache := filepath.Join(t.TempDir(), "gocache")
		cmd.Env = append(os.Environ(),
			"GOCACHE="+cache,
			"DATABASE_URL=postgres://example/processgate",
			"TLS_KEY=/tmp/key",
			"DB_NAME=processgate",
			"DB_USER=processgate",
			"IDENTITY_API_TOKEN=example-token",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v failed: %v\n%s", args, err, out)
		}
	}
}
