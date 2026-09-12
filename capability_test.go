package bcl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileDatasetIsConfinedToItsDocument covers a document reading files it was
// never meant to reach: the path is written by whoever wrote the document, and
// the contents come back inside decision records.
func TestFileDatasetIsConfinedToItsDocument(t *testing.T) {
	dir := t.TempDir()
	docDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(filepath.Join(docDir, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(docDir, "inputs", "batch.jsonl")
	if err := os.WriteFile(inside, []byte(`{"id":"a","input":{"request":{"amount":20}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret.jsonl")
	if err := os.WriteFile(secret, []byte(`{"id":"s","input":{"request":{"amount":20}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	program := func(path string) *DecisionProgram {
		return &DecisionProgram{Datasets: map[string]*DatasetDefinition{
			"d": {ID: "d", Source: DatasetSource{
				Adapter: "file",
				Root:    docDir,
				Config:  map[string]any{"path": path, "format": "jsonl", "facts_path": "input"},
			}},
		}}
	}

	if _, err := OpenDecisionDataset(t.Context(), program(inside), "d", nil); err != nil {
		t.Fatalf("a file beside the document should be readable: %v", err)
	}

	_, err := OpenDecisionDataset(t.Context(), program(secret), "d", nil)
	if err == nil {
		t.Fatal("expected a file outside the document's directory to be refused")
	}
	if !strings.Contains(err.Error(), "AllowedDatasetRoots") {
		t.Fatalf("the error should say how to permit it: %v", err)
	}

	escape := filepath.Join(docDir, "inputs", "..", "..", "secret.jsonl")
	if _, err := OpenDecisionDataset(t.Context(), program(escape), "d", nil); err == nil {
		t.Fatal(`expected a "../" escape to be refused`)
	}

	// The embedder can widen it deliberately, per directory or entirely.
	if _, err := OpenDecisionDataset(t.Context(), program(secret), "d", &Options{AllowedDatasetRoots: []string{dir}}); err != nil {
		t.Fatalf("an explicitly allowed root should be readable: %v", err)
	}
	if _, err := OpenDecisionDataset(t.Context(), program(secret), "d", &Options{AllowedDatasetRoots: []string{"*"}}); err != nil {
		t.Fatalf(`"*" should allow any path: %v`, err)
	}
}

// TestSymlinkOutOfDatasetRootIsRefused covers the obvious way around a path check.
func TestSymlinkOutOfDatasetRootIsRefused(t *testing.T) {
	dir := t.TempDir()
	docDir := filepath.Join(dir, "policy")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "secret.jsonl")
	if err := os.WriteFile(secret, []byte(`{"id":"s"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(docDir, "link.jsonl")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	program := &DecisionProgram{Datasets: map[string]*DatasetDefinition{
		"d": {ID: "d", Source: DatasetSource{
			Adapter: "file",
			Root:    docDir,
			Config:  map[string]any{"path": link, "format": "jsonl"},
		}},
	}}
	if _, err := OpenDecisionDataset(t.Context(), program, "d", nil); err == nil {
		t.Fatal("expected a symlink pointing outside the root to be refused")
	}
}

func TestExternalHTTPClientAlwaysHasADeadline(t *testing.T) {
	if got := externalHTTPClient(nil, 0).Timeout; got != DefaultExternalTimeout {
		t.Fatalf("default timeout = %v", got)
	}
	if got := externalHTTPClient(&Options{ExternalTimeout: 5}, 0).Timeout; got != 5 {
		t.Fatalf("explicit timeout = %v", got)
	}
	if got := externalHTTPClient(nil, 7).Timeout; got != 7 {
		t.Fatalf("document timeout = %v", got)
	}
}

// TestGitModuleSourceRejectsArgumentAndCommandTransports covers the two ways a
// lockfile entry could turn "git clone" into arbitrary execution.
func TestGitModuleSourceRejectsArgumentAndCommandTransports(t *testing.T) {
	refused := []string{
		"--upload-pack=touch /tmp/pwned", // git reads a leading dash as an option
		"-u./evil",                       // same, in short form
		"ext::sh -c 'touch /tmp/pwned'",  // the ext transport runs a command by design
		"EXT::sh -c id",                  // transports are matched case-insensitively
		"fd::7",                          // reads an inherited file descriptor
		"",                               // nothing to clone
	}
	for _, source := range refused {
		if err := validateGitSource(source, nil); err == nil {
			t.Errorf("expected %q to be refused", source)
		}
	}
	for _, source := range []string{
		"https://github.com/oarkflow/bcl.git",
		"git@github.com:oarkflow/bcl.git",
		"file:///srv/modules/bcl.git",
	} {
		if err := validateGitSource(source, nil); err != nil {
			t.Errorf("expected %q to be allowed: %v", source, err)
		}
	}
}

func TestGitRevisionMustBeARef(t *testing.T) {
	for _, rev := range []string{"--upload-pack=id", "-x", "a b", "rev;id", "$(id)"} {
		if validGitRevision(rev) {
			t.Errorf("expected %q to be rejected as a revision", rev)
		}
	}
	for _, rev := range []string{"main", "v1.2.3", "release/2026-05", "9f2a1c4", "feature_x+1"} {
		if !validGitRevision(rev) {
			t.Errorf("expected %q to be a usable revision", rev)
		}
	}
}

func TestFetchHostAllowlist(t *testing.T) {
	hosts := []string{"github.com"}
	if err := checkFetchHost("https://github.com/a/b.git", hosts); err != nil {
		t.Fatalf("allowed host refused: %v", err)
	}
	if err := checkFetchHost("git@github.com:a/b.git", hosts); err != nil {
		t.Fatalf("scp-style address on an allowed host refused: %v", err)
	}
	if err := checkFetchHost("https://evil.example/a/b.git", hosts); err == nil {
		t.Fatal("expected a host outside the allowlist to be refused")
	}
	if err := checkFetchHost("https://evil.example/a/b.git", nil); err != nil {
		t.Fatalf("an empty allowlist should allow any host: %v", err)
	}
}
