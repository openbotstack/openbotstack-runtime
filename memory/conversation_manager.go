package memory

import (
	"context"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

// ConversationContext holds all context needed for a single conversation turn.
// It consolidates history, memory retrieval, and summary loading into one result
// to avoid duplicate retrieval across HarnessAgent and RuntimeContextAssembler.
type ConversationContext struct {
	// History is the conversation history (summary + messages) for this session.
	History []aitypes.Message
	// MemoryEntries contains the results of semantic memory retrieval.
	MemoryEntries []abstraction.MemoryEntry
	// Summary is the stored conversation summary, if any.
	Summary string
}

// ConversationManager consolidates conversation history loading, memory retrieval,
// and summary loading behind a single interface. It eliminates duplicate
// RetrieveSimilar calls between HarnessAgent.buildPlannerContext and
// RuntimeContextAssembler.Assemble.
type ConversationManager struct {
	convStore     coreagent.ConversationStore
	memoryManager abstraction.MemoryManager
	maxMessages   int
}

// NewConversationManager creates a ConversationManager.
// convStore and memoryManager may be nil; missing components are handled gracefully.
// maxMessages controls how many history messages to load (0 = use default of 50).
func NewConversationManager(convStore coreagent.ConversationStore, memoryManager abstraction.MemoryManager, maxMessages int) *ConversationManager {
	if maxMessages <= 0 {
		maxMessages = 50
	}
	return &ConversationManager{
		convStore:     convStore,
		memoryManager: memoryManager,
		maxMessages:   maxMessages,
	}
}

// GetConversationContext loads conversation context for a single turn.
// Performs memory retrieval exactly once, even if called from multiple callers.
func (cm *ConversationManager) GetConversationContext(ctx context.Context, sessionID, message, tenantID, userID string) (*ConversationContext, error) {
	result := &ConversationContext{}

	// 1. Load summary + history from conversation store
	if cm.convStore != nil && sessionID != "" {
		summary, err := cm.convStore.GetSummary(ctx, tenantID, userID, sessionID)
		if err == nil && summary != "" {
			result.Summary = summary
			result.History = append(result.History, aitypes.Message{
				Role:    "system",
				Content: "Previous conversation summary:\n" + summary,
			})
		}

		msgs, err := cm.convStore.GetHistory(ctx, tenantID, userID, sessionID, cm.maxMessages)
		if err == nil && len(msgs) > 0 {
			result.History = append(result.History, msgs...)
		}
	}

	// 2. Retrieve similar memories (exactly once)
	if cm.memoryManager != nil && message != "" && tenantID != "" && userID != "" && sessionID != "" {
		memCtx := ScopeWithMemory(ctx, MemoryScope{
			TenantID:  tenantID,
			UserID:    userID,
			SessionID: sessionID,
		})
		entries, err := cm.memoryManager.RetrieveSimilar(memCtx, message, 5)
		if err == nil && len(entries) > 0 {
			result.MemoryEntries = entries
		}
	}

	return result, nil
}

// StoreMessage persists a single message via the conversation store.
// Returns nil if conversation store is not configured.
func (cm *ConversationManager) StoreMessage(ctx context.Context, sessionID, tenantID, userID, role, content string) error {
	if cm.convStore == nil || sessionID == "" {
		return nil
	}
	return cm.convStore.AppendMessage(ctx, coreagent.SessionMessage{
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessionID,
		Role:      role,
		Content:   content,
	})
}
