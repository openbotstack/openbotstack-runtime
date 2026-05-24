package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

// RetrievalStrategy defines how memory entries are searched.
type RetrievalStrategy interface {
	// Search returns memory entries matching the query within the given scope.
	Search(ctx context.Context, scope MemoryScope, query string, limit int) ([]abstraction.MemoryEntry, error)
}

// KeywordStrategy performs keyword-based matching against session history.
type KeywordStrategy struct {
	store *MarkdownMemoryStore
}

// NewKeywordStrategy creates a keyword-only retrieval strategy.
func NewKeywordStrategy(store *MarkdownMemoryStore) *KeywordStrategy {
	return &KeywordStrategy{store: store}
}

// Search tokenizes the query and scores messages by keyword overlap.
func (s *KeywordStrategy) Search(ctx context.Context, scope MemoryScope, query string, limit int) ([]abstraction.MemoryEntry, error) {
	msgs, err := s.store.GetHistory(ctx, scope.TenantID, scope.UserID, scope.SessionID, 0)
	if err != nil {
		slog.WarnContext(ctx, "keyword strategy: failed to get history",
			"error", err)
		return nil, abstraction.ErrRetrieveFailed
	}
	if len(msgs) == 0 {
		return nil, nil
	}

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	type scored struct {
		msg   agent.Message
		score int
	}
	var candidates []scored
	for _, m := range msgs {
		s := score(tokens, m.Content)
		if s > 0 {
			candidates = append(candidates, scored{msg: m, score: s})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if limit <= 0 {
		limit = 10
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}

	results := make([]abstraction.MemoryEntry, 0, limit)
	for i := 0; i < limit; i++ {
		results = append(results, abstraction.MemoryEntry{
			ID:      fmt.Sprintf("msg_%d", i),
			Content: candidates[i].msg.Content,
			Tags:    []string{"role:" + candidates[i].msg.Role},
			Metadata: map[string]string{
				"tenant_id":  scope.TenantID,
				"user_id":    scope.UserID,
				"session_id": scope.SessionID,
			},
		})
	}
	return results, nil
}

// VectorFirstStrategy tries semantic vector search, falling back to keyword.
type VectorFirstStrategy struct {
	store        *MarkdownMemoryStore
	vectorStore  VectorStore
	embeddingSvc *EmbeddingService
}

// NewVectorFirstStrategy creates a vector-first retrieval strategy.
func NewVectorFirstStrategy(store *MarkdownMemoryStore, vs VectorStore, es *EmbeddingService) *VectorFirstStrategy {
	return &VectorFirstStrategy{store: store, vectorStore: vs, embeddingSvc: es}
}

// Search tries vector semantic search first, falling back to keyword matching.
func (s *VectorFirstStrategy) Search(ctx context.Context, scope MemoryScope, query string, limit int) ([]abstraction.MemoryEntry, error) {
	embedding, err := s.embeddingSvc.Embed(ctx, query)
	if err != nil {
		slog.WarnContext(ctx, "vector-first strategy: embed failed, falling back to keyword",
			"error", err)
		return (&KeywordStrategy{store: s.store}).Search(ctx, scope, query, limit)
	}

	results, err := s.vectorStore.Search(ctx, embedding, SearchOptions{
		TenantID: scope.TenantID,
		UserID:   scope.UserID,
		Limit:    limit,
	})
	if err != nil {
		slog.WarnContext(ctx, "vector-first strategy: vector search failed, falling back to keyword",
			"error", err)
		return (&KeywordStrategy{store: s.store}).Search(ctx, scope, query, limit)
	}

	if len(results) == 0 {
		return (&KeywordStrategy{store: s.store}).Search(ctx, scope, query, limit)
	}

	entries := make([]abstraction.MemoryEntry, 0, len(results))
	for _, r := range results {
		entries = append(entries, abstraction.MemoryEntry{
			ID:        r.ID,
			Content:   r.Content,
			Embedding: r.Embedding,
			Tags:      []string{"role:" + r.Role},
			Metadata: map[string]string{
				"tenant_id":  r.TenantID,
				"user_id":    r.UserID,
				"session_id": r.SessionID,
				"score":      fmt.Sprintf("%.4f", r.Score),
			},
		})
	}
	return entries, nil
}
