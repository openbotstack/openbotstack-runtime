package memory_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// mockZonedStoreForCompaction implements both ConversationStore and ZonedStore.
type mockZonedStoreForCompaction struct {
	mu         sync.Mutex
	msgs       []aitypes.Message
	zoned      []memory.ZonedMessage
	appendHook func() // called after append for testing
}

func (m *mockZonedStoreForCompaction) AppendMessage(_ context.Context, msg coreagent.SessionMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, aitypes.NewTextMessage(msg.Role, msg.Content))
	if m.appendHook != nil {
		m.appendHook()
	}
	return nil
}
func (m *mockZonedStoreForCompaction) GetHistory(_ context.Context, _, _, _ string, limit int) ([]aitypes.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit > 0 && len(m.msgs) > limit {
		return m.msgs[len(m.msgs)-limit:], nil
	}
	return m.msgs, nil
}
func (m *mockZonedStoreForCompaction) GetSummary(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}
func (m *mockZonedStoreForCompaction) StoreSummary(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockZonedStoreForCompaction) ClearSession(_ context.Context, _, _, _ string) error     { return nil }

func (m *mockZonedStoreForCompaction) GetZonedHistory(_ context.Context, _, _, _ string) ([]memory.ZonedMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.zoned) > 0 {
		return m.zoned, nil
	}
	// Convert flat messages to ZoneFull zoned messages
	var zoned []memory.ZonedMessage
	for i := range m.msgs {
		zoned = append(zoned, memory.ZonedMessage{Zone: memory.ZoneFull, Message: &m.msgs[i]})
	}
	return zoned, nil
}

func (m *mockZonedStoreForCompaction) WriteZonedHistory(_ context.Context, _, _, _ string, zoned []memory.ZonedMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.zoned = zoned
	return nil
}

// mockCompactorForTrigger tracks compaction calls.
type mockCompactorForTrigger struct {
	mu        sync.Mutex
	called    bool
	callCount int
	plan      memory.CompactionPlan
	result    *memory.CompactionResult
	err       error
}

func (m *mockCompactorForTrigger) Compact(_ context.Context, plan memory.CompactionPlan) (*memory.CompactionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.callCount++
	m.plan = plan
	if m.result != nil {
		return m.result, nil
	}
	return &memory.CompactionResult{}, m.err
}

// --- Test: compaction triggers when token budget exceeded ---

func TestSummarizingStore_CompactionTriggered(t *testing.T) {
	compactor := &mockCompactorForTrigger{
		result: &memory.CompactionResult{
			NewTurnSummaries: []memory.TurnSummary{
				{Topic: "Compressed", Summary: "Short summary."},
			},
			TokensSaved: 100,
		},
	}
	store := &mockZonedStoreForCompaction{}
	// Add messages that exceed the 16000 token budget (64,000 chars needed)
	for i := 0; i < 20; i++ {
		store.msgs = append(store.msgs, aitypes.NewTextMessage("user", strings.Repeat("This is a long message to consume tokens in the context window for testing. ", 100)))
		store.msgs = append(store.msgs, aitypes.NewTextMessage("assistant", strings.Repeat("This is a long response that also consumes tokens in the context window for testing. ", 100)))
	}

	summarizer := memory.NewConversationSummarizer(store, nil, 100)
	wrapper := memory.NewSummarizingConversationStore(store, summarizer)
	wrapper.SetCompactor(compactor)

	// Append 5 messages to satisfy compaction throttle (triggers on every 5th message)
	for i := 0; i < 5; i++ {
		err := wrapper.AppendMessage(context.Background(), coreagent.SessionMessage{
			TenantID:  "t1",
			UserID:    "u1",
			SessionID: "s1",
			Role:      "user",
			Content:   "trigger compaction check for token budget exceeded",
		})
		if err != nil {
			t.Fatalf("AppendMessage error: %v", err)
		}
	}

	// Wait for async compaction goroutine to complete
	time.Sleep(500 * time.Millisecond)
	compactor.mu.Lock()
	called := compactor.called
	compactor.mu.Unlock()

	if !called {
		t.Error("expected compactor.Compact to be called")
	}
}

// --- Test: compaction NOT triggered when within budget ---

func TestSummarizingStore_NoCompactionWithinBudget(t *testing.T) {
	compactor := &mockCompactorForTrigger{}
	store := &mockZonedStoreForCompaction{}
	// Just a few short messages
	store.msgs = []aitypes.Message{
		aitypes.NewTextMessage("user", "Hello"),
		aitypes.NewTextMessage("assistant", "Hi"),
	}

	summarizer := memory.NewConversationSummarizer(store, nil, 100)
	wrapper := memory.NewSummarizingConversationStore(store, summarizer)
	wrapper.SetCompactor(compactor)

	err := wrapper.AppendMessage(context.Background(), coreagent.SessionMessage{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Role:      "user",
		Content:   "short message",
	})
	if err != nil {
		t.Fatalf("AppendMessage error: %v", err)
	}

	if compactor.called {
		t.Error("compactor should NOT be called when within budget")
	}
}

// --- Test: SetCompactor accepts nil ---

func TestSummarizingStore_SetCompactorNil(t *testing.T) {
	store := &mockZonedStoreForCompaction{}
	summarizer := memory.NewConversationSummarizer(store, nil, 100)
	wrapper := memory.NewSummarizingConversationStore(store, summarizer)
	wrapper.SetCompactor(nil) // should not panic

	err := wrapper.AppendMessage(context.Background(), coreagent.SessionMessage{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Role:      "user",
		Content:   "test",
	})
	if err != nil {
		t.Fatalf("AppendMessage error: %v", err)
	}
}

// --- Test: ZonedStore interface compliance ---

func TestZonedStoreInterface(t *testing.T) {
	// Verify the interface is correctly defined
	var _ memory.ZonedStore = (*mockZonedStoreForCompaction)(nil)
}

// --- Test: store without ZonedStore support doesn't compact ---

// mockPlainStore implements ConversationStore but NOT ZonedStore.
type mockPlainStore struct {
	msgs []aitypes.Message
}

func (m *mockPlainStore) AppendMessage(_ context.Context, msg coreagent.SessionMessage) error {
	m.msgs = append(m.msgs, aitypes.NewTextMessage(msg.Role, msg.Content))
	return nil
}
func (m *mockPlainStore) GetHistory(_ context.Context, _, _, _ string, limit int) ([]aitypes.Message, error) {
	if limit > 0 && len(m.msgs) > limit {
		return m.msgs[len(m.msgs)-limit:], nil
	}
	return m.msgs, nil
}
func (m *mockPlainStore) GetSummary(_ context.Context, _, _, _ string) (string, error)        { return "", nil }
func (m *mockPlainStore) StoreSummary(_ context.Context, _, _, _, _ string) error              { return nil }
func (m *mockPlainStore) ClearSession(_ context.Context, _, _, _ string) error                 { return nil }

func TestSummarizingStore_NoZonedStoreSupport(t *testing.T) {
	compactor := &mockCompactorForTrigger{}
	store := &mockPlainStore{
		msgs: []aitypes.Message{
			aitypes.NewTextMessage("user", "Hello"),
		},
	}
	summarizer := memory.NewConversationSummarizer(store, nil, 100)
	wrapper := memory.NewSummarizingConversationStore(store, summarizer)
	wrapper.SetCompactor(compactor)

	err := wrapper.AppendMessage(context.Background(), coreagent.SessionMessage{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Role:      "user",
		Content:   "test",
	})
	if err != nil {
		t.Fatalf("AppendMessage error: %v", err)
	}

	if compactor.called {
		t.Error("compactor should not be called for non-ZonedStore")
	}
}
