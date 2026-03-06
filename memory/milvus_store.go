package memory

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrDocNotFound is returned when a document doesn't exist.
	ErrDocNotFound = errors.New("memory: document not found")
)

// Document represents a vector document.
type Document struct {
	ID        string
	Content   string
	Embedding []float32
	Metadata  map[string]string
	Score     float32 // For search results
}

// MilvusStore implements vector storage with Milvus (in-memory stub).
type MilvusStore struct {
	mu          sync.RWMutex
	collections map[string]map[string]Document
}

// NewMilvusStore creates a new Milvus store.
func NewMilvusStore() *MilvusStore {
	return &MilvusStore{
		collections: make(map[string]map[string]Document),
	}
}

// CreateCollection creates a new collection.
func (m *MilvusStore) CreateCollection(ctx context.Context, name string, dimension int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.collections[name]; !exists {
		m.collections[name] = make(map[string]Document)
	}
	return nil
}

// Upsert inserts or updates a document.
func (m *MilvusStore) Upsert(ctx context.Context, collection string, doc Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.collections[collection]; !exists {
		m.collections[collection] = make(map[string]Document)
	}

	m.collections[collection][doc.ID] = doc
	return nil
}

// Search performs vector similarity search.
func (m *MilvusStore) Search(ctx context.Context, collection string, query []float32, topK int) ([]Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	coll, exists := m.collections[collection]
	if !exists {
		return []Document{}, nil
	}

	// TODO: Actual vector similarity - stub returns all up to topK
	results := make([]Document, 0, topK)
	count := 0
	for _, doc := range coll {
		if count >= topK {
			break
		}
		doc.Score = 0.9 // Mock score
		results = append(results, doc)
		count++
	}

	return results, nil
}

// Get retrieves a document by ID.
func (m *MilvusStore) Get(ctx context.Context, collection, id string) (*Document, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	coll, exists := m.collections[collection]
	if !exists {
		return nil, ErrDocNotFound
	}

	doc, exists := coll[id]
	if !exists {
		return nil, ErrDocNotFound
	}

	return &doc, nil
}

// Delete removes a document by ID.
func (m *MilvusStore) Delete(ctx context.Context, collection, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	coll, exists := m.collections[collection]
	if !exists {
		return nil
	}

	delete(coll, id)
	return nil
}
