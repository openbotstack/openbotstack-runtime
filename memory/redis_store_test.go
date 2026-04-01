package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestRedisMemoryStore(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	entry := memory.Entry{
		ID:        "entry-1",
		SessionID: "session-1",
		Content:   "Hello world",
		Tags:      []string{"greeting"},
		TTL:       time.Hour,
	}

	err := store.Store(ctx, entry)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
}

func TestRedisMemoryRetrieve(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	entry := memory.Entry{
		ID:        "entry-2",
		SessionID: "session-1",
		Content:   "Test content",
		Tags:      []string{"test"},
		TTL:       time.Hour,
	}

	_ = store.Store(ctx, entry)

	retrieved, err := store.Retrieve(ctx, "entry-2")
	if err != nil {
		t.Fatalf("Retrieve failed: %v", err)
	}

	if retrieved.Content != "Test content" {
		t.Errorf("Expected 'Test content', got '%s'", retrieved.Content)
	}
}

func TestRedisMemoryRetrieveNotFound(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	_, err := store.Retrieve(ctx, "nonexistent")
	if err != memory.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestRedisMemoryListBySession(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	entries := []memory.Entry{
		{ID: "e1", SessionID: "session-a", Content: "first"},
		{ID: "e2", SessionID: "session-a", Content: "second"},
		{ID: "e3", SessionID: "session-b", Content: "other"},
	}

	for _, e := range entries {
		_ = store.Store(ctx, e)
	}

	result, err := store.ListBySession(ctx, "session-a")
	if err != nil {
		t.Fatalf("ListBySession failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(result))
	}
}

func TestRedisMemoryDelete(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	entry := memory.Entry{
		ID:        "to-delete",
		SessionID: "session-1",
		Content:   "Temporary",
	}

	_ = store.Store(ctx, entry)
	err := store.Delete(ctx, "to-delete")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Retrieve(ctx, "to-delete")
	if err != memory.ErrNotFound {
		t.Errorf("Expected ErrNotFound after delete, got %v", err)
	}
}

func TestRedisMemoryClearSession(t *testing.T) {
	store := memory.NewInMemoryStore()
	ctx := context.Background()

	entries := []memory.Entry{
		{ID: "s1", SessionID: "clear-me", Content: "a"},
		{ID: "s2", SessionID: "clear-me", Content: "b"},
	}

	for _, e := range entries {
		_ = store.Store(ctx, e)
	}

	err := store.ClearSession(ctx, "clear-me")
	if err != nil {
		t.Fatalf("ClearSession failed: %v", err)
	}

	result, _ := store.ListBySession(ctx, "clear-me")
	if len(result) != 0 {
		t.Errorf("Expected 0 entries after clear, got %d", len(result))
	}
}
