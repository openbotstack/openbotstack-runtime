package memory

import (
	"context"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

// mockZonedStore supports ZonedHistoryProvider for testing zone-aware context.
type mockZonedStore struct {
	mockConvStoreForCM
	zonedMsgs []ZonedMessage
}

func (m *mockZonedStore) GetZonedHistory(_ context.Context, _, _, _ string) ([]ZonedMessage, error) {
	return m.zonedMsgs, nil
}

// --- Test: zone-aware context assembly with all three zones ---

func TestConversationManager_ZonedContext_AllZones(t *testing.T) {
	msg1 := aitypes.NewTextMessage("user", "Hello")
	msg2 := aitypes.NewTextMessage("assistant", "Hi")
	zoned := []ZonedMessage{
		{Zone: ZoneArchive, ArchiveSummary: "Previous session about API design."},
		{Zone: ZoneCompressed, TurnSummary: &TurnSummary{Topic: "Auth", Summary: "Decided on JWT."}},
		{Zone: ZoneFull, Message: &msg1},
		{Zone: ZoneFull, Message: &msg2},
	}
	store := &mockZonedStore{zonedMsgs: zoned}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	// Should populate ZonedMessages
	if len(ctx.ZonedMessages) != 4 {
		t.Fatalf("ZonedMessages len = %d, want 4", len(ctx.ZonedMessages))
	}

	// Archive becomes Summary
	if ctx.Summary != "Previous session about API design." {
		t.Errorf("Summary = %q", ctx.Summary)
	}

	// Full messages go into History
	if len(ctx.History) != 2 {
		t.Errorf("History len = %d, want 2 (only ZoneFull)", len(ctx.History))
	}
}

// --- Test: zone-aware context with only full zone ---

func TestConversationManager_ZonedContext_OnlyFullZone(t *testing.T) {
	msg := aitypes.NewTextMessage("user", "Hello")
	zoned := []ZonedMessage{
		{Zone: ZoneFull, Message: &msg},
	}
	store := &mockZonedStore{zonedMsgs: zoned}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	if len(ctx.ZonedMessages) != 1 {
		t.Fatalf("ZonedMessages len = %d, want 1", len(ctx.ZonedMessages))
	}
	if len(ctx.History) != 1 {
		t.Errorf("History len = %d, want 1", len(ctx.History))
	}
	if ctx.Summary != "" {
		t.Errorf("Summary should be empty, got %q", ctx.Summary)
	}
}

// --- Test: backward compat - store without ZonedHistoryProvider uses old path ---

func TestConversationManager_ZonedContext_FallbackToOldPath(t *testing.T) {
	store := &mockConvStoreForCM{
		msgs: []aitypes.Message{
			aitypes.NewTextMessage("user", "Hello"),
		},
	}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	// Should NOT populate ZonedMessages (old path)
	if len(ctx.ZonedMessages) != 0 {
		t.Errorf("ZonedMessages should be empty (old path), got %d", len(ctx.ZonedMessages))
	}
	// History should still work
	if len(ctx.History) != 1 {
		t.Errorf("History len = %d, want 1", len(ctx.History))
	}
}

// --- Test: zone-aware context with token budget ---

func TestConversationManager_ZonedContext_TokenBudget(t *testing.T) {
	// Create many messages that exceed budget
	var zoned []ZonedMessage
	for i := 0; i < 20; i++ {
		msg := aitypes.NewTextMessage("user", "This is a longer message to consume tokens in the context window for testing purposes.")
		zoned = append(zoned, ZonedMessage{Zone: ZoneFull, Message: &msg})
	}
	store := &mockZonedStore{zonedMsgs: zoned}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	// History should be truncated to fit within token budget
	totalTokens := EstimateZonedTokens(ctx.ZonedMessages)
	if totalTokens > cm.maxTokens {
		t.Errorf("total tokens = %d, exceeds budget %d", totalTokens, cm.maxTokens)
	}
}

// --- Test: empty zoned messages ---

func TestConversationManager_ZonedContext_Empty(t *testing.T) {
	store := &mockZonedStore{zonedMsgs: nil}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if len(ctx.ZonedMessages) != 0 {
		t.Errorf("ZonedMessages should be empty, got %d", len(ctx.ZonedMessages))
	}
}

// Ensure mockConvStoreForCM still satisfies ConversationStore
var _ coreagent.ConversationStore = (*mockZonedStore)(nil)
var _ ZonedHistoryProvider = (*mockZonedStore)(nil)
