package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oarkflow/bcl/condition/pkg/audit"
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

func TestStoresPersistChainEventsAndState(t *testing.T) {
	tests := []struct {
		name  string
		store Store
		close func()
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "file", store: mustFileStore(t)},
		{name: "sqlite", store: mustSQLiteStore(t), close: func() {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.close != nil {
				defer tt.close()
			}
			ctx := ContextWithTenant(context.Background(), "tenant-a")
			now := time.Now().UTC()
			expires := now.Add(time.Hour)
			event := ChainEventRecord{ID: "evt-1", Chain: "auth", Watch: "failed", EntityKey: "user-1", EventType: "failed_login", Metadata: map[string]any{"category": "abuse"}, CreatedAt: now, ExpiresAt: &expires}
			if err := tt.store.AppendChainEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			events, err := tt.store.QueryChainEvents(ctx, ChainEventQuery{Chain: "auth", Watch: "failed", EntityKey: "user-1", EventType: "failed_login"})
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].TenantID != "tenant-a" || events[0].Metadata["category"] != "abuse" {
				t.Fatalf("events = %#v", events)
			}
			state := ChainStateRecord{Chain: "auth", Watch: "failed", EntityKey: "user-1", Counters: map[string]int{"failed_login": 3}, Step: "rate_limit", Action: "rate_limit", Metadata: map[string]any{"category": "abuse"}, UpdatedAt: now, ExpiresAt: &expires}
			if err := tt.store.UpsertChainState(ctx, state); err != nil {
				t.Fatal(err)
			}
			got, err := tt.store.GetChainState(ctx, "auth", "failed", "user-1")
			if err != nil {
				t.Fatal(err)
			}
			if got.Counters["failed_login"] != 3 || got.Action != "rate_limit" || got.Metadata["category"] != "abuse" {
				t.Fatalf("state = %#v", got)
			}
			states, err := tt.store.ListChainStates(ctx, ChainStateQuery{Chain: "auth", EntityKey: "user-1"})
			if err != nil {
				t.Fatal(err)
			}
			if len(states) != 1 || states[0].Watch != "failed" || states[0].Action != "rate_limit" {
				t.Fatalf("states = %#v", states)
			}
			if err := tt.store.DeleteExpiredChainStates(ctx, now.Add(2*time.Hour)); err != nil {
				t.Fatal(err)
			}
			if _, err := tt.store.GetChainState(ctx, "auth", "failed", "user-1"); err == nil {
				t.Fatal("expected expired state to be deleted")
			}
		})
	}
}

func TestStoresPersistActionDeliveriesAndIncidents(t *testing.T) {
	tests := []struct {
		name  string
		store Store
		close func()
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "file", store: mustFileStore(t)},
		{name: "sqlite", store: mustSQLiteStore(t), close: func() {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.close != nil {
				defer tt.close()
			}
			ctx := ContextWithTenant(context.Background(), "tenant-a")
			now := time.Now().UTC()
			delivery := ActionDeliveryRecord{ID: "delivery-1", Definition: "demo", Environment: "test", Action: "notify", Sink: "event", Status: "event_persisted", Attempts: 1, Severity: "high", CreatedAt: now, UpdatedAt: now, Metadata: map[string]any{"category": "ops"}}
			if err := tt.store.SaveActionDelivery(ctx, delivery); err != nil {
				t.Fatal(err)
			}
			deliveries, err := tt.store.ListActionDeliveries(ctx, ActionDeliveryQuery{Action: "notify"})
			if err != nil {
				t.Fatal(err)
			}
			if len(deliveries) != 1 || deliveries[0].TenantID != "tenant-a" || deliveries[0].Metadata["category"] != "ops" {
				t.Fatalf("deliveries = %#v", deliveries)
			}
			incident := IncidentRecord{ID: "incident-1", Definition: "demo", Environment: "test", GroupKey: "demo|notify", Status: "open", Action: "notify", Severity: "high", Count: 2, FirstSeen: now, LastSeen: now, Metadata: map[string]any{"route": "admin"}}
			if err := tt.store.UpsertIncident(ctx, incident); err != nil {
				t.Fatal(err)
			}
			incidents, err := tt.store.ListIncidents(ctx, IncidentQuery{Action: "notify", Status: "open"})
			if err != nil {
				t.Fatal(err)
			}
			if len(incidents) != 1 || incidents[0].TenantID != "tenant-a" || incidents[0].Count != 2 || incidents[0].Metadata["route"] != "admin" {
				t.Fatalf("incidents = %#v", incidents)
			}
		})
	}
}

func TestStoresCompactRetention(t *testing.T) {
	tests := []struct {
		name  string
		store Store
		close func()
	}{
		{name: "memory", store: NewMemoryStore()},
		{name: "file", store: mustFileStore(t)},
		{name: "sqlite", store: mustSQLiteStore(t), close: func() {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.close != nil {
				defer tt.close()
			}
			ctx := ContextWithTenant(context.Background(), "tenant-a")
			now := time.Now().UTC()
			old := now.Add(-2 * time.Hour)
			before := now.Add(-time.Hour)
			if err := tt.store.AppendChainEvent(ctx, ChainEventRecord{ID: "old-event", Definition: "demo", Environment: "prod", Chain: "auth", EntityKey: "u-1", EventType: "failed_login", CreatedAt: old}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.AppendChainEvent(ctx, ChainEventRecord{ID: "new-event", Definition: "demo", Environment: "prod", Chain: "auth", EntityKey: "u-1", EventType: "failed_login", CreatedAt: now}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.UpsertChainState(ctx, ChainStateRecord{Definition: "demo", Environment: "prod", Chain: "auth", Watch: "failed", EntityKey: "u-1", UpdatedAt: old, ExpiresAt: &old}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.SaveActionDelivery(ctx, ActionDeliveryRecord{ID: "done-action", Definition: "demo", Environment: "prod", Action: "notify", Status: "event_persisted", CreatedAt: old, UpdatedAt: old}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.SaveActionDelivery(ctx, ActionDeliveryRecord{ID: "retry-action", Definition: "demo", Environment: "prod", Action: "notify", Status: "retry_scheduled", CreatedAt: old, UpdatedAt: old}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.UpsertIncident(ctx, IncidentRecord{ID: "resolved-incident", Definition: "demo", Environment: "prod", GroupKey: "demo|notify", Status: "resolved", Action: "notify", Count: 1, FirstSeen: old, LastSeen: old}); err != nil {
				t.Fatal(err)
			}
			if err := tt.store.UpsertIncident(ctx, IncidentRecord{ID: "open-incident", Definition: "demo", Environment: "prod", GroupKey: "demo|escalate", Status: "open", Action: "escalate", Count: 1, FirstSeen: old, LastSeen: old}); err != nil {
				t.Fatal(err)
			}
			result, err := tt.store.Compact(ctx, RetentionRequest{Definition: "demo", Environment: "prod", Before: before})
			if err != nil {
				t.Fatal(err)
			}
			if result.ChainEvents != 1 || result.ChainStates != 1 || result.ActionDeliveries != 1 || result.Incidents != 1 {
				t.Fatalf("compaction result = %#v", result)
			}
			events, err := tt.store.QueryChainEvents(ctx, ChainEventQuery{Definition: "demo", Environment: "prod", IncludeExpired: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 1 || events[0].ID != "new-event" {
				t.Fatalf("events after compact = %#v", events)
			}
			actions, err := tt.store.ListActionDeliveries(ctx, ActionDeliveryQuery{Definition: "demo", Environment: "prod"})
			if err != nil {
				t.Fatal(err)
			}
			if len(actions) != 1 || actions[0].ID != "retry-action" {
				t.Fatalf("actions after compact = %#v", actions)
			}
			incidents, err := tt.store.ListIncidents(ctx, IncidentQuery{Definition: "demo", Environment: "prod"})
			if err != nil {
				t.Fatal(err)
			}
			if len(incidents) != 1 || incidents[0].ID != "open-incident" {
				t.Fatalf("incidents after compact = %#v", incidents)
			}
			stats, err := tt.store.Stats(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stats.ChainEvents != 1 || stats.ActionDeliveries != 1 || stats.Incidents != 1 {
				t.Fatalf("stats = %#v", stats)
			}
		})
	}
}

func mustFileStore(t *testing.T) Store {
	t.Helper()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustSQLiteStore(t *testing.T) Store {
	t.Helper()
	store, err := NewSQLiteStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
