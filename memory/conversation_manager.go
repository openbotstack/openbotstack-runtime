package memory

import (
	"context"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

// ConversationContext holds all context needed for a single conversation turn.
type ConversationContext struct {
	History       []types.Message
	MemoryEntries []abstraction.MemoryEntry
	Summary       string
	ZonedMessages []ZonedMessage
}

// SummaryMetadata provides information about a stored summary.
type SummaryMetadata struct {
	SourceMessageCount int
}

// SummaryMetaProvider is an optional interface for ConversationStore implementations
// that can provide summary metadata including how many messages the summary covers.
type SummaryMetaProvider interface {
	GetSummaryMeta(ctx context.Context, tenantID, userID, sessionID string) (*SummaryMetadata, error)
}

// ZonedHistoryProvider is an optional interface for stores that support zone-aware history.
type ZonedHistoryProvider interface {
	GetZonedHistory(ctx context.Context, tenantID, userID, sessionID string) ([]ZonedMessage, error)
}

// MessageCountProvider is an optional interface for stores that can efficiently return message counts.
type MessageCountProvider interface {
	GetMessageCount(ctx context.Context, tenantID, userID, sessionID string) (int, error)
}

// ZonedStore extends ZonedHistoryProvider with write support for zoned content.
// MarkdownMemoryStore implements both GetZonedHistory and WriteZonedHistory,
// so the compaction pipeline is active when SummarizingConversationStore wraps
// a MarkdownMemoryStore.
type ZonedStore interface {
	ZonedHistoryProvider
	WriteZonedHistory(ctx context.Context, tenantID, userID, sessionID string, zoned []ZonedMessage) error
}

// ConversationManager consolidates conversation history loading, memory retrieval,
// and summary loading behind a single interface.
type ConversationManager struct {
	convStore     coreagent.ConversationStore
	memoryManager abstraction.MemoryManager
	maxMessages   int
	maxTokens     int

	// Capabilities resolved once at construction from convStore.
	// Eliminates runtime type assertion sniffing on every GetConversationContext call.
	zonedProvider       ZonedHistoryProvider  // nil = legacy flat-history path
	summaryMetaProvider SummaryMetaProvider    // nil = no summary-based skip
}

// NewConversationManager creates a ConversationManager.
// Capabilities (ZonedHistoryProvider, SummaryMetaProvider) are resolved once
// from the convStore at construction time — not sniffed on every call.
func NewConversationManager(convStore coreagent.ConversationStore, memoryManager abstraction.MemoryManager, maxMessages int) *ConversationManager {
	if maxMessages <= 0 {
		maxMessages = 50
	}

	cm := &ConversationManager{
		convStore:     convStore,
		memoryManager: memoryManager,
		maxMessages:   maxMessages,
		maxTokens:     16000,
	}

	// Resolve optional capabilities once at construction.
	caps := ResolveCapabilities(convStore)
	cm.zonedProvider = caps.ZonedProvider
	cm.summaryMetaProvider = caps.SummaryMeta

	return cm
}

// GetConversationContext loads conversation context for a single turn.
func (cm *ConversationManager) GetConversationContext(ctx context.Context, sessionID, message, tenantID, userID string) (*ConversationContext, error) {
	result := &ConversationContext{}

	if cm.convStore != nil && sessionID != "" {
		// Use zone-aware path if capability was resolved at construction.
		if cm.zonedProvider != nil {
			return cm.getZonedContext(ctx, cm.zonedProvider, result, sessionID, message, tenantID, userID)
		}

		// Legacy path: flat history + summary
		cm.loadLegacyContext(ctx, result, sessionID, tenantID, userID)

		// Truncate history to fit token budget
		if cm.maxTokens > 0 && len(result.History) > 0 {
			result.History = harness.TruncateHistoryByToken(result.History, cm.maxTokens)
		}
	}

	// Retrieve similar memories
	cm.retrieveMemories(ctx, result, message, tenantID, userID, sessionID)

	return result, nil
}

func (cm *ConversationManager) loadLegacyContext(ctx context.Context, result *ConversationContext, sessionID, tenantID, userID string) {
	summary, err := cm.convStore.GetSummary(ctx, tenantID, userID, sessionID)
	if err == nil && summary != "" {
		result.Summary = summary
	}

	skipCount := 0
	if result.Summary != "" && cm.summaryMetaProvider != nil {
		meta, metaErr := cm.summaryMetaProvider.GetSummaryMeta(ctx, tenantID, userID, sessionID)
		if metaErr == nil && meta != nil && meta.SourceMessageCount > 0 {
			skipCount = meta.SourceMessageCount
		}
	}

	loadLimit := skipCount + cm.maxMessages
	msgs, err := cm.convStore.GetHistory(ctx, tenantID, userID, sessionID, loadLimit)
	if err == nil && len(msgs) > 0 {
		if skipCount > 0 && skipCount < len(msgs) {
			msgs = msgs[skipCount:]
		}
		if cm.maxMessages > 0 && len(msgs) > cm.maxMessages {
			msgs = msgs[len(msgs)-cm.maxMessages:]
		}
		result.History = append(result.History, msgs...)
	}
}

func (cm *ConversationManager) getZonedContext(ctx context.Context, zoned ZonedHistoryProvider, result *ConversationContext, sessionID, message, tenantID, userID string) (*ConversationContext, error) {
	zonedMsgs, err := zoned.GetZonedHistory(ctx, tenantID, userID, sessionID)
	if err != nil {
		return nil, err
	}

	// Apply token budget truncation to zoned messages
	if cm.maxTokens > 0 && len(zonedMsgs) > 0 {
		zonedMsgs = TruncateZonedMessages(zonedMsgs, cm.maxTokens)
	}

	result.ZonedMessages = zonedMsgs

	// Extract archive as summary
	for _, zm := range zonedMsgs {
		if zm.Zone == ZoneArchive && zm.ArchiveSummary != "" {
			result.Summary = zm.ArchiveSummary
			break
		}
	}

	// Extract full-zone messages as History
	for _, zm := range zonedMsgs {
		if zm.Zone == ZoneFull && zm.Message != nil {
			result.History = append(result.History, *zm.Message)
		}
	}

	// Retrieve similar memories
	cm.retrieveMemories(ctx, result, message, tenantID, userID, sessionID)

	return result, nil
}

func (cm *ConversationManager) retrieveMemories(ctx context.Context, result *ConversationContext, message, tenantID, userID, sessionID string) {
	if cm.memoryManager == nil || message == "" || tenantID == "" || userID == "" || sessionID == "" {
		return
	}
	memCtx := ScopeWithMemory(ctx, MemoryScope{
		TenantID:              tenantID,
		UserID:                userID,
		SessionID:             sessionID,
		ExcludeRecentMessages: len(result.History),
	})
	entries, err := cm.memoryManager.RetrieveSimilar(memCtx, message, 5)
	if err != nil {
		slog.WarnContext(ctx, "conversation_manager: memory retrieval failed", "error", err)
	} else if len(entries) > 0 {
		result.MemoryEntries = entries
	}
}

// TruncateZonedMessages trims zoned messages to fit within a token budget.
// Drops oldest entries in each zone (archive last, compressed next, full first to keep).
func TruncateZonedMessages(msgs []ZonedMessage, maxTokens int) []ZonedMessage {
	total := EstimateZonedTokens(msgs)
	if total <= maxTokens {
		return msgs
	}

	// Strategy: truncate from the beginning (oldest) of each zone
	// First try dropping oldest compressed turns
	result := make([]ZonedMessage, len(msgs))
	copy(result, msgs)

	for total > maxTokens {
		// Find first compressed entry and remove it
		found := false
		for i, zm := range result {
			if zm.Zone == ZoneCompressed {
				result = append(result[:i], result[i+1:]...)
				total = EstimateZonedTokens(result)
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	// If still over budget, truncate full messages from the front
	if total > maxTokens {
		for i := 0; i < len(result) && total > maxTokens; {
			if result[i].Zone == ZoneFull {
				result = append(result[:i], result[i+1:]...)
				total = EstimateZonedTokens(result)
			} else {
				i++
			}
		}
	}

	// Last resort: if archive alone exceeds budget, truncate its text
	if total > maxTokens {
		maxChars := maxTokens * 4
		for i := range result {
			if result[i].Zone == ZoneArchive && len(result[i].ArchiveSummary) > maxChars {
				result[i].ArchiveSummary = result[i].ArchiveSummary[:maxChars]
				total = EstimateZonedTokens(result)
				break
			}
		}
	}

	return result
}

// StoreMessage persists a single message via the conversation store.
func (cm *ConversationManager) StoreMessage(ctx context.Context, sessionID, tenantID, userID, role, content, executionID string) error {
	if cm.convStore == nil || sessionID == "" {
		slog.WarnContext(ctx, "conversation manager: message dropped, no store configured",
			"session_id", sessionID, "has_store", cm.convStore != nil)
		return nil
	}
	return cm.convStore.AppendMessage(ctx, coreagent.SessionMessage{
		TenantID:    tenantID,
		UserID:      userID,
		SessionID:   sessionID,
		Role:        role,
		Content:     content,
		ExecutionID: executionID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}
