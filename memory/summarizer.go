package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
)

const summarizationTimeout = 15 * time.Second

// ConversationSummarizer generates session summaries using an LLM.
type ConversationSummarizer struct {
	store     agent.ConversationStore
	router    providers.ModelRouter
	threshold int
}

// NewConversationSummarizer creates a summarizer that triggers after threshold messages.
func NewConversationSummarizer(store agent.ConversationStore, router providers.ModelRouter, threshold int) *ConversationSummarizer {
	return &ConversationSummarizer{
		store:     store,
		router:    router,
		threshold: threshold,
	}
}

// CheckAndSummarize checks if a session exceeds the message threshold and generates a summary.
// Bounded by the context timeout set by the caller.
func (s *ConversationSummarizer) CheckAndSummarize(ctx context.Context, tenantID, userID, sessionID string) {
	msgs, err := s.store.GetHistory(ctx, tenantID, userID, sessionID, 0)
	if err != nil || len(msgs) < s.threshold {
		return
	}

	// Check if summary already exists
	existing, _ := s.store.GetSummary(ctx, tenantID, userID, sessionID)
	if existing != "" {
		return
	}

	slog.InfoContext(ctx, "memory: generating session summary",
		"tenant_id", tenantID, "user_id", userID, "session_id", sessionID, "message_count", len(msgs))

	summary, err := s.generateSummary(ctx, msgs)
	if err != nil {
		slog.WarnContext(ctx, "memory: summarization failed",
			"tenant_id", tenantID, "user_id", userID, "session_id", sessionID, "error", err)
		return
	}

	if err := s.store.StoreSummary(ctx, tenantID, userID, sessionID, summary); err != nil {
		slog.WarnContext(ctx, "memory: failed to store summary",
			"tenant_id", tenantID, "session_id", sessionID, "error", err)
		return
	}

	slog.InfoContext(ctx, "memory: session summary stored",
		"tenant_id", tenantID, "session_id", sessionID, "summary_length", len(summary))
}

func (s *ConversationSummarizer) generateSummary(ctx context.Context, msgs []agent.Message) (string, error) {
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, topics, and any important context. Write in third person.\n\n")
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}

	prov, err := s.router.Route([]skills.CapabilityType{skills.CapTextGeneration}, skills.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("summarizer: routing failed: %w", err)
	}

	resp, err := prov.Generate(ctx, skills.GenerateRequest{
		Messages: []skills.Message{
			{Role: "user", Content: sb.String()},
		},
	})
	if err != nil {
		return "", fmt.Errorf("summarizer: LLM call failed: %w", err)
	}

	return strings.TrimSpace(resp.Content), nil
}

// SummarizingConversationStore wraps a ConversationStore and triggers summarization
// after each message append when the threshold is reached.
// Uses per-session dedup and a threshold counter to avoid redundant LLM calls,
// and a bounded semaphore for the async indexer.
type SummarizingConversationStore struct {
	inner      agent.ConversationStore
	summarizer *ConversationSummarizer
	indexer    *AsyncEmbeddingIndexer // optional: nil = vector indexing disabled

	// Per-session summarization dedup + threshold counter
	mu        sync.Mutex
	pending   map[string]struct{} // sessions with in-flight summarization
	counts    map[string]int      // sessionID -> message count since last summarization
	threshold int                 // cached from summarizer

	// Bounded concurrency for indexer
	idxSem chan struct{} // semaphore, cap = maxConcurrentIndexing
}

// NewSummarizingConversationStore creates a decorator that auto-summarizes conversations.
func NewSummarizingConversationStore(inner agent.ConversationStore, summarizer *ConversationSummarizer) *SummarizingConversationStore {
	return &SummarizingConversationStore{
		inner:      inner,
		summarizer: summarizer,
		pending:    make(map[string]struct{}),
		counts:     make(map[string]int),
		threshold:  summarizer.threshold,
		idxSem:     make(chan struct{}, 16),
	}
}

// SetIndexer sets the async embedding indexer for vector search.
func (s *SummarizingConversationStore) SetIndexer(indexer *AsyncEmbeddingIndexer) {
	s.indexer = indexer
}

func (s *SummarizingConversationStore) AppendMessage(ctx context.Context, msg agent.SessionMessage) error {
	if err := s.inner.AppendMessage(ctx, msg); err != nil {
		return err
	}

	// Bounded indexer: try-acquire semaphore, skip if full
	if s.indexer != nil {
		select {
		case s.idxSem <- struct{}{}:
			idx := s.indexer // capture
			go func() {
				defer func() { <-s.idxSem }()
				idx.OnMessage(context.Background(), msg)
			}()
		default:
			slog.Warn("indexer pool full, skipping embedding", "session_id", msg.SessionID)
		}
	}

	// Threshold-gated summarization with per-session dedup
	if s.shouldTriggerSummarization(msg.SessionID) {
		sumCtx, cancel := context.WithTimeout(context.Background(), summarizationTimeout)
		go func() {
			defer cancel()
			defer s.clearPending(msg.SessionID)
			s.summarizer.CheckAndSummarize(sumCtx, msg.TenantID, msg.UserID, msg.SessionID)
		}()
	}

	return nil
}

// shouldTriggerSummarization returns true when a summarization goroutine should be
// launched for the given session. It enforces threshold gating (first trigger at
// threshold, then every threshold/2 messages) and per-session dedup (skip if a
// summarization is already in-flight for this session).
func (s *SummarizingConversationStore) shouldTriggerSummarization(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.pending[sessionID]; ok {
		s.counts[sessionID]++
		return false
	}

	s.counts[sessionID]++
	count := s.counts[sessionID]

	if count < s.threshold {
		return false
	}
	// Trigger at threshold, then every threshold/2 after
	if count == s.threshold || (count > s.threshold && s.threshold > 0 && (count-s.threshold)%(s.threshold/2) == 0) {
		s.pending[sessionID] = struct{}{}
		return true
	}
	return false
}

func (s *SummarizingConversationStore) clearPending(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, sessionID)
}

func (s *SummarizingConversationStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]agent.Message, error) {
	return s.inner.GetHistory(ctx, tenantID, userID, sessionID, maxMessages)
}

func (s *SummarizingConversationStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return s.inner.GetSummary(ctx, tenantID, userID, sessionID)
}

func (s *SummarizingConversationStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return s.inner.StoreSummary(ctx, tenantID, userID, sessionID, summary)
}

func (s *SummarizingConversationStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	return s.inner.ClearSession(ctx, tenantID, userID, sessionID)
}
