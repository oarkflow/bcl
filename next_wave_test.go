package bcl

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNextWaveParamPredicateAndExecutableTest(t *testing.T) {
	doc, err := Parse([]byte(`
param tenant_id string {
  required true
  description "Tenant"
}

predicate "admin_mfa_request" {
  all {
    context.request.path starts_with "/admin"
    session.attrs.mfa == true
  }
}

rule "context-session-check" {
  effect allow
  when {
    predicate.admin_mfa_request
  }

  then {
    emit "context.session.allowed"
  }
}

test "admin mfa can access admin path" {
  input {
    context.request.path "/admin/settings"
    session.attrs.mfa true
  }

  expect {
    emit "context.session.allowed"
    diagnostics none
    effect allow
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
	if n.Params["tenant_id"] == nil || n.Predicates["admin_mfa_request"] == nil || len(n.Tests) != 1 {
		t.Fatalf("missing next-wave metadata: %#v", n)
	}
	result := runCompiledTest(n, n.Tests[0], nil)
	if !result.Passed {
		t.Fatalf("test failed: %#v", result.Diagnostics)
	}
}

func TestNextWavePredicateValidation(t *testing.T) {
	doc, err := Parse([]byte(`
predicate "a" {
  predicate.b
}

predicate "b" {
  predicate.a
}

rule "r" {
  when {
    predicate.missing
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, nil)
	text := FormatDiagnostics(diags)
	if !strings.Contains(text, "unknown predicate") || !strings.Contains(text, "cyclic predicate") {
		t.Fatalf("missing predicate diagnostics:\n%s", text)
	}
}

func TestNextWaveModuleParamValidation(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "mod")
	if err := os.Mkdir(modDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "main.bcl"), []byte(`param tenant_id string { required true }`), 0644); err != nil {
		t.Fatal(err)
	}
	doc, err := ParseFile(filepath.Join(dir, "main.bcl"), []byte(`
module "m" {
  source "./mod"
  inputs {
    tenant_id 42
  }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	diags := Validate(doc, &Options{BaseDir: dir})
	if !strings.Contains(FormatDiagnostics(diags), "must be string") {
		t.Fatalf("missing module input type diagnostic: %#v", diags)
	}
}

func TestNextWaveFormatPreservesCommentsAndTrivia(t *testing.T) {
	src := []byte("# keep me\npolicy \"p\" { // same line\n  effect allow\n}\n")
	out, err := Format(src)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, out) {
		t.Fatalf("comments were not preserved:\n%s", out)
	}
	trivia, err := ParseFileWithTrivia("test.bcl", src)
	if err != nil {
		t.Fatal(err)
	}
	if len(trivia.Comments) != 2 {
		t.Fatalf("expected comments, got %#v", trivia.Comments)
	}
}

func TestNextWaveFetchAndVerifyLocalRegistryArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "mod.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("main.bcl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`policy "p" { effect allow }`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.bcl")
	if err := os.WriteFile(main, []byte(`module "m" { source "file://`+archive+`" }`), 0644); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "bcl.lock")
	if err := FetchModules(main, lock, &ModuleFetchOptions{CacheDir: filepath.Join(dir, "cache")}); err != nil {
		t.Fatal(err)
	}
	if diags := VerifyModules(lock, &ModuleVerifyOptions{CacheDir: filepath.Join(dir, "cache")}); len(diags) != 0 {
		t.Fatalf("verify failed: %#v", diags)
	}
}
