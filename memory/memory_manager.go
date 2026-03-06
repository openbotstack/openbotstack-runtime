package memory

import (
	"context"
	"fmt"
	"sync"
)

const (
	conversationCollection = "conversations"
	summaryCollection      = "summaries"
	embeddingDimension     = 384
)

// MemoryManager orchestrates the embed → store → summarize pipeline.
//
// On each message:
//   - Generate embedding for the message
//   - Store in vector store (scoped by tenant + user)
//
// On session threshold:
//   - Summarize conversation
//   - Store summary as long-term memory
type MemoryManager struct {
	embedder   Embedder
	store      *MilvusStore
	summarizer Summarizer
	mu         sync.Mutex
	history    map[string][]ChatMessage // sessionID → messages
}

// NewMemoryManager creates a new MemoryManager with the given components.
func NewMemoryManager(embedder Embedder, store *MilvusStore, summarizer Summarizer) *MemoryManager {
	return &MemoryManager{
		embedder:   embedder,
		store:      store,
		summarizer: summarizer,
		history:    make(map[string][]ChatMessage),
	}
}

// Initialize sets up required collections.
func (m *MemoryManager) Initialize(ctx context.Context) error {
	if err := m.store.CreateCollection(ctx, conversationCollection, embeddingDimension); err != nil {
		return fmt.Errorf("memory: failed to create conversation collection: %w", err)
	}
	if err := m.store.CreateCollection(ctx, summaryCollection, embeddingDimension); err != nil {
		return fmt.Errorf("memory: failed to create summary collection: %w", err)
	}
	return nil
}

// OnMessage processes a new message: embed and store, then check summarization threshold.
func (m *MemoryManager) OnMessage(ctx context.Context, sessionID, tenantID, userID, role, content string) error {
	// 1. Generate embedding
	embedding, err := m.embedder.Embed(ctx, content)
	if err != nil {
		return fmt.Errorf("memory: embedding failed: %w", err)
	}

	// 2. Store in vector store with tenant/user scope
	docID := fmt.Sprintf("%s:%s:%d", sessionID, role, len(content))
	doc := Document{
		ID:        docID,
		Content:   content,
		Embedding: embedding,
		Metadata: map[string]string{
			"session_id": sessionID,
			"tenant_id":  tenantID,
			"user_id":    userID,
			"role":       role,
		},
	}

	if err := m.store.Upsert(ctx, conversationCollection, doc); err != nil {
		return fmt.Errorf("memory: store failed: %w", err)
	}

	// 3. Track conversation history for summarization
	m.mu.Lock()
	m.history[sessionID] = append(m.history[sessionID], ChatMessage{Role: role, Content: content})
	messages := m.history[sessionID]
	m.mu.Unlock()

	// 4. Check if summarization is needed (non-blocking)
	if m.summarizer.ShouldSummarize(messages) {
		go m.summarizeAsync(context.Background(), sessionID, tenantID, userID, messages)
	}

	return nil
}

// Recall retrieves relevant memories for context enrichment.
func (m *MemoryManager) Recall(ctx context.Context, query string, topK int) ([]Document, error) {
	// Generate query embedding
	embedding, err := m.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory: query embedding failed: %w", err)
	}

	// Search conversation memories
	convDocs, err := m.store.Search(ctx, conversationCollection, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("memory: conversation search failed: %w", err)
	}

	// Search summaries
	sumDocs, err := m.store.Search(ctx, summaryCollection, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("memory: summary search failed: %w", err)
	}

	// Merge results: summaries first (higher value), then conversation turns
	results := make([]Document, 0, len(sumDocs)+len(convDocs))
	results = append(results, sumDocs...)
	results = append(results, convDocs...)

	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// summarizeAsync generates a summary and stores it.
func (m *MemoryManager) summarizeAsync(ctx context.Context, sessionID, tenantID, userID string, messages []ChatMessage) {
	summary, err := m.summarizer.Summarize(ctx, messages)
	if err != nil {
		return // Log in production, skip in V1
	}

	embedding, err := m.embedder.Embed(ctx, summary)
	if err != nil {
		return
	}

	doc := Document{
		ID:        fmt.Sprintf("summary:%s", sessionID),
		Content:   summary,
		Embedding: embedding,
		Metadata: map[string]string{
			"session_id": sessionID,
			"tenant_id":  tenantID,
			"user_id":    userID,
			"type":       "summary",
		},
	}

	_ = m.store.Upsert(ctx, summaryCollection, doc)

	// Clear history after summarization
	m.mu.Lock()
	m.history[sessionID] = nil
	m.mu.Unlock()
}
