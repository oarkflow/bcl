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

func TestValidateAllowsMultipleScopedOverridesForSameTarget(t *testing.T) {
	doc, err := Parse([]byte(`
engine {
  workers 8
}

profile "dev" {
  override engine {
    workers 2
  }
}

profile "prod" {
  override engine {
    workers 16
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	for _, d := range diags {
		if strings.Contains(d.Message, "duplicate block override.engine") {
			t.Fatalf("scoped overrides should not be duplicate blocks: %#v", diags)
		}
	}
}

func TestCapitalizedBareBlocksCompileAsExtensibleCommands(t *testing.T) {
	src := []byte(`
Migration "1748976351_create_seo_metadatas_table" {
  Version = "1.0.0"
  Description = "Create table seo_metadatas."

  Up {
    CreateTable "seo_metadatas" {
      Column "id" {
        type = "integer"
        primary_key = true
      }
    }
  }

  Down {
    DropTable "seo_metadatas" {
      Cascade = true
    }
  }
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	migration := findBlock(n.Blocks, "Migration", "1748976351_create_seo_metadatas_table")
	if migration == nil {
		t.Fatalf("missing Migration block: %#v", n.Blocks)
	}
	body := migration["body"].(map[string]any)
	if body["Version"] != "1.0.0" || body["Description"] != "Create table seo_metadatas." {
		t.Fatalf("migration assignments = %#v", body)
	}
	up := firstNestedBlock(body, "Up")
	if up == nil {
		t.Fatalf("missing Up command block: %#v", body)
	}
	create := firstNestedBlock(up["body"].(map[string]any), "CreateTable")
	if create == nil || create["id"] != "seo_metadatas" {
		t.Fatalf("missing CreateTable command block: %#v", up)
	}
	column := firstNestedBlock(create["body"].(map[string]any), "Column")
	if column == nil || column["id"] != "id" {
		t.Fatalf("missing Column command block: %#v", create)
	}
	down := firstNestedBlock(body, "Down")
	if down == nil {
		t.Fatalf("missing Down command block: %#v", body)
	}
	drop := firstNestedBlock(down["body"].(map[string]any), "DropTable")
	if drop == nil || drop["id"] != "seo_metadatas" {
		t.Fatalf("missing DropTable command block: %#v", down)
	}
}

func TestLowercaseBraceAssignmentsRemainObjects(t *testing.T) {
	src := []byte(`
engine {
  workers 8
}

tenant "org1" {
  attrs {
    region "NP"
  }
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, ok := n.Body["engine"].(map[string]any)
	if !ok || engine["workers"] != int64(8) {
		t.Fatalf("engine should remain a body object: %#v", n.Body["engine"])
	}
	tenant := findBlock(n.Blocks, "tenant", "org1")
	if tenant == nil {
		t.Fatalf("missing tenant block: %#v", n.Blocks)
	}
	body := tenant["body"].(map[string]any)
	attrs, ok := body["attrs"].(map[string]any)
	if !ok || attrs["region"] != "NP" {
		t.Fatalf("attrs should remain an object field: %#v", body["attrs"])
	}
	if firstNestedBlock(body, "attrs") != nil {
		t.Fatalf("attrs unexpectedly normalized as nested block: %#v", body)
	}
}

func TestBlockSpreadCanReuseSameTypeBlockWithOverrides(t *testing.T) {
	src := []byte(`
database "db" {
  host "localhost"
  port 5432
  username "app"
  password "secret"
  pool {
    max 10
    idle 2
  }
}

database "db-1" {
  &db {
    username "readonly"
    password ""
    pool.max 4
  }
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	db := findBlock(n.Blocks, "database", "db-1")
	if db == nil {
		t.Fatalf("missing database db-1: %#v", n.Blocks)
	}
	body := db["body"].(map[string]any)
	if body["host"] != "localhost" || body["port"] != int64(5432) {
		t.Fatalf("spread did not copy base fields: %#v", body)
	}
	if body["username"] != "readonly" || body["password"] != "" {
		t.Fatalf("spread overrides not applied: %#v", body)
	}
	pool, _ := body["pool"].(map[string]any)
	if pool["max"] != int64(4) || pool["idle"] != int64(2) {
		t.Fatalf("nested spread merge failed: %#v", pool)
	}
}

func TestExtensibleSamplesWithMixedAssignmentSeparators(t *testing.T) {
	src := []byte(`
network "RealWorldEnterprise" {
  device Edge_Router {
    type = "Router"
    interfaces = {
      eth0 = { ip = "203.0.113.1", protocol = "BGP", extra = { connection = "WAN", bandwidth = "1Gbps" } }
    }
  }
}

sources "prod-db" {
  type: "mysql"
  key: "prod-db"
  port: 3306
}

tables "users" {
  old_name: "tbl_user"
  migrate: true
  mapping = {
    user_id = "user_uid"
    email = "user_email_address"
  }
}
`)
	n, err := CompileBytes(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	network := findBlock(n.Blocks, "network", "RealWorldEnterprise")
	if network == nil {
		t.Fatalf("missing network block: %#v", n.Blocks)
	}
	device := firstNestedBlock(network["body"].(map[string]any), "device")
	if device == nil || device["id"] != "Edge_Router" {
		t.Fatalf("missing device block: %#v", network)
	}
	sources := findBlock(n.Blocks, "sources", "prod-db")
	if sources == nil {
		t.Fatalf("missing sources block: %#v", n.Blocks)
	}
	sourceBody := sources["body"].(map[string]any)
	if sourceBody["type"] != "mysql" || sourceBody["port"] != int64(3306) {
		t.Fatalf("source body = %#v", sourceBody)
	}
	tables := findBlock(n.Blocks, "tables", "users")
	if tables == nil {
		t.Fatalf("missing tables block: %#v", n.Blocks)
	}
	tableBody := tables["body"].(map[string]any)
	if tableBody["old_name"] != "tbl_user" || tableBody["migrate"] != true {
		t.Fatalf("table body = %#v", tableBody)
	}
	mapping := tableBody["mapping"].(map[string]any)
	if mapping["user_id"] != "user_uid" || mapping["email"] != "user_email_address" {
		t.Fatalf("mapping = %#v", mapping)
	}
}

func findBlock(blocks []map[string]any, typ, id string) map[string]any {
	for _, b := range blocks {
		if b["type"] == typ && b["id"] == id {
			return b
		}
	}
	return nil
}

func firstNestedBlock(body map[string]any, typ string) map[string]any {
	blocks, ok := body[typ].([]map[string]any)
	if ok && len(blocks) > 0 {
		return blocks[0]
	}
	items, ok := body[typ].([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	block, _ := items[0].(map[string]any)
	return block
}

func TestSchemaBackedNestedBlocksAreScopedForDuplicateValidation(t *testing.T) {
	doc, err := Parse([]byte(`
schema Migration {
}

schema CreateTable {
}

schema Column {
  optional type string
}

Migration "a" {
  CreateTable "users" {
    Column "id" {
      type integer
    }
  }
}

Migration "b" {
  CreateTable "events" {
    Column "id" {
      type integer
    }
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Validate(doc, &Options{Strict: true}) {
		if strings.Contains(d.Message, "duplicate block Column.id") {
			t.Fatalf("schema-backed nested command blocks should be scoped: %#v", d)
		}
	}
}

func TestDeepDomainPathUnderExistingBlockIsNotUnknownReference(t *testing.T) {
	doc, err := Parse([]byte(`
schema network {
}

schema device {
  optional interfaces object
}

schema connection {
  optional from string
  optional to string
}

network "n" {
  device Edge_Router {
    interfaces {
      eth1 {
        ip "10.0.0.1"
      }
    }
  }

  connection Edge_to_Switch {
    from device.Edge_Router.interfaces.eth1
    to device.Edge_Router.interfaces.eth1
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Validate(doc, &Options{Strict: true}) {
		if strings.Contains(d.Message, `unknown reference "device.Edge_Router.interfaces.eth1"`) {
			t.Fatalf("deep domain path under existing block should be allowed: %#v", d)
		}
	}
}

func TestResolveDocumentImportsSchemaFilesForValidation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "commands.schema"), `
schema Migration {
}

schema CreateTable {
}

schema Column {
  optional type string
}
`)
	mainPath := filepath.Join(dir, "main.bcl")
	mustWrite(t, mainPath, `
import "./commands.schema"

Migration "a" {
  CreateTable "users" {
    Column "id" {
      type integer
    }
  }
}

Migration "b" {
  CreateTable "events" {
    Column "id" {
      type integer
    }
  }
}
`)
	doc := mustParsePath(t, mainPath)
	opts := &Options{Strict: true, ResolveImports: true, BaseDir: dir}
	resolved, resolveDiags := ResolveDocument(doc, opts)
	if len(resolveDiags) != 0 {
		t.Fatalf("resolve diagnostics: %#v", resolveDiags)
	}
	for _, d := range Validate(resolved, opts) {
		if strings.Contains(d.Message, "duplicate block Column.id") {
			t.Fatalf("imported schema should scope nested commands: %#v", d)
		}
	}
}
