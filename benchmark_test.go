package bcl

import (
	"io"
	"os"
	"testing"
)

var benchSmall = []byte(`
name "ProcessGate"
enabled true
workers 8
cache_ttl 5m

server {
  host "0.0.0.0"
  port 8080
}
`)

var benchPolicy = []byte(`
bcl {
  version "1.0"
  strict true
}

type Action = string

schema policy {
  required tenant string
  required effect string enum [allow, deny]
  required priority int
  optional actions list<string>
  optional resources list<string>
  optional when expression
}

const ADMIN_ROLES = ["admin", "superadmin"]

set "document-actions" {
  read
  write
  delete
}

policy "document-owner-or-admin" {
  tenant "org1"
  effect allow
  priority 100

  actions {
    read
    use set("document-actions")
  }

  resources {
    document:*
  }

  when {
    any {
      resource.owner_id == subject.id
      subject.roles has_any ADMIN_ROLES
    }

    not {
      subject.status == "blocked"
    }
  }

  meta {
    owner "security-team"
    description "Allow owners or admins to access documents"
    tags ["document", "rbac", "abac"]
  }
}

engine {
  cache_ttl env.duration("CACHE_TTL", 5m)
  workers env.int("WORKERS", 8)
  strict_mode true
}
`)

var benchEvalVars = map[string]any{
	"subject": map[string]any{
		"id":     "u1",
		"roles":  []any{"member", "admin"},
		"status": "active",
	},
	"resource": map[string]any{
		"owner_id": "u2",
	},
	"ADMIN_ROLES": []any{"admin", "superadmin"},
}

func BenchmarkParseSmall(b *testing.B) {
	for b.Loop() {
		doc, err := Parse(benchSmall)
		if err != nil {
			b.Fatal(err)
		}
		_ = doc
	}
}

func BenchmarkParsePolicy(b *testing.B) {
	for b.Loop() {
		doc, err := Parse(benchPolicy)
		if err != nil {
			b.Fatal(err)
		}
		_ = doc
	}
}

func BenchmarkScanSmall(b *testing.B) {
	for b.Loop() {
		if err := Scan(benchSmall); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanPolicy(b *testing.B) {
	for b.Loop() {
		if err := Scan(benchPolicy); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompilePolicy(b *testing.B) {
	opts := &Options{
		AllowEnv: true,
		Env: func(key string) (string, bool) {
			if key == "WORKERS" {
				return "16", true
			}
			return "", false
		},
	}
	for b.Loop() {
		n, err := CompileBytes(benchPolicy, opts)
		if err != nil {
			b.Fatal(err)
		}
		_ = n
	}
}

func BenchmarkCompilePolicyParsed(b *testing.B) {
	doc, err := Parse(benchPolicy)
	if err != nil {
		b.Fatal(err)
	}
	opts := &Options{
		AllowEnv: true,
		Env: func(key string) (string, bool) {
			if key == "WORKERS" {
				return "16", true
			}
			return "", false
		},
	}
	for b.Loop() {
		n, err := Compile(doc, opts)
		if err != nil {
			b.Fatal(err)
		}
		_ = n
	}
}

func BenchmarkValidatePolicy(b *testing.B) {
	doc, err := Parse(benchPolicy)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		diags := Validate(doc, &Options{Strict: true})
		_ = diags
	}
}

func BenchmarkFormatPolicy(b *testing.B) {
	for b.Loop() {
		out, err := Format(benchPolicy)
		if err != nil {
			b.Fatal(err)
		}
		_ = out
	}
}

func BenchmarkEvalExpression(b *testing.B) {
	const expr = `subject.roles has_any ["admin", "superadmin"]`
	for b.Loop() {
		v, err := Eval(expr, benchEvalVars)
		if err != nil {
			b.Fatal(err)
		}
		_ = v
	}
}

func BenchmarkEvalCondition(b *testing.B) {
	doc, err := Parse(benchPolicy)
	if err != nil {
		b.Fatal(err)
	}
	var cond *Condition
	for _, item := range doc.Items {
		block, ok := item.(*Block)
		if !ok || block.Type != "policy" {
			continue
		}
		for _, bodyItem := range block.Body {
			assign, ok := bodyItem.(*Assignment)
			if !ok || assign.Name != "when" {
				continue
			}
			cond, _ = assign.Value.(*Condition)
		}
	}
	if cond == nil {
		b.Fatal("missing benchmark condition")
	}
	for b.Loop() {
		ok, err := EvalCondition(cond, benchEvalVars, nil)
		if err != nil {
			b.Fatal(err)
		}
		_ = ok
	}
}

func BenchmarkMarshalStruct(b *testing.B) {
	type Config struct {
		Name    string `bcl:"name"`
		Enabled bool   `bcl:"enabled"`
		Workers int    `bcl:"workers"`
		Secret  string `bcl:"secret,sensitive"`
	}
	cfg := Config{Name: "ProcessGate", Enabled: true, Workers: 8, Secret: "secret"}
	for b.Loop() {
		out, err := Marshal(cfg)
		if err != nil {
			b.Fatal(err)
		}
		_ = out
	}
}

func BenchmarkUnmarshalStruct(b *testing.B) {
	type Config struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Workers int    `json:"workers"`
	}
	data := []byte(`name "ProcessGate"
enabled true
workers 8
`)
	for b.Loop() {
		var cfg Config
		if err := Unmarshal(data, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCompileExampleMain(b *testing.B) {
	env := func(key string) (string, bool) {
		values := map[string]string{
			"DATABASE_URL":       "postgres://example/processgate",
			"TLS_KEY":            "/tmp/key",
			"DB_NAME":            "processgate",
			"DB_USER":            "processgate",
			"IDENTITY_API_TOKEN": "example-token",
			"APP_ENV":            "prod",
		}
		v, ok := values[key]
		return v, ok
	}
	for b.Loop() {
		n, err := CompileFile("example/main.bcl", &Options{
			AllowEnv:       true,
			ResolveImports: true,
			ResolveModules: true,
			Profile:        "prod",
			Env:            env,
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = n
	}
}

func BenchmarkCompileDetailedExampleMain(b *testing.B) {
	env := func(key string) (string, bool) {
		values := map[string]string{
			"DATABASE_URL":       "postgres://example/processgate",
			"TLS_KEY":            "/tmp/key",
			"DB_NAME":            "processgate",
			"DB_USER":            "processgate",
			"IDENTITY_API_TOKEN": "example-token",
			"APP_ENV":            "prod",
		}
		v, ok := values[key]
		return v, ok
	}
	for b.Loop() {
		doc, err := ParsePath("example/main.bcl")
		if err != nil {
			b.Fatal(err)
		}
		result, err := CompileDetailed(doc, &Options{
			AllowEnv:       true,
			ResolveImports: true,
			ResolveModules: true,
			Profile:        "prod",
			Env:            env,
			BaseDir:        "example",
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkWriteNormalizedJSON(b *testing.B) {
	n, err := CompileBytes(benchPolicy, &Options{AllowEnv: true, Env: func(string) (string, bool) { return "", false }})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		out, err := n.JSON(false)
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Discard.Write(out)
	}
}

func BenchmarkReadExampleFile(b *testing.B) {
	for b.Loop() {
		data, err := os.ReadFile("example/main.bcl")
		if err != nil {
			b.Fatal(err)
		}
		_ = data
	}
}
