package bcl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatConfigIsWrittenInBCL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FormatConfigName),
		[]byte("indent 4\ntabs false\nkeep_blank_lines true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, path, err := FindFormatConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected the config to be found")
	}
	if opts.IndentWidth != 4 || opts.UseTabs || !opts.KeepBlankLines {
		t.Fatalf("options = %+v", opts)
	}

	out, err := FormatWithOptions([]byte("a {\nb 1\n}\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "a {\n    b 1\n}\n" {
		t.Fatalf("formatted with the config = %q", out)
	}
}

func TestFormatConfigIsFoundFromASubdirectory(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FormatConfigName), []byte("tabs true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, path, err := FindFormatConfig(nested)
	if err != nil {
		t.Fatal(err)
	}
	if path == "" || !opts.UseTabs {
		t.Fatalf("config from a subdirectory: path=%q opts=%+v", path, opts)
	}
}

// The search stops at a repository boundary so one project never picks up
// another's formatting rules.
func TestFormatConfigSearchStopsAtRepositoryRoot(t *testing.T) {
	outer := t.TempDir()
	if err := os.WriteFile(filepath.Join(outer, FormatConfigName), []byte("indent 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(outer, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, path, err := FindFormatConfig(repo)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("search escaped the repository: found %q", path)
	}
}

func TestMissingFormatConfigIsNotAnError(t *testing.T) {
	opts, path, err := FindFormatConfig(t.TempDir())
	if err != nil || path != "" || opts != (FormatOptions{}) {
		t.Fatalf("opts=%+v path=%q err=%v", opts, path, err)
	}
}
