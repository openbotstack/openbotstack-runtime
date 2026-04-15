package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

func TestStoreShortTerm_AppendsMessage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	bridge := NewMarkdownMemoryBridge(store, nil)

	entry := abstraction.MemoryEntry{
		Content: "Hello from test",
		Tags:    []string{"role:user"},
		Metadata: map[string]string{
			"tenant_id":  "tenant-1",
			"user_id":    "user-1",
			"session_id": "session-1",
		},
		CreatedAt: time.Now(),
	}

	ctx := context.Background()
	if err := bridge.StoreShortTerm(ctx, entry); err != nil {
		t.Fatalf("StoreShortTerm failed: %v", err)
	}

	msgs, err := store.GetHistory(ctx, "tenant-1", "user-1", "session-1", 10)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello from test" {
		t.Errorf("expected content 'Hello from test', got %q", msgs[0].Content)
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", msgs[0].Role)
	}
}

func TestStoreShortTerm_AssistantRole(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	bridge := NewMarkdownMemoryBridge(store, nil)

	entry := abstraction.MemoryEntry{
		Content: "Assistant reply",
		Tags:    []string{"role:assistant"},
		Metadata: map[string]string{
			"tenant_id":  "t1",
			"user_id":    "u1",
			"session_id": "s1",
		},
		CreatedAt: time.Now(),
	}

	if err := bridge.StoreShortTerm(context.Background(), entry); err != nil {
		t.Fatalf("StoreShortTerm failed: %v", err)
	}

	msgs, _ := store.GetHistory(context.Background(), "t1", "u1", "s1", 10)
	if len(msgs) != 1 || msgs[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msgs[0].Role)
	}
}

func TestStoreLongTerm_StoresSummary(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	bridge := NewMarkdownMemoryBridge(store, nil)

	entry := abstraction.MemoryEntry{
		Content: "This is a long-term summary of the conversation.",
		Metadata: map[string]string{
			"tenant_id":  "t1",
			"user_id":    "u1",
			"session_id": "s1",
		},
	}

	if err := bridge.StoreLongTerm(context.Background(), entry); err != nil {
		t.Fatalf("StoreLongTerm failed: %v", err)
	}

	summary, err := store.GetSummary(context.Background(), "t1", "u1", "s1")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary != "This is a long-term summary of the conversation." {
		t.Errorf("unexpected summary: %q", summary)
	}
}

func TestRetrieveSimilar_KeywordMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()

	// Store some messages
	msgs := []struct{ role, content string }{
		{"user", "What is the weather in Tokyo?"},
		{"assistant", "The weather in Tokyo is sunny and 25°C."},
		{"user", "How about Kyoto?"},
		{"assistant", "Kyoto is cloudy with 20°C."},
	}
	for _, m := range msgs {
		store.AppendMessage(ctx, agent.SessionMessage{
			TenantID: "t1", UserID: "u1", SessionID: "s1",
			Role: m.role, Content: m.content,
			Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		})
	}

	bridge := NewMarkdownMemoryBridge(store, nil)

	// Query with scope
	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "weather Tokyo", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results, got none")
	}

	// First result should mention Tokyo
	found := false
	for _, r := range results {
		if contains(r.Content, "Tokyo") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a result mentioning Tokyo")
	}
}

func TestRetrieveSimilar_NoMatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "Hello world",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	bridge := NewMarkdownMemoryBridge(store, nil)
	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "quantum physics astronomy", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestRetrieveSimilar_NoScope(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	bridge := NewMarkdownMemoryBridge(store, nil)
	results, err := bridge.RetrieveSimilar(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected no results without scope, got %d", len(results))
	}
}

func TestRetrieveByTag_BestEffort(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "I love golang programming",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "assistant", Content: "Golang is great for systems programming",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	bridge := NewMarkdownMemoryBridge(store, nil)
	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})

	results, err := bridge.RetrieveByTag(scopeCtx, []string{"golang"}, 10)
	if err != nil {
		t.Fatalf("RetrieveByTag failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results for 'golang' tag")
	}
}

func TestForget_ReturnsNotFound(t *testing.T) {
	bridge := NewMarkdownMemoryBridge(nil, nil)
	err := bridge.Forget(context.Background(), "some-id")
	if err != abstraction.ErrMemoryNotFound {
		t.Errorf("expected ErrMemoryNotFound, got %v", err)
	}
}

func TestSummarize_WithEntries(t *testing.T) {
	bridge := NewMarkdownMemoryBridge(nil, nil)

	entries := []abstraction.MemoryEntry{
		{Content: "First point about the project."},
		{Content: "Second point about deadlines."},
	}

	result, err := bridge.Summarize(context.Background(), entries)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if result.Content == "" {
		t.Error("expected non-empty summary content")
	}
	if len(result.Tags) == 0 || result.Tags[0] != "summary" {
		t.Errorf("expected 'summary' tag, got %v", result.Tags)
	}
}

func TestSummarize_EmptyEntries(t *testing.T) {
	bridge := NewMarkdownMemoryBridge(nil, nil)
	_, err := bridge.Summarize(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty entries")
	}
	if !errors.Is(err, abstraction.ErrSummarizeFailed) {
		t.Errorf("expected ErrSummarizeFailed, got %v", err)
	}
}

func TestSummarize_WithStoreAndSummarizer(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Pre-store some messages so summarizer has content
	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "Hello from test",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	// Store a summary directly
	store.StoreSummary(ctx, "t1", "u1", "s1", "Pre-existing summary of conversation")

	// Use nil summarizer — but non-nil store triggers the GetSummary path
	// when entries are passed with a valid scope context
	bridge := NewMarkdownMemoryBridge(store, nil)

	entries := []abstraction.MemoryEntry{
		{Content: "First point"},
		{Content: "Second point"},
	}

	// Without summarizer, it falls through to concatenation
	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	result, err := bridge.Summarize(scopeCtx, entries)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if result.Content == "" {
		t.Error("expected non-empty summary content")
	}
	if result.Tags[0] != "summary" {
		t.Errorf("expected 'summary' tag, got %v", result.Tags)
	}
}

func TestSummarize_NilStoreWithSummarizer(t *testing.T) {
	// Verify no panic when summarizer is set but store is nil
	bridge := NewMarkdownMemoryBridge(nil, &ConversationSummarizer{})

	entries := []abstraction.MemoryEntry{
		{Content: "Test content"},
	}

	// Should not panic — falls through to concatenation fallback
	result, err := bridge.Summarize(context.Background(), entries)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}
	if result.Content == "" {
		t.Error("expected non-empty content from fallback")
	}
}

func TestNewMarkdownMemoryBridge(t *testing.T) {
	bridge := NewMarkdownMemoryBridge(nil, nil)
	if bridge == nil {
		t.Fatal("expected non-nil bridge")
	}
}

// --- Vector search tests ---

// mockVectorStoreForBridge captures search calls for bridge tests.
type mockVectorStoreForBridge struct {
	results   []SearchResult
	searchErr error
	storeErr  error
}

func (m *mockVectorStoreForBridge) Store(_ context.Context, _ VectorDocument) error {
	return m.storeErr
}

func (m *mockVectorStoreForBridge) Search(_ context.Context, _ []float32, _ SearchOptions) ([]SearchResult, error) {
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.results, nil
}

func (m *mockVectorStoreForBridge) Delete(_ context.Context, _ DeleteFilter) error { return nil }
func (m *mockVectorStoreForBridge) Close() error                                   { return nil }

func TestRetrieveSimilar_VectorSearchWithMock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "keyword fallback content",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	vectorStore := &mockVectorStoreForBridge{
		results: []SearchResult{
			{
				VectorDocument: VectorDocument{
					ID:        "vec-1",
					Content:   "Semantic match from vector search",
					Embedding: []float32{0.1, 0.2, 0.3},
					TenantID:  "t1",
					UserID:    "u1",
					SessionID: "s1",
					Role:      "user",
				},
				Score: 0.95,
			},
		},
	}

	// Use real EmbeddingService with mock router+provider (reused from embedding_service_test.go)
	embeddingSvc := NewEmbeddingService(
		&mockRouter{provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}},
		"text-embedding-3-small", 3,
	)

	bridge := NewMarkdownMemoryBridge(store, nil)
	bridge.SetVectorStore(vectorStore)
	bridge.SetEmbeddingService(embeddingSvc)

	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "semantic query", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results from vector search")
	}
	if results[0].Content != "Semantic match from vector search" {
		t.Errorf("expected vector search result, got %q", results[0].Content)
	}
	if results[0].ID != "vec-1" {
		t.Errorf("expected ID 'vec-1', got %q", results[0].ID)
	}
	if results[0].Metadata["score"] != "0.9500" {
		t.Errorf("expected score metadata '0.9500', got %q", results[0].Metadata["score"])
	}
}

func TestRetrieveSimilar_FallbackToKeyword(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "The weather in Tokyo is sunny today",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "assistant", Content: "Great weather for sightseeing!",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Vector store returns error → triggers keyword fallback
	vectorStore := &mockVectorStoreForBridge{
		searchErr: fmt.Errorf("PG connection lost"),
	}

	// Embedding succeeds, but Search fails → fallback
	embeddingSvc := NewEmbeddingService(
		&mockRouter{provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2, 0.3}},
		}},
		"text-embedding-3-small", 3,
	)

	bridge := NewMarkdownMemoryBridge(store, nil)
	bridge.SetVectorStore(vectorStore)
	bridge.SetEmbeddingService(embeddingSvc)

	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "weather Tokyo", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results from keyword fallback")
	}
	found := false
	for _, r := range results {
		if contains(r.Content, "Tokyo") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected keyword fallback to find Tokyo message")
	}
}

func TestRetrieveSimilar_EmbeddingFailureFallsBack(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "Test content for keyword search",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Embedding provider fails → triggers keyword fallback
	embeddingSvc := NewEmbeddingService(
		&mockRouter{provider: &mockEmbedProvider{
			err: fmt.Errorf("embedding API timeout"),
		}},
		"text-embedding-3-small", 3,
	)

	bridge := NewMarkdownMemoryBridge(store, nil)
	bridge.SetVectorStore(&mockVectorStoreForBridge{})
	bridge.SetEmbeddingService(embeddingSvc)

	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "keyword search", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected keyword fallback results when embedding fails")
	}
}

func TestRetrieveSimilar_VectorReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	store.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "keyword match content",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})

	// Vector store returns empty results → falls back to keyword
	vectorStore := &mockVectorStoreForBridge{results: nil}

	embeddingSvc := NewEmbeddingService(
		&mockRouter{provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1}},
		}},
		"text-embedding-3-small", 1,
	)

	bridge := NewMarkdownMemoryBridge(store, nil)
	bridge.SetVectorStore(vectorStore)
	bridge.SetEmbeddingService(embeddingSvc)

	scopeCtx := ScopeWithMemory(ctx, MemoryScope{TenantID: "t1", UserID: "u1", SessionID: "s1"})
	results, err := bridge.RetrieveSimilar(scopeCtx, "keyword match", 5)
	if err != nil {
		t.Fatalf("RetrieveSimilar failed: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected keyword fallback when vector returns empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
