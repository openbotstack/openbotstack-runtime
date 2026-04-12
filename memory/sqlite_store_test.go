package memory

import (
	"context"
	"errors"
	"testing"
	"time"

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
	db.Exec(`INSERT INTO session_entries (id, session_id, content, tags, created_at, ttl)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"expired", "s1", "old data", "[]", past.UTC().Format(time.RFC3339Nano), ttlSeconds)

	_, err := store.Retrieve(ctx, "expired")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired entry, got: %v", err)
	}
}
