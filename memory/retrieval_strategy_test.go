package memory

import (
	"context"
	"fmt"
	"testing"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

func setupTestStore(t *testing.T) *MarkdownMemoryStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("NewMarkdownMemoryStore: %v", err)
	}
	return store
}

func populateMessages(t *testing.T, store *MarkdownMemoryStore, msgs []coreagent.SessionMessage) {
	t.Helper()
	ctx := context.Background()
	for i := range msgs {
		if msgs[i].Timestamp == "" {
			msgs[i].Timestamp = fmt.Sprintf("2026-01-01T00:%02d:00Z", i)
		}
		if err := store.AppendMessage(ctx, msgs[i]); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
}

func TestKeywordStrategy_ExcludeRecent(t *testing.T) {
	store := setupTestStore(t)
	populateMessages(t, store, []coreagent.SessionMessage{
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "old message about weather"},
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "assistant", Content: "old weather reply"},
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "recent message about weather"},
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "assistant", Content: "recent weather reply"},
	})

	strategy := NewKeywordStrategy(store)
	scope := MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1", ExcludeRecentMessages: 2}

	results, err := strategy.Search(context.Background(), scope, "weather", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	for _, r := range results {
		t.Logf("result: %s", r.Content)
		if r.Content == "recent message about weather" || r.Content == "recent weather reply" {
			t.Errorf("should not return recent messages (in history window), got: %s", r.Content)
		}
	}
	// Should only return old messages
	if len(results) > 0 {
		found := false
		for _, r := range results {
			if r.Content == "old message about weather" || r.Content == "old weather reply" {
				found = true
			}
		}
		if !found {
			t.Error("should return old messages matching query")
		}
	}
}

func TestKeywordStrategy_NoExclude(t *testing.T) {
	store := setupTestStore(t)
	populateMessages(t, store, []coreagent.SessionMessage{
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "message about testing"},
	})

	strategy := NewKeywordStrategy(store)
	scope := MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"}

	results, err := strategy.Search(context.Background(), scope, "testing", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("should return 1 result without exclude, got %d", len(results))
	}
}

func TestKeywordStrategy_ExcludeAll(t *testing.T) {
	store := setupTestStore(t)
	populateMessages(t, store, []coreagent.SessionMessage{
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "only message"},
	})

	strategy := NewKeywordStrategy(store)
	// Exclude all messages
	scope := MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1", ExcludeRecentMessages: 10}

	results, err := strategy.Search(context.Background(), scope, "message", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("should return 0 when all messages excluded, got %d", len(results))
	}
}

func TestKeywordStrategy_EmptyQuery(t *testing.T) {
	store := setupTestStore(t)
	strategy := NewKeywordStrategy(store)
	scope := MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"}

	results, err := strategy.Search(context.Background(), scope, "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if results != nil {
		t.Errorf("empty query should return nil, got %d results", len(results))
	}
}
