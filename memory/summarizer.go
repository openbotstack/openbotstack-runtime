package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

const summarizationTimeout = 15 * time.Second

// ConversationSummarizer generates session summaries using an LLM.
type ConversationSummarizer struct {
	store     coreagent.ConversationStore
	router    providers.ModelRouter
	threshold int
}

// NewConversationSummarizer creates a summarizer that triggers after threshold messages.
func NewConversationSummarizer(store coreagent.ConversationStore, router providers.ModelRouter, threshold int) *ConversationSummarizer {
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

func (s *ConversationSummarizer) generateSummary(ctx context.Context, msgs []aitypes.Message) (string, error) {
	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, topics, and any important context. Write in third person.\n\n")
	for _, m := range msgs {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(aitypes.FlattenToText(m.Contents))
		sb.WriteString("\n")
	}

	prov, err := s.router.Route([]aitypes.CapabilityType{aitypes.CapTextGeneration}, aitypes.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("summarizer: routing failed: %w", err)
	}

	resp, err := prov.Generate(ctx, aitypes.GenerateRequest{
		Messages: []aitypes.Message{
			aitypes.NewTextMessage("user", sb.String()),
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
	inner      coreagent.ConversationStore
	summarizer *ConversationSummarizer
	indexer    *AsyncEmbeddingIndexer // optional: nil = vector indexing disabled
	compactor  Compactor              // optional: nil = compaction disabled

	// Per-session summarization dedup + threshold counter
	mu        sync.Mutex
	pending   map[string]struct{} // sessions with in-flight summarization
	counts    map[string]int      // sessionID -> message count since last summarization
	threshold int                 // cached from summarizer

	// Per-session compaction dedup + throttle
	compactionPending map[string]struct{}
	compactionCounts  map[string]int // sessionID -> message count since last compaction

	// Token budget for compaction trigger
	maxTokens int

	// Bounded concurrency for indexer
	idxSem chan struct{} // semaphore, cap = maxConcurrentIndexing
}

// NewSummarizingConversationStore creates a decorator that auto-summarizes conversations.
func NewSummarizingConversationStore(inner coreagent.ConversationStore, summarizer *ConversationSummarizer) *SummarizingConversationStore {
	return &SummarizingConversationStore{
		inner:             inner,
		summarizer:        summarizer,
		pending:           make(map[string]struct{}),
		counts:            make(map[string]int),
		compactionPending: make(map[string]struct{}),
		compactionCounts:  make(map[string]int),
		threshold:         summarizer.threshold,
		maxTokens:         16000,
		idxSem:            make(chan struct{}, 16),
	}
}

// SetIndexer sets the async embedding indexer for vector search.
func (s *SummarizingConversationStore) SetIndexer(indexer *AsyncEmbeddingIndexer) {
	s.indexer = indexer
}

// SetCompactor sets the session compactor for progressive compression.
func (s *SummarizingConversationStore) SetCompactor(compactor Compactor) {
	s.compactor = compactor
}

func (s *SummarizingConversationStore) AppendMessage(ctx context.Context, msg coreagent.SessionMessage) error {
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

	// Initialize state from persistent store if needed
	s.initializeSessionState(ctx, msg.TenantID, msg.UserID, msg.SessionID)

	// Threshold-gated summarization with per-session dedup
	if s.shouldTriggerSummarization(msg.SessionID) {
		sumCtx, cancel := context.WithTimeout(context.Background(), summarizationTimeout)
		go func() {
			defer cancel()
			defer s.clearPending(msg.SessionID)
			s.summarizer.CheckAndSummarize(sumCtx, msg.TenantID, msg.UserID, msg.SessionID)
		}()
	}

	// Zone-aware compaction trigger (gated by message count threshold)
	if s.compactor != nil {
		if zoned, ok := s.inner.(ZonedStore); ok {
			if s.shouldTriggerCompaction(msg.SessionID, len(msg.Content)) {
				compCtx, cancel := context.WithTimeout(context.Background(), compactionTimeout)
				go func() {
					defer cancel()
					defer s.clearCompactionPending(msg.SessionID)
					s.runCompaction(compCtx, zoned, msg.TenantID, msg.UserID, msg.SessionID)
				}()
			}
		}
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
	// Trigger at threshold; counter resets after each summarization via clearPending
	// Wait, if count is strictly increasing, clearPending deletes it from s.counts,
	// so it starts from 0 again. Then count will grow to threshold again.
	// We don't need (count-s.threshold)%(s.threshold/2) if it resets to 0.
	// The original code did: `count == s.threshold || ...`. Let's just trigger at threshold.
	if count >= s.threshold {
		s.pending[sessionID] = struct{}{}
		return true
	}
	return false
}

// initializeSessionState ensures local counters are initialized from the store.
func (s *SummarizingConversationStore) initializeSessionState(ctx context.Context, tenantID, userID, sessionID string) {
	s.mu.Lock()
	_, ok1 := s.counts[sessionID]
	_, ok2 := s.compactionCounts[sessionID]
	if ok1 && ok2 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	count := 0
	if provider, ok := s.inner.(MessageCountProvider); ok {
		c, err := provider.GetMessageCount(ctx, tenantID, userID, sessionID)
		if err == nil {
			count = c
		}
	} else if msgs, err := s.inner.GetHistory(ctx, tenantID, userID, sessionID, 0); err == nil {
		count = len(msgs)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.counts[sessionID]; !ok {
		// Use modulo threshold to avoid instant triggers on old long sessions
		if s.threshold > 0 {
			s.counts[sessionID] = count % s.threshold
		} else {
			s.counts[sessionID] = count
		}
	}
	if _, ok := s.compactionCounts[sessionID]; !ok {
		s.compactionCounts[sessionID] = count
	}
}

func (s *SummarizingConversationStore) clearPending(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, sessionID)
	delete(s.counts, sessionID)
}

func (s *SummarizingConversationStore) shouldTriggerCompaction(sessionID string, contentLen int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.compactionPending[sessionID]; ok {
		s.compactionCounts[sessionID]++
		return false
	}
	s.compactionCounts[sessionID]++
	// Skip trivial messages to avoid unnecessary goroutine launches
	if contentLen < 20 {
		return false
	}
	// Throttle: only check every 5 messages
	if s.compactionCounts[sessionID]%5 != 0 {
		return false
	}
	s.compactionPending[sessionID] = struct{}{}
	return true
}

func (s *SummarizingConversationStore) clearCompactionPending(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.compactionPending, sessionID)
	// Keep compactionCounts to maintain throttle across compaction rounds
}

func (s *SummarizingConversationStore) runCompaction(ctx context.Context, store ZonedStore, tenantID, userID, sessionID string) {
	zoned, err := store.GetZonedHistory(ctx, tenantID, userID, sessionID)
	if err != nil || len(zoned) == 0 {
		return
	}

	totalTokens := EstimateZonedTokens(zoned)
	if totalTokens <= s.maxTokens {
		return
	}

	// Build compaction plan
	plan := s.buildCompactionPlan(zoned)
	result, err := s.compactor.Compact(ctx, plan)
	if err != nil {
		slog.Warn("compaction failed", "session_id", sessionID, "error", err)
		return
	}

	// Rebuild zoned messages with compaction result
	updated := s.applyCompactionResult(plan, result)
	if err := store.WriteZonedHistory(ctx, tenantID, userID, sessionID, updated); err != nil {
		slog.Warn("compaction write failed", "session_id", sessionID, "error", err)
	}
}

func (s *SummarizingConversationStore) buildCompactionPlan(zoned []ZonedMessage) CompactionPlan {
	var plan CompactionPlan
	var fullMsgs []aitypes.Message
	var compressedTurns []TurnSummary

	for _, zm := range zoned {
		switch zm.Zone {
		case ZoneArchive:
			plan.ArchiveSummary = zm.ArchiveSummary
		case ZoneCompressed:
			if zm.TurnSummary != nil {
				compressedTurns = append(compressedTurns, *zm.TurnSummary)
			}
		case ZoneFull:
			if zm.Message != nil {
				fullMsgs = append(fullMsgs, *zm.Message)
			}
		}
	}

	// Only compress oldest half of full-zone messages
	if len(fullMsgs) > 5 {
		compressCount := len(fullMsgs) / 2
		plan.MessagesToCompress = fullMsgs[:compressCount]
		plan.fullMsgsToKeep = fullMsgs[compressCount:]
	} else {
		plan.fullMsgsToKeep = fullMsgs
	}

	// Only archive oldest half of compressed turns
	if len(compressedTurns) > 5 {
		archiveCount := len(compressedTurns) / 2
		plan.TurnsToArchive = compressedTurns[:archiveCount]
		plan.compressedToKeep = compressedTurns[archiveCount:]
	} else {
		plan.compressedToKeep = compressedTurns
	}

	return plan
}

func (s *SummarizingConversationStore) applyCompactionResult(plan CompactionPlan, result *CompactionResult) []ZonedMessage {
	var updated []ZonedMessage

	// Archive zone
	if result.UpdatedArchive != "" {
		updated = append(updated, ZonedMessage{Zone: ZoneArchive, ArchiveSummary: result.UpdatedArchive})
	} else if plan.ArchiveSummary != "" {
		updated = append(updated, ZonedMessage{Zone: ZoneArchive, ArchiveSummary: plan.ArchiveSummary})
	}

	// Compressed zone: new summaries + kept existing turns
	for _, ts := range result.NewTurnSummaries {
		tsCopy := ts
		updated = append(updated, ZonedMessage{Zone: ZoneCompressed, TurnSummary: &tsCopy})
	}
	for _, ts := range plan.compressedToKeep {
		tsCopy := ts
		updated = append(updated, ZonedMessage{Zone: ZoneCompressed, TurnSummary: &tsCopy})
	}

	// Full zone: only messages not compressed
	for i := range plan.fullMsgsToKeep {
		updated = append(updated, ZonedMessage{Zone: ZoneFull, Message: &plan.fullMsgsToKeep[i]})
	}

	return updated
}

func (s *SummarizingConversationStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]aitypes.Message, error) {
	return s.inner.GetHistory(ctx, tenantID, userID, sessionID, maxMessages)
}

func (s *SummarizingConversationStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return s.inner.GetSummary(ctx, tenantID, userID, sessionID)
}

func (s *SummarizingConversationStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return s.inner.StoreSummary(ctx, tenantID, userID, sessionID, summary)
}

// GetSummaryMeta delegates to inner store if it supports SummaryMetaProvider.
func (s *SummarizingConversationStore) GetSummaryMeta(ctx context.Context, tenantID, userID, sessionID string) (*SummaryMetadata, error) {
	if provider, ok := s.inner.(SummaryMetaProvider); ok {
		return provider.GetSummaryMeta(ctx, tenantID, userID, sessionID)
	}
	return nil, nil
}

func (s *SummarizingConversationStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	err := s.inner.ClearSession(ctx, tenantID, userID, sessionID)
	s.mu.Lock()
	delete(s.counts, sessionID)
	delete(s.pending, sessionID)
	delete(s.compactionPending, sessionID)
	delete(s.compactionCounts, sessionID)
	s.mu.Unlock()
	return err
}
