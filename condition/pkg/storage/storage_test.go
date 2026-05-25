package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/oarkflow/condition/pkg/audit"
)

func TestFileStorePersistsDefinitionAndAudit(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	record := DefinitionRecord{Name: "demo", Version: "1", Digest: "abc", PublishedAt: time.Now()}
	if err := store.SaveDefinition(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetDefinition(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != "abc" {
		t.Fatalf("digest = %q", got.Digest)
	}
	envelope := audit.Seal(audit.Envelope{ID: "audit-1", Operation: "publish"}, "")
	if err := store.AppendAudit(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	hash, err := store.LastAuditHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hash != envelope.Hash {
		t.Fatalf("hash = %q", hash)
	}
}

func TestSQLiteStorePersistsDefinitionAndAudit(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	record := DefinitionRecord{Name: "demo", Version: "1", Digest: "abc", PublishedAt: time.Now()}
	if err := store.SaveDefinition(ctx, record); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetDefinition(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
	envelope := audit.Seal(audit.Envelope{ID: "audit-1", Operation: "publish"}, "")
	if err := store.AppendAudit(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListAudits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("audits = %d", len(records))
	}
}

func TestSQLiteStoreConfiguresOperationalPragmas(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "condition.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var busyTimeout int
	if err := store.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout <= 0 {
		t.Fatalf("busy_timeout = %d", busyTimeout)
	}
	var foreignKeys int
	if err := store.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d", foreignKeys)
	}
}
