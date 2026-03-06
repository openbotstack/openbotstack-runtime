package memory_test

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestMilvusStoreCreate(t *testing.T) {
	store := memory.NewMilvusStore()
	if store == nil {
		t.Fatal("NewMilvusStore returned nil")
	}
}

func TestMilvusStoreUpsert(t *testing.T) {
	store := memory.NewMilvusStore()
	ctx := context.Background()

	doc := memory.Document{
		ID:        "doc-1",
		Content:   "This is a test document about AI agents.",
		Embedding: make([]float32, 384), // Mock embedding
		Metadata: map[string]string{
			"source": "test",
		},
	}

	err := store.Upsert(ctx, "collection-1", doc)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
}

func TestMilvusStoreSearch(t *testing.T) {
	store := memory.NewMilvusStore()
	ctx := context.Background()

	// Insert docs
	docs := []memory.Document{
		{ID: "d1", Content: "AI agents can automate tasks", Embedding: make([]float32, 384)},
		{ID: "d2", Content: "Machine learning models", Embedding: make([]float32, 384)},
		{ID: "d3", Content: "Natural language processing", Embedding: make([]float32, 384)},
	}

	for _, d := range docs {
		_ = store.Upsert(ctx, "search-test", d)
	}

	// Search
	query := make([]float32, 384)
	results, err := store.Search(ctx, "search-test", query, 2)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) > 2 {
		t.Errorf("Expected max 2 results, got %d", len(results))
	}
}

func TestMilvusStoreGet(t *testing.T) {
	store := memory.NewMilvusStore()
	ctx := context.Background()

	doc := memory.Document{
		ID:        "get-test",
		Content:   "Test content",
		Embedding: make([]float32, 384),
	}

	_ = store.Upsert(ctx, "get-collection", doc)

	result, err := store.Get(ctx, "get-collection", "get-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if result.ID != "get-test" {
		t.Errorf("Expected ID 'get-test', got '%s'", result.ID)
	}
}

func TestMilvusStoreDelete(t *testing.T) {
	store := memory.NewMilvusStore()
	ctx := context.Background()

	doc := memory.Document{ID: "delete-test", Content: "test", Embedding: make([]float32, 384)}
	_ = store.Upsert(ctx, "del-collection", doc)

	err := store.Delete(ctx, "del-collection", "delete-test")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get(ctx, "del-collection", "delete-test")
	if err != memory.ErrDocNotFound {
		t.Errorf("Expected ErrDocNotFound after delete, got %v", err)
	}
}

func TestMilvusStoreCreateCollection(t *testing.T) {
	store := memory.NewMilvusStore()
	ctx := context.Background()

	err := store.CreateCollection(ctx, "new-collection", 384)
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
}
