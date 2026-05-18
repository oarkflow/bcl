package bcl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileGenericDocument(t *testing.T) {
	src := []byte(`
bcl {
  version "1.0"
  strict true
}

import "./common.bcl" as common
const ADMIN_ROLES = ["admin", "superadmin"]

set "write-actions" {
  write
  update
  delete
}

tenant "org1" {
  name "Engineering Org"
  attrs {
    region "NP"
    tier "enterprise"
  }
}

policy "document-owner-or-admin" {
  tenant "org1"
  effect allow
  priority 100
  actions {
    read
    use set("write-actions")
  }
  when {
    resource.owner_id == subject.id
  }
}

engine {
  cache_ttl env.duration("CACHE_TTL", 5m)
  workers env.int("WORKERS", 8)
  strict_mode true
}
`)
	n, err := CompileBytes(src, &Options{
		AllowEnv: true,
		Env: func(key string) (string, bool) {
			if key == "WORKERS" {
				return "16", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if n.Version != "1.0" {
		t.Fatalf("version = %q", n.Version)
	}
	if n.Constants["ADMIN_ROLES"] == nil {
		t.Fatalf("missing constant")
	}
	if len(n.Sets["write-actions"]) != 3 {
		t.Fatalf("set length = %d", len(n.Sets["write-actions"]))
	}
	if n.Body["engine"] == nil {
		t.Fatalf("missing engine body")
	}
	if len(n.Blocks) < 2 {
		t.Fatalf("blocks = %#v", n.Blocks)
	}
}

func TestExpressionEvaluator(t *testing.T) {
	ok, err := Eval(`subject.roles has_any ["admin", "manager"]`, map[string]any{
		"subject": map[string]any{"roles": []any{"user", "admin"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok != true {
		t.Fatalf("expected true, got %#v", ok)
	}
	ok, err = Eval(`request.path matches regex("^/admin/.*")`, map[string]any{
		"request": map[string]any{"path": "/admin/users"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok != true {
		t.Fatalf("expected regex match")
	}
}

func TestStringQuoteForms(t *testing.T) {
	src := []byte("single 'subject.status != \"blocked\"'\nraw `subject.roles has_any [\"admin\", \"superadmin\"]`\n")
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body["single"] != `subject.status != "blocked"` {
		t.Fatalf("single quote string = %#v", n.Body["single"])
	}
	if n.Body["raw"] != `subject.roles has_any ["admin", "superadmin"]` {
		t.Fatalf("raw string = %#v", n.Body["raw"])
	}
	formatted, err := Format([]byte("expr \"subject.status != \\\"blocked\\\"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(formatted, []byte("expr `subject.status != \"blocked\"`")) {
		t.Fatalf("expected backtick formatted string, got %s", formatted)
	}
}

func TestContextSessionFunctions(t *testing.T) {
	src := []byte(`
runtime_scope {
  request_id context.required("request.id")
  score context.float("request.score", 0)
  session_id session.required("id")
  mfa session.bool("attrs.mfa", false)
  expires session.duration("expires_in", 5m)
}
`)
	n, err := CompileBytes(src, &Options{
		Context: map[string]any{
			"request": map[string]any{
				"id":    "req-001",
				"score": "91.5",
			},
		},
		Session: map[string]any{
			"id":         "sess-001",
			"expires_in": "30m",
			"attrs": map[string]any{
				"mfa": "true",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := n.Body["runtime_scope"].(map[string]any)
	if scope["request_id"] != "req-001" || scope["session_id"] != "sess-001" {
		t.Fatalf("scope = %#v", scope)
	}
	if scope["mfa"] != true {
		t.Fatalf("mfa = %#v", scope["mfa"])
	}
	if scope["expires"].(map[string]any)["$duration"] != "30m" {
		t.Fatalf("expires = %#v", scope["expires"])
	}
}

func TestEnvFileDirective(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.test"), []byte("APP_NAME=FromEnvFile\nPORT=9090\nSECRET='quoted value'\n"), 0644); err != nil {
		t.Fatal(err)
	}
	src := []byte(`
env_file ".env.test"
name env.required("APP_NAME")
port env.int("PORT", 8080)
secret env.required("SECRET")
`)
	doc, err := ParseFile(filepath.Join(dir, "app.bcl"), src)
	if err != nil {
		t.Fatal(err)
	}
	n, err := Compile(doc, &Options{AllowEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if n.Body["name"] != "FromEnvFile" || n.Body["port"] != int64(9090) || n.Body["secret"] != "quoted value" {
		t.Fatalf("body = %#v", n.Body)
	}
	if _, ok := n.Body["env_file"]; ok {
		t.Fatalf("env_file directive leaked into normalized body: %#v", n.Body)
	}
}

func TestMarshalUnmarshal(t *testing.T) {
	type Config struct {
		Name    string `bcl:"name"`
		Enabled bool   `bcl:"enabled"`
		Workers int    `bcl:"workers,omitempty"`
		Secret  string `bcl:"secret,sensitive"`
	}
	in := Config{Name: "ProcessGate", Enabled: true, Workers: 8, Secret: "s3cr3t"}
	data, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `secret sensitive("s3cr3t")`) {
		t.Fatalf("sensitive marshal missing: %s", data)
	}
	var out Config
	if err := Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Workers != in.Workers || !out.Enabled {
		t.Fatalf("round trip = %#v", out)
	}
}

func TestFormatAndDecode(t *testing.T) {
	src := []byte(`name "x"
roles { admin
superadmin
}
`)
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("roles [admin, superadmin]")) {
		t.Fatalf("formatted output: %s", out)
	}
	var dst map[string]any
	if err := Unmarshal(out, &dst); err != nil {
		t.Fatal(err)
	}
	if dst["name"] != "x" {
		t.Fatalf("decode = %#v", dst)
	}
}

func TestSchemaValidateDuplicate(t *testing.T) {
	doc, err := Parse([]byte(`
schema policy {
  required tenant string
  optional priority int default 0
}
policy "a" {}
policy "a" {}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) == 0 {
		t.Fatalf("expected duplicate diagnostic")
	}
}
