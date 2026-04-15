package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
)

const embeddingTimeout = 30 * time.Second

// AsyncEmbeddingIndexer generates embeddings for new messages and stores
// them in the vector database asynchronously (fire-and-forget).
// If vectorStore or embeddingSvc is nil, indexing is silently skipped.
type AsyncEmbeddingIndexer struct {
	embeddingSvc *EmbeddingService
	vectorStore  VectorStore
	enabled      bool
}

// NewAsyncEmbeddingIndexer creates a new async embedding indexer.
func NewAsyncEmbeddingIndexer(embeddingSvc *EmbeddingService, vectorStore VectorStore) *AsyncEmbeddingIndexer {
	return &AsyncEmbeddingIndexer{
		embeddingSvc: embeddingSvc,
		vectorStore:  vectorStore,
		enabled:      embeddingSvc != nil && vectorStore != nil,
	}
}

// OnMessage triggers async embedding generation and storage after a message is appended.
// This is fire-and-forget: errors are logged but never block the caller.
func (idx *AsyncEmbeddingIndexer) OnMessage(ctx context.Context, msg agent.SessionMessage) {
	if !idx.enabled {
		return
	}

	embedCtx, cancel := context.WithTimeout(context.Background(), embeddingTimeout)
	go func() {
		defer cancel()
		idx.indexMessage(embedCtx, msg)
	}()
}

// indexMessage generates an embedding and stores it in the vector database.
func (idx *AsyncEmbeddingIndexer) indexMessage(ctx context.Context, msg agent.SessionMessage) {
	embedding, err := idx.embeddingSvc.Embed(ctx, msg.Content)
	if err != nil {
		slog.Warn("async indexer: embedding generation failed",
			"session_id", msg.SessionID, "error", err)
		return
	}

	doc := VectorDocument{
		ID:        fmt.Sprintf("%s:%d", msg.SessionID, time.Now().UnixNano()),
		Content:   msg.Content,
		Embedding: embedding,
		TenantID:  msg.TenantID,
		UserID:    msg.UserID,
		SessionID: msg.SessionID,
		Role:      msg.Role,
		CreatedAt: time.Now(),
	}

	if err := idx.vectorStore.Store(ctx, doc); err != nil {
		slog.Warn("async indexer: vector store failed",
			"session_id", msg.SessionID, "error", err)
	}
}
