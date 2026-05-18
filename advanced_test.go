package bcl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConditionsNormalizeAndEvaluate(t *testing.T) {
	doc, err := Parse([]byte(`
policy "p1" {
  when {
    any {
      resource.owner_id == subject.id
      subject.roles has_any ["admin"]
    }
    not {
      subject.status == "blocked"
    }
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := Compile(doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := n.Blocks[0]["body"].(map[string]any)
	when := body["when"].(map[string]any)
	if when["op"] != "all" {
		t.Fatalf("condition = %#v", when)
	}
	block := doc.Items[0].(*Block)
	cond := block.Body[0].(*Assignment).Value.(*Condition)
	ok, err := EvalCondition(cond, map[string]any{
		"resource": map[string]any{"owner_id": "u1"},
		"subject":  map[string]any{"id": "u2", "roles": []any{"admin"}, "status": "active"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatalf("expected condition to pass: %#v", cond.ToInterface(false))
	}
}

func TestResolveImportsModulesProfilesAndOverrides(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "common.bcl"), `
const COMMON = "yes"
set "common-set" {
  one
}
`)
	modDir := filepath.Join(dir, "mods", "routing")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(modDir, "route.bcl"), `
route "admin" {
  path "/admin"
}
`)
	root := filepath.Join(dir, "main.bcl")
	mustWrite(t, root, `
import "./common.bcl" as common

module "routing" {
  source "./mods/routing"
  inputs {
    tenant "org1"
  }
}

engine {
  workers 8
  cache_ttl 5m
}

profile "prod" {
  override engine {
    workers 16
  }
}
`)
	n, err := CompileFile(root, &Options{ResolveImports: true, ResolveModules: true, Profile: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Namespaces["common"] == nil {
		t.Fatalf("missing imported namespace: %#v", n.Namespaces)
	}
	if n.Namespaces["routing"] == nil {
		t.Fatalf("missing module namespace: %#v", n.Namespaces)
	}
	engine := n.Body["engine"].(map[string]any)
	if engine["workers"] != int64(16) {
		t.Fatalf("override did not apply: %#v", engine)
	}
	lock, err := GenerateLockfile(mustParsePath(t, root), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Modules) != 2 {
		t.Fatalf("lock entries = %#v", lock.Modules)
	}
}

func TestSchemaReferenceAndCycleValidation(t *testing.T) {
	doc, err := Parse([]byte(`
schema policy {
  required effect string enum [allow, deny]
  required priority int
  optional geo tuple<float, float>
}

policy "bad" {
  effect maybe
  geo [27.7, "bad"]
}

role "a" {
  inherits {
    ref role.b
  }
}

role "b" {
  inherits {
    ref role.a
  }
}

role "c" {
  inherits {
    ref role.missing
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	if len(diags) < 4 {
		t.Fatalf("expected schema/ref/cycle diagnostics, got %#v", diags)
	}
}

func mustWrite(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustParsePath(t *testing.T, path string) *Document {
	t.Helper()
	doc, err := ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
