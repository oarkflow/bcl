package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oarkflow/condition/pkg/audit"
	_ "modernc.org/sqlite"
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

func TestMemoryStoreTenantIsolation(t *testing.T) {
	store := NewMemoryStore()
	ctxA := ContextWithTenant(context.Background(), "tenant-a")
	ctxB := ContextWithTenant(context.Background(), "tenant-b")
	record := DefinitionRecord{Name: "demo", Version: "1", Digest: "abc", PublishedAt: time.Now()}
	if err := store.SaveDefinition(ctxA, record); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDefinition(ctxB, "demo"); err == nil {
		t.Fatal("expected tenant-b lookup to fail")
	}
	if got, err := store.GetDefinition(ctxA, "demo"); err != nil || got.TenantID != "tenant-a" {
		t.Fatalf("tenant-a lookup = %#v, %v", got, err)
	}
}

func TestMemoryStoreConcurrentTenantAccess(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			tenant := fmt.Sprintf("tenant-%d", i%4)
			ctx := ContextWithTenant(context.Background(), tenant)
			record := DefinitionRecord{Name: fmt.Sprintf("demo-%d", i), Version: "1", Digest: "abc", PublishedAt: time.Now()}
			if err := store.SaveDefinition(ctx, record); err != nil {
				t.Errorf("save: %v", err)
				return
			}
			if _, err := store.GetDefinition(ctx, record.Name); err != nil {
				t.Errorf("get: %v", err)
			}
		}()
	}
	wg.Wait()
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

func TestSQLiteStoreTenantIsolation(t *testing.T) {
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctxA := ContextWithTenant(context.Background(), "tenant-a")
	ctxB := ContextWithTenant(context.Background(), "tenant-b")
	record := DefinitionRecord{Name: "demo", Version: "1", Digest: "abc", PublishedAt: time.Now()}
	if err := store.SaveDefinition(ctxA, record); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetDefinition(ctxB, "demo"); err == nil {
		t.Fatal("expected tenant-b lookup to fail")
	}
	if got, err := store.GetDefinition(ctxA, "demo"); err != nil || got.TenantID != "tenant-a" {
		t.Fatalf("tenant-a lookup = %#v, %v", got, err)
	}
}

func TestSQLiteStoreMigratesLegacyPrimaryKeysForTenants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE condition_definition_versions (
name TEXT NOT NULL,
version TEXT NOT NULL,
environment TEXT NOT NULL,
source TEXT,
source_path TEXT,
digest TEXT NOT NULL,
program_json TEXT NOT NULL,
published_at TEXT NOT NULL,
metadata_json TEXT,
tenant_id TEXT NOT NULL DEFAULT 'default',
PRIMARY KEY(name, version, environment)
);
CREATE TABLE condition_active_definitions (
name TEXT NOT NULL,
environment TEXT NOT NULL,
version TEXT NOT NULL,
activated_at TEXT NOT NULL,
tenant_id TEXT NOT NULL DEFAULT 'default',
PRIMARY KEY(name, environment)
)`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctxA := ContextWithTenant(context.Background(), "tenant-a")
	ctxB := ContextWithTenant(context.Background(), "tenant-b")
	recordA := DefinitionRecord{Name: "demo", Version: "1", Digest: "a", PublishedAt: time.Now()}
	recordB := DefinitionRecord{Name: "demo", Version: "1", Digest: "b", PublishedAt: time.Now()}
	if err := store.SaveDefinition(ctxA, recordA); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctxB, recordB); err != nil {
		t.Fatal(err)
	}
	gotA, err := store.GetDefinition(ctxA, "demo")
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := store.GetDefinition(ctxB, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if gotA.Digest != "a" || gotB.Digest != "b" {
		t.Fatalf("digests = %q/%q", gotA.Digest, gotB.Digest)
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
