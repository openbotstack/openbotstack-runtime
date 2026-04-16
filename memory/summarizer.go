package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
type SummarizingConversationStore struct {
	inner      agent.ConversationStore
	summarizer *ConversationSummarizer
	indexer    *AsyncEmbeddingIndexer // optional: nil = vector indexing disabled
}

// NewSummarizingConversationStore creates a decorator that auto-summarizes conversations.
func NewSummarizingConversationStore(inner agent.ConversationStore, summarizer *ConversationSummarizer) *SummarizingConversationStore {
	return &SummarizingConversationStore{inner: inner, summarizer: summarizer}
}

// SetIndexer sets the async embedding indexer for vector search.
func (s *SummarizingConversationStore) SetIndexer(indexer *AsyncEmbeddingIndexer) {
	s.indexer = indexer
}

func (s *SummarizingConversationStore) AppendMessage(ctx context.Context, msg agent.SessionMessage) error {
	if err := s.inner.AppendMessage(ctx, msg); err != nil {
		return err
	}
	// Trigger async vector indexing (fire-and-forget, no-op if indexer is nil)
	if s.indexer != nil {
		s.indexer.OnMessage(ctx, msg)
	}
	// Trigger summarization check asynchronously with bounded timeout
	sumCtx, cancel := context.WithTimeout(ctx, summarizationTimeout)
	go func() {
		defer cancel()
		s.summarizer.CheckAndSummarize(sumCtx, msg.TenantID, msg.UserID, msg.SessionID)
	}()
	return nil
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
