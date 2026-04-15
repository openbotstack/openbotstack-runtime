package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
)

// mockVectorStore captures Store calls for verification.
type mockVectorStore struct {
	mu      sync.Mutex
	docs    []VectorDocument
	err     error
	calls   int
}

func (m *mockVectorStore) Store(_ context.Context, doc VectorDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return m.err
	}
	m.docs = append(m.docs, doc)
	return nil
}

func (m *mockVectorStore) Search(_ context.Context, _ []float32, _ SearchOptions) ([]SearchResult, error) {
	return nil, nil
}

func (m *mockVectorStore) Delete(_ context.Context, _ DeleteFilter) error { return nil }
func (m *mockVectorStore) Close() error                                   { return nil }

func (m *mockVectorStore) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockVectorStore) getDocs() []VectorDocument {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.docs
}

func TestAsyncEmbeddingIndexer_OnMessage_Disabled(t *testing.T) {
	indexer := NewAsyncEmbeddingIndexer(nil, nil)
	if indexer.enabled {
		t.Error("expected disabled when services are nil")
	}

	// Should not panic or block
	msg := agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "test",
	}
	indexer.OnMessage(context.Background(), msg)
}

func TestAsyncEmbeddingIndexer_OnMessage_Indexes(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2, 0.3}},
		},
	}
	embeddingSvc := NewEmbeddingService(router, "text-embedding-3-small", 3)
	store := &mockVectorStore{}

	indexer := NewAsyncEmbeddingIndexer(embeddingSvc, store)
	if !indexer.enabled {
		t.Fatal("expected enabled when services are non-nil")
	}

	msg := agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "hello world",
	}
	indexer.OnMessage(context.Background(), msg)

	// Wait for async goroutine to complete
	time.Sleep(200 * time.Millisecond)

	if store.getCalls() != 1 {
		t.Errorf("expected 1 Store call, got %d", store.getCalls())
	}

	docs := store.getDocs()
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", docs[0].Content)
	}
	if docs[0].TenantID != "t1" {
		t.Errorf("expected tenant t1, got %q", docs[0].TenantID)
	}
	if len(docs[0].Embedding) != 3 {
		t.Errorf("expected 3-dim embedding, got %d", len(docs[0].Embedding))
	}
}

func TestAsyncEmbeddingIndexer_EmbeddingFailure(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			err: fmt.Errorf("embedding API down"),
		},
	}
	embeddingSvc := NewEmbeddingService(router, "text-embedding-3-small", 3)
	store := &mockVectorStore{}

	indexer := NewAsyncEmbeddingIndexer(embeddingSvc, store)

	msg := agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "test",
	}
	indexer.OnMessage(context.Background(), msg)

	time.Sleep(200 * time.Millisecond)

	// Embedding failed → nothing stored
	if store.getCalls() != 0 {
		t.Errorf("expected 0 Store calls on embedding failure, got %d", store.getCalls())
	}
}

func TestAsyncEmbeddingIndexer_StoreFailure(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2}},
		},
	}
	embeddingSvc := NewEmbeddingService(router, "text-embedding-3-small", 2)
	store := &mockVectorStore{err: fmt.Errorf("PG down")}

	indexer := NewAsyncEmbeddingIndexer(embeddingSvc, store)

	msg := agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "test",
	}
	indexer.OnMessage(context.Background(), msg)

	time.Sleep(200 * time.Millisecond)

	// Store was attempted (even though it failed)
	if store.getCalls() != 1 {
		t.Errorf("expected 1 Store attempt, got %d", store.getCalls())
	}
}

func TestNewAsyncEmbeddingIndexer(t *testing.T) {
	indexer := NewAsyncEmbeddingIndexer(nil, nil)
	if indexer == nil {
		t.Fatal("expected non-nil indexer")
	}
	if indexer.enabled {
		t.Error("expected disabled with nil services")
	}
}

func TestAsyncEmbeddingIndexer_ConcurrentMessages(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2, 0.3}},
		},
	}
	embeddingSvc := NewEmbeddingService(router, "text-embedding-3-small", 3)
	store := &mockVectorStore{}

	indexer := NewAsyncEmbeddingIndexer(embeddingSvc, store)

	// Fire 50 concurrent messages
	const n = 50
	for i := 0; i < n; i++ {
		msg := agent.SessionMessage{
			TenantID:  "t1",
			UserID:    "u1",
			SessionID: "s1",
			Role:      "user",
			Content:   fmt.Sprintf("concurrent message %d", i),
		}
		indexer.OnMessage(context.Background(), msg)
	}

	// Wait for all goroutines to complete
	time.Sleep(500 * time.Millisecond)

	calls := store.getCalls()
	if calls != n {
		t.Errorf("expected %d Store calls, got %d", n, calls)
	}

	// Verify all documents are stored with correct content
	docs := store.getDocs()
	if len(docs) != n {
		t.Errorf("expected %d docs, got %d", n, len(docs))
	}
}
