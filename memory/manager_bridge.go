package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

// Compile-time interface compliance check.
var _ abstraction.MemoryManager = (*MarkdownMemoryBridge)(nil)

// MarkdownMemoryBridge wraps MarkdownMemoryStore to implement abstraction.MemoryManager.
// When vectorStore + embeddingSvc are configured, RetrieveSimilar uses semantic vector search
// and falls back to keyword matching on failure. Without vector config, pure keyword matching.
// This is the markdown-first bridge between core's memory abstraction and runtime's storage.
//
// Design note: RetrieveByTag falls back to content-substring matching because the
// underlying MarkdownMemoryStore does not store structured tags per message.
// Full tag-based retrieval requires the vector layer (Phase 3.1).
type MarkdownMemoryBridge struct {
	store        *MarkdownMemoryStore
	summarizer   *ConversationSummarizer
	vectorStore  VectorStore       // optional: nil = keyword matching only
	embeddingSvc *EmbeddingService // optional: nil = keyword matching only
}

// NewMarkdownMemoryBridge creates a bridge from MarkdownMemoryStore to MemoryManager.
func NewMarkdownMemoryBridge(store *MarkdownMemoryStore, summarizer *ConversationSummarizer) *MarkdownMemoryBridge {
	return &MarkdownMemoryBridge{
		store:      store,
		summarizer: summarizer,
	}
}

// SetVectorStore configures an optional vector store for semantic search.
func (b *MarkdownMemoryBridge) SetVectorStore(vs VectorStore) { b.vectorStore = vs }

// SetEmbeddingService configures an optional embedding service for vector generation.
func (b *MarkdownMemoryBridge) SetEmbeddingService(es *EmbeddingService) { b.embeddingSvc = es }

// memoryBridgeKey is the context key for MemoryScope.
type memoryBridgeKey struct{}

// MemoryScope contains tenant/user/session identifiers for memory operations.
type MemoryScope struct {
	TenantID  string
	UserID    string
	SessionID string
}

// ScopeWithMemory adds memory scope to context.
func ScopeWithMemory(ctx context.Context, scope MemoryScope) context.Context {
	return context.WithValue(ctx, memoryBridgeKey{}, scope)
}

// memoryScopeFromContext extracts memory scope from context.
func memoryScopeFromContext(ctx context.Context) MemoryScope {
	scope, _ := ctx.Value(memoryBridgeKey{}).(MemoryScope)
	return scope
}

// StoreShortTerm saves a conversation-scoped entry to the markdown store.
func (b *MarkdownMemoryBridge) StoreShortTerm(ctx context.Context, entry abstraction.MemoryEntry) error {
	if b.store == nil {
		return fmt.Errorf("memory bridge: store not configured")
	}

	tenantID := entry.Metadata["tenant_id"]
	userID := entry.Metadata["user_id"]
	sessionID := entry.Metadata["session_id"]
	if tenantID == "" || userID == "" || sessionID == "" {
		return fmt.Errorf("memory bridge: tenant_id, user_id, and session_id required in metadata")
	}

	role := "user"
	for _, tag := range entry.Tags {
		if tag == "role:assistant" {
			role = "assistant"
			break
		}
	}

	ts := entry.CreatedAt.Format(time.RFC3339Nano)
	if ts == "" || entry.CreatedAt.IsZero() {
		ts = time.Now().UTC().Format(time.RFC3339Nano)
	}

	return b.store.AppendMessage(ctx, agent.SessionMessage{
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessionID,
		Role:      role,
		Content:   entry.Content,
		Timestamp: ts,
	})
}

// StoreLongTerm saves an entry as a session summary in the markdown store.
func (b *MarkdownMemoryBridge) StoreLongTerm(ctx context.Context, entry abstraction.MemoryEntry) error {
	if b.store == nil {
		return fmt.Errorf("memory bridge: store not configured")
	}

	tenantID := entry.Metadata["tenant_id"]
	userID := entry.Metadata["user_id"]
	sessionID := entry.Metadata["session_id"]
	if tenantID == "" || userID == "" || sessionID == "" {
		return fmt.Errorf("memory bridge: tenant_id, user_id, and session_id required in metadata")
	}

	return b.store.StoreSummary(ctx, tenantID, userID, sessionID, entry.Content)
}

// RetrieveSimilar performs semantic search against the vector store if configured,
// falling back to keyword-based matching against session history.
func (b *MarkdownMemoryBridge) RetrieveSimilar(ctx context.Context, query string, limit int) ([]abstraction.MemoryEntry, error) {
	if b.store == nil {
		return nil, nil
	}

	scope := memoryScopeFromContext(ctx)
	if scope.TenantID == "" || scope.UserID == "" || scope.SessionID == "" {
		return nil, nil
	}

	// Try vector semantic search first (if configured)
	if b.vectorStore != nil && b.embeddingSvc != nil {
		results, err := b.vectorSearch(ctx, scope, query, limit)
		if err == nil && len(results) > 0 {
			return results, nil
		}
		if err != nil {
			slog.WarnContext(ctx, "memory bridge: vector search failed, falling back to keyword",
				"error", err)
		}
	}

	// Fallback: keyword matching
	return b.keywordSearch(ctx, scope, query, limit)
}

// vectorSearch performs semantic search via embedding + vector store.
func (b *MarkdownMemoryBridge) vectorSearch(ctx context.Context, scope MemoryScope, query string, limit int) ([]abstraction.MemoryEntry, error) {
	embedding, err := b.embeddingSvc.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	results, err := b.vectorStore.Search(ctx, embedding, SearchOptions{
		TenantID: scope.TenantID,
		UserID:   scope.UserID,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
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

// keywordSearch performs keyword-based matching against session history.
func (b *MarkdownMemoryBridge) keywordSearch(ctx context.Context, scope MemoryScope, query string, limit int) ([]abstraction.MemoryEntry, error) {

	// Get all history for this session
	msgs, err := b.store.GetHistory(ctx, scope.TenantID, scope.UserID, scope.SessionID, 0)
	if err != nil {
		slog.WarnContext(ctx, "memory bridge: failed to get history for similar retrieval",
			"error", err)
		return nil, abstraction.ErrRetrieveFailed
	}

	if len(msgs) == 0 {
		return nil, nil
	}

	// Tokenize query
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Score each message by keyword overlap
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

// RetrieveByTag performs a best-effort scan of session history for tag matches.
// Requires ALL tags to be present in the message content (AND semantics).
// Falls back to content-substring matching since MarkdownMemoryStore does not
// store structured tags per message.
func (b *MarkdownMemoryBridge) RetrieveByTag(ctx context.Context, tags []string, limit int) ([]abstraction.MemoryEntry, error) {
	if b.store == nil || len(tags) == 0 {
		return nil, nil
	}

	scope := memoryScopeFromContext(ctx)
	if scope.TenantID == "" || scope.UserID == "" || scope.SessionID == "" {
		return nil, nil
	}

	msgs, err := b.store.GetHistory(ctx, scope.TenantID, scope.UserID, scope.SessionID, 0)
	if err != nil {
		return nil, abstraction.ErrRetrieveFailed
	}

	var results []abstraction.MemoryEntry
	for _, m := range msgs {
		// ALL tags must match (AND semantics per interface contract)
		allMatch := true
		for _, tag := range tags {
			if !strings.Contains(strings.ToLower(m.Content), strings.ToLower(tag)) {
				allMatch = false
				break
			}
		}
		if allMatch {
			results = append(results, abstraction.MemoryEntry{
				Content: m.Content,
				Tags:    []string{"role:" + m.Role},
			})
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// Forget removes a specific memory entry.
// Markdown store is append-only — individual message deletion is not supported.
func (b *MarkdownMemoryBridge) Forget(_ context.Context, _ string) error {
	return abstraction.ErrMemoryNotFound
}

// Summarize produces a summary from the given entries.
// If the summarizer (LLM-based) is available, it is used for real summarization.
// Otherwise, falls back to concatenation with rune-safe truncation.
func (b *MarkdownMemoryBridge) Summarize(ctx context.Context, entries []abstraction.MemoryEntry) (abstraction.MemoryEntry, error) {
	if len(entries) == 0 {
		return abstraction.MemoryEntry{}, fmt.Errorf("memory bridge: no entries to summarize: %w", abstraction.ErrSummarizeFailed)
	}

	// Combine entry contents
	var parts []string
	for _, e := range entries {
		if e.Content != "" {
			parts = append(parts, e.Content)
		}
	}

	if len(parts) == 0 {
		return abstraction.MemoryEntry{}, fmt.Errorf("memory bridge: all entries have empty content: %w", abstraction.ErrSummarizeFailed)
	}

	// Use LLM summarizer if available (best-effort)
	if b.summarizer != nil && b.store != nil {
		scope := memoryScopeFromContext(ctx)
		if scope.TenantID != "" && scope.UserID != "" && scope.SessionID != "" {
			// Trigger summarization check which generates and stores summary
			b.summarizer.CheckAndSummarize(ctx, scope.TenantID, scope.UserID, scope.SessionID)
			// Retrieve the stored summary
			if summary, err := b.store.GetSummary(ctx, scope.TenantID, scope.UserID, scope.SessionID); err == nil && summary != "" {
				return abstraction.MemoryEntry{
					ID:      "summary",
					Content: summary,
					Tags:    []string{"summary"},
				}, nil
			}
		}
	}

	// Fallback: concatenate with rune-safe truncation
	content := strings.Join(parts, "\n")
	runes := []rune(content)
	if len(runes) > 2000 {
		content = string(runes[:2000]) + "..."
	}

	return abstraction.MemoryEntry{
		ID:      "summary",
		Content: content,
		Tags:    []string{"summary"},
	}, nil
}

// tokenize splits text into lowercase tokens for keyword matching.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.Fields(text)
	seen := make(map[string]bool, len(words))
	tokens := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,!?;:\"'()")
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		tokens = append(tokens, w)
	}
	return tokens
}

// score counts how many tokens appear in the content.
func score(tokens []string, content string) int {
	lower := strings.ToLower(content)
	count := 0
	for _, t := range tokens {
		if strings.Contains(lower, t) {
			count++
		}
	}
	return count
}
