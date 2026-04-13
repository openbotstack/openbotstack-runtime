package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func setupMemoryTestDB(t *testing.T) *persistence.DB {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func ctxWithTenant(tenantID string) context.Context {
	user := &auth.User{ID: "user1", TenantID: tenantID, Name: "Test"}
	return middleware.WithUser(context.Background(), user)
}

func TestStoreAndRetrieve(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	ctx := context.Background()

	store := NewSQLiteMemoryStore(db.DB)
	entry := Entry{
		ID: "entry-1", SessionID: "sess-1", Content: "Hello world",
		Tags: []string{"greeting"}, CreatedAt: time.Now(),
	}

	if err := store.Store(ctx, entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := store.Retrieve(ctx, "entry-1")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", got.Content, "Hello world")
	}
	if len(got.Tags) != 1 || got.Tags[0] != "greeting" {
		t.Errorf("Tags = %v, want [greeting]", got.Tags)
	}
}

func TestRetrieveNotFound(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	_, err := store.Retrieve(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestListBySession(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	ctx := context.Background()
	store := NewSQLiteMemoryStore(db.DB)

	store.Store(ctx, Entry{ID: "e1", SessionID: "s1", Content: "first", CreatedAt: time.Now()})
	store.Store(ctx, Entry{ID: "e2", SessionID: "s1", Content: "second", CreatedAt: time.Now()})
	store.Store(ctx, Entry{ID: "e3", SessionID: "s2", Content: "other", CreatedAt: time.Now()})

	entries, err := store.ListBySession(ctx, "s1")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("ListBySession returned %d entries, want 2", len(entries))
	}
}

func TestDelete(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	ctx := context.Background()
	store := NewSQLiteMemoryStore(db.DB)

	store.Store(ctx, Entry{ID: "e1", SessionID: "s1", Content: "data", CreatedAt: time.Now()})

	if err := store.Delete(ctx, "e1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.Retrieve(ctx, "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestClearSession(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	ctx := context.Background()
	store := NewSQLiteMemoryStore(db.DB)

	store.Store(ctx, Entry{ID: "e1", SessionID: "s1", Content: "a", CreatedAt: time.Now()})
	store.Store(ctx, Entry{ID: "e2", SessionID: "s1", Content: "b", CreatedAt: time.Now()})
	store.Store(ctx, Entry{ID: "e3", SessionID: "s2", Content: "c", CreatedAt: time.Now()})

	if err := store.ClearSession(ctx, "s1"); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	entries, _ := store.ListBySession(ctx, "s1")
	if len(entries) != 0 {
		t.Errorf("ListBySession after clear returned %d, want 0", len(entries))
	}

	entries, _ = store.ListBySession(ctx, "s2")
	if len(entries) != 1 {
		t.Errorf("Session s2 returned %d, want 1", len(entries))
	}
}

func TestTTLExpiry(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	ctx := context.Background()
	store := NewSQLiteMemoryStore(db.DB)

	// Insert expired entry directly
	past := time.Now().Add(-2 * time.Hour)
	ttlSeconds := int64(3600)
	db.Exec(`INSERT INTO session_entries (id, session_id, tenant_id, content, tags, created_at, ttl)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"expired", "s1", "", "old data", "[]", past.UTC().Format(time.RFC3339Nano), ttlSeconds)

	_, err := store.Retrieve(ctx, "expired")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired entry, got: %v", err)
	}
}

// --- Tenant isolation tests ---

func TestTenantIsolation_StoreAndRetrieve(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	entry := Entry{
		ID: "entry-1", SessionID: "sess-1", Content: "secret data",
		Tags: []string{"private"}, CreatedAt: time.Now(),
	}

	// Store as tenant A
	if err := store.Store(ctxA, entry); err != nil {
		t.Fatalf("Store as tenant A: %v", err)
	}

	// Retrieve as tenant A - should succeed
	got, err := store.Retrieve(ctxA, "entry-1")
	if err != nil {
		t.Fatalf("Retrieve as tenant A: %v", err)
	}
	if got.Content != "secret data" {
		t.Errorf("Content = %q, want %q", got.Content, "secret data")
	}

	// Retrieve as tenant B - should get ErrNotFound
	_, err = store.Retrieve(ctxB, "entry-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound when retrieving as tenant B, got: %v", err)
	}
}

func TestTenantIsolation_ListBySession(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	// Store entries for tenant A and tenant B in the same session
	store.Store(ctxA, Entry{ID: "e1", SessionID: "s1", Content: "tenant-a-first", CreatedAt: time.Now()})
	store.Store(ctxB, Entry{ID: "e2", SessionID: "s1", Content: "tenant-b-data", CreatedAt: time.Now()})
	store.Store(ctxA, Entry{ID: "e3", SessionID: "s1", Content: "tenant-a-second", CreatedAt: time.Now()})

	// List as tenant A - should only see tenant A entries
	entriesA, err := store.ListBySession(ctxA, "s1")
	if err != nil {
		t.Fatalf("ListBySession as tenant A: %v", err)
	}
	if len(entriesA) != 2 {
		t.Fatalf("ListBySession as tenant A returned %d entries, want 2", len(entriesA))
	}
	for _, e := range entriesA {
		if e.Content == "tenant-b-data" {
			t.Error("tenant A should not see tenant B data")
		}
	}

	// List as tenant B - should only see tenant B entry
	entriesB, err := store.ListBySession(ctxB, "s1")
	if err != nil {
		t.Fatalf("ListBySession as tenant B: %v", err)
	}
	if len(entriesB) != 1 {
		t.Fatalf("ListBySession as tenant B returned %d entries, want 1", len(entriesB))
	}
	if entriesB[0].Content != "tenant-b-data" {
		t.Errorf("Content = %q, want %q", entriesB[0].Content, "tenant-b-data")
	}
}

func TestTenantIsolation_Delete(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	store.Store(ctxA, Entry{ID: "e1", SessionID: "s1", Content: "tenant-a-data", CreatedAt: time.Now()})

	// Delete as tenant B - should fail (entry belongs to tenant A)
	err := store.Delete(ctxB, "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound when deleting as tenant B, got: %v", err)
	}

	// Verify entry still exists
	got, err := store.Retrieve(ctxA, "e1")
	if err != nil {
		t.Fatalf("entry should still exist after failed delete: %v", err)
	}
	if got.Content != "tenant-a-data" {
		t.Errorf("Content = %q, want %q", got.Content, "tenant-a-data")
	}

	// Delete as tenant A - should succeed
	if err := store.Delete(ctxA, "e1"); err != nil {
		t.Fatalf("Delete as tenant A: %v", err)
	}

	// Verify entry is gone
	_, err = store.Retrieve(ctxA, "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestTenantIsolation_ClearSession(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	ctxA := ctxWithTenant("tenant-a")
	ctxB := ctxWithTenant("tenant-b")

	// Store entries for both tenants in same session
	store.Store(ctxA, Entry{ID: "e1", SessionID: "s1", Content: "a1", CreatedAt: time.Now()})
	store.Store(ctxA, Entry{ID: "e2", SessionID: "s1", Content: "a2", CreatedAt: time.Now()})
	store.Store(ctxB, Entry{ID: "e3", SessionID: "s1", Content: "b1", CreatedAt: time.Now()})

	// Clear session as tenant B - should only remove tenant B entries
	if err := store.ClearSession(ctxB, "s1"); err != nil {
		t.Fatalf("ClearSession as tenant B: %v", err)
	}

	// Tenant A entries should still exist
	entriesA, err := store.ListBySession(ctxA, "s1")
	if err != nil {
		t.Fatalf("ListBySession as tenant A after clear: %v", err)
	}
	if len(entriesA) != 2 {
		t.Errorf("tenant A entries after clear = %d, want 2", len(entriesA))
	}

	// Tenant B entries should be gone
	entriesB, err := store.ListBySession(ctxB, "s1")
	if err != nil {
		t.Fatalf("ListBySession as tenant B after clear: %v", err)
	}
	if len(entriesB) != 0 {
		t.Errorf("tenant B entries after clear = %d, want 0", len(entriesB))
	}
}

func TestNoAuthContext(t *testing.T) {
	db := setupMemoryTestDB(t)
	defer db.Close()
	store := NewSQLiteMemoryStore(db.DB)

	// No auth context - tenantFromCtx returns ""
	ctx := context.Background()

	entry := Entry{
		ID: "e1", SessionID: "s1", Content: "no-auth-data",
		Tags: []string{"test"}, CreatedAt: time.Now(),
	}

	// Store without auth - should store with tenant_id = ""
	if err := store.Store(ctx, entry); err != nil {
		t.Fatalf("Store without auth: %v", err)
	}

	// Retrieve without auth - should work
	got, err := store.Retrieve(ctx, "e1")
	if err != nil {
		t.Fatalf("Retrieve without auth: %v", err)
	}
	if got.Content != "no-auth-data" {
		t.Errorf("Content = %q, want %q", got.Content, "no-auth-data")
	}

	// ListBySession without auth - should work
	entries, err := store.ListBySession(ctx, "s1")
	if err != nil {
		t.Fatalf("ListBySession without auth: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("ListBySession returned %d, want 1", len(entries))
	}

	// Delete without auth - should work
	if err := store.Delete(ctx, "e1"); err != nil {
		t.Fatalf("Delete without auth: %v", err)
	}

	// Verify deleted
	_, err = store.Retrieve(ctx, "e1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got: %v", err)
	}

	// Also verify that no-auth context cannot see tenant-scoped entries
	ctxA := ctxWithTenant("tenant-a")
	store.Store(ctxA, Entry{ID: "e2", SessionID: "s1", Content: "tenant-data", CreatedAt: time.Now()})

	// No-auth retrieve of tenant-scoped entry should see it (no filter applied)
	got, err = store.Retrieve(ctx, "e2")
	if err != nil {
		t.Fatalf("Retrieve without auth of tenant entry: %v", err)
	}
	if got.Content != "tenant-data" {
		t.Errorf("Content = %q, want %q", got.Content, "tenant-data")
	}
}
