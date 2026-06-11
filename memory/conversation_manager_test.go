package memory

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

// --- Mock implementations ---

type mockConvStoreForCM struct {
	summary   string
	msgs      []aitypes.Message
	appendErr error
}

func (m *mockConvStoreForCM) AppendMessage(_ context.Context, msg coreagent.SessionMessage) error {
	if m.appendErr != nil {
		return m.appendErr
	}
	m.msgs = append(m.msgs, aitypes.NewTextMessage(msg.Role, msg.Content))
	return nil
}
func (m *mockConvStoreForCM) GetHistory(_ context.Context, _, _, _ string, limit int) ([]aitypes.Message, error) {
	if limit > 0 && len(m.msgs) > limit {
		return m.msgs[len(m.msgs)-limit:], nil
	}
	return m.msgs, nil
}
func (m *mockConvStoreForCM) GetSummary(_ context.Context, _, _, _ string) (string, error) {
	return m.summary, nil
}
func (m *mockConvStoreForCM) StoreSummary(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockConvStoreForCM) ClearSession(_ context.Context, _, _, _ string) error     { return nil }

type mockMemoryForCM struct {
	entries     []abstraction.MemoryEntry
	retrieveErr error
	callCount   int
	lastQuery   string
}

func (m *mockMemoryForCM) StoreShortTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *mockMemoryForCM) StoreLongTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *mockMemoryForCM) RetrieveSimilar(_ context.Context, query string, _ int) ([]abstraction.MemoryEntry, error) {
	m.callCount++
	m.lastQuery = query
	if m.retrieveErr != nil {
		return nil, m.retrieveErr
	}
	return m.entries, nil
}
func (m *mockMemoryForCM) RetrieveByTag(_ context.Context, _ []string, _ int) ([]abstraction.MemoryEntry, error) {
	return nil, nil
}
func (m *mockMemoryForCM) Forget(_ context.Context, _ string) error { return nil }
func (m *mockMemoryForCM) Summarize(_ context.Context, _ []abstraction.MemoryEntry) (abstraction.MemoryEntry, error) {
	return abstraction.MemoryEntry{}, nil
}

// --- Tests ---

func TestConversationManager_GetContext_LoadsHistory(t *testing.T) {
	convStore := &mockConvStoreForCM{
		msgs: []aitypes.Message{
			aitypes.NewTextMessage("user", "Hello"),
			aitypes.NewTextMessage("assistant", "Hi there"),
		},
	}
	cm := NewConversationManager(convStore, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "test message", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if len(ctx.History) != 2 {
		t.Errorf("History len = %d, want 2", len(ctx.History))
	}
	if aitypes.FlattenToText(ctx.History[0].Contents) != "Hello" {
		t.Errorf("History[0] = %q, want %q", aitypes.FlattenToText(ctx.History[0].Contents), "Hello")
	}
}

func TestConversationManager_GetContext_LoadsSummary(t *testing.T) {
	convStore := &mockConvStoreForCM{
		summary: "Previous conversation about Go",
	}
	cm := NewConversationManager(convStore, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if ctx.Summary != "Previous conversation about Go" {
		t.Errorf("Summary = %q, want %q", ctx.Summary, "Previous conversation about Go")
	}
	// Summary should NOT be in History (moved to MemoryContext at planner level)
	if len(ctx.History) != 0 {
		t.Errorf("History should be empty (summary not in history), got %d items", len(ctx.History))
	}
}

func TestConversationManager_GetContext_RetrievesMemories(t *testing.T) {
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{
			{Content: "User prefers dark mode"},
			{Content: "User is a developer"},
		},
	}
	cm := NewConversationManager(nil, memory, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "settings", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if len(ctx.MemoryEntries) != 2 {
		t.Errorf("MemoryEntries len = %d, want 2", len(ctx.MemoryEntries))
	}
	if memory.callCount != 1 {
		t.Errorf("RetrieveSimilar call count = %d, want 1 (no duplicate)", memory.callCount)
	}
}

func TestConversationManager_GetContext_NoDuplicateRetrieval(t *testing.T) {
	// Core invariant: RetrieveSimilar is called exactly once
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{{Content: "memory item"}},
	}
	cm := NewConversationManager(nil, memory, 50)

	_, err := cm.GetConversationContext(context.Background(), "sess-1", "query", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if memory.callCount != 1 {
		t.Errorf("RetrieveSimilar called %d times, want exactly 1", memory.callCount)
	}
}

func TestConversationManager_GetContext_NilDependencies_NoPanic(t *testing.T) {
	cm := NewConversationManager(nil, nil, 0)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if ctx == nil {
		t.Fatal("context should not be nil")
	}
	if len(ctx.History) != 0 {
		t.Errorf("History should be empty, got %d items", len(ctx.History))
	}
	if len(ctx.MemoryEntries) != 0 {
		t.Errorf("MemoryEntries should be empty, got %d items", len(ctx.MemoryEntries))
	}
}

func TestConversationManager_GetContext_EmptyMessage_NoMemoryCall(t *testing.T) {
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{{Content: "something"}},
	}
	cm := NewConversationManager(nil, memory, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if memory.callCount != 0 {
		t.Errorf("RetrieveSimilar should not be called for empty message, got %d calls", memory.callCount)
	}
	if len(ctx.MemoryEntries) != 0 {
		t.Errorf("MemoryEntries should be empty for empty message, got %d", len(ctx.MemoryEntries))
	}
}

func TestConversationManager_GetContext_MemoryErrorIsGraceful(t *testing.T) {
	memory := &mockMemoryForCM{
		retrieveErr: fmt.Errorf("memory unavailable"),
	}
	cm := NewConversationManager(nil, memory, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext should not fail on memory error: %v", err)
	}
	if len(ctx.MemoryEntries) != 0 {
		t.Errorf("MemoryEntries should be empty on error, got %d", len(ctx.MemoryEntries))
	}
}

func TestConversationManager_GetContext_FullPipeline(t *testing.T) {
	convStore := &mockConvStoreForCM{
		summary: "Previous: discussed APIs",
		msgs: []aitypes.Message{
			aitypes.NewTextMessage("user", "Tell me about REST"),
			aitypes.NewTextMessage("assistant", "REST is..."),
		},
	}
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{
			{Content: "User knows gRPC"},
		},
	}
	cm := NewConversationManager(convStore, memory, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "more about APIs", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	// Summary NOT in history (moved to MemoryContext) + 2 history messages = 2
	if len(ctx.History) != 2 {
		t.Errorf("History len = %d, want 2 (summary is in MemoryContext)", len(ctx.History))
	}
	if ctx.Summary == "" {
		t.Error("Summary should be populated")
	}
	if len(ctx.MemoryEntries) != 1 {
		t.Errorf("MemoryEntries len = %d, want 1", len(ctx.MemoryEntries))
	}
	if memory.callCount != 1 {
		t.Errorf("RetrieveSimilar called %d times, want 1", memory.callCount)
	}
}

func TestConversationManager_StoreMessage(t *testing.T) {
	convStore := &mockConvStoreForCM{}
	cm := NewConversationManager(convStore, nil, 50)

	err := cm.StoreMessage(context.Background(), "sess-1", "t1", "u1", "user", "hello", "")
	if err != nil {
		t.Fatalf("StoreMessage failed: %v", err)
	}
	if len(convStore.msgs) != 1 {
		t.Fatalf("msgs len = %d, want 1", len(convStore.msgs))
	}
	if aitypes.FlattenToText(convStore.msgs[0].Contents) != "hello" {
		t.Errorf("msg content = %q, want %q", aitypes.FlattenToText(convStore.msgs[0].Contents), "hello")
	}
}

func TestConversationManager_StoreMessage_NilStore_NoPanic(t *testing.T) {
	cm := NewConversationManager(nil, nil, 50)
	err := cm.StoreMessage(context.Background(), "sess-1", "t1", "u1", "user", "hello", "")
	if err != nil {
		t.Fatalf("StoreMessage should return nil with nil store: %v", err)
	}
}

func TestConversationManager_StoreMessage_NilStore_LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	original := slog.Default()
	defer slog.SetDefault(original)
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	cm := NewConversationManager(nil, nil, 50)
	_ = cm.StoreMessage(context.Background(), "sess-1", "t1", "u1", "user", "hello", "")

	logOutput := buf.String()
	if logOutput == "" {
		t.Error("StoreMessage with nil store should log a warning")
	}
}

func TestConversationManager_StoreMessage_EmptySessionID_LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	original := slog.Default()
	defer slog.SetDefault(original)
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))

	convStore := &mockConvStoreForCM{}
	cm := NewConversationManager(convStore, nil, 50)
	_ = cm.StoreMessage(context.Background(), "", "t1", "u1", "user", "hello", "")

	logOutput := buf.String()
	if logOutput == "" {
		t.Error("StoreMessage with empty sessionID should log a warning")
	}
}

func TestConversationManager_DefaultMaxMessages(t *testing.T) {
	cm := NewConversationManager(nil, nil, 0)
	if cm.maxMessages != 50 {
		t.Errorf("maxMessages = %d, want default 50", cm.maxMessages)
	}
}

func TestConversationManager_CustomMaxMessages(t *testing.T) {
	cm := NewConversationManager(nil, nil, 100)
	if cm.maxMessages != 100 {
		t.Errorf("maxMessages = %d, want 100", cm.maxMessages)
	}
}

func TestConversationManager_GetContext_ScopesMemoryCorrectly(t *testing.T) {
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{{Content: "scoped"}},
	}
	cm := NewConversationManager(nil, memory, 50)

	// Without tenant/user/session IDs, should not call memory
	ctx, err := cm.GetConversationContext(context.Background(), "", "query", "", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if memory.callCount != 0 {
		t.Errorf("RetrieveSimilar should not be called without full scope, got %d calls", memory.callCount)
	}
	if len(ctx.MemoryEntries) != 0 {
		t.Errorf("MemoryEntries should be empty without scope, got %d", len(ctx.MemoryEntries))
	}
}

func TestConversationManager_GetContext_MemoryQueryUsesUserMessage(t *testing.T) {
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{{Content: "result"}},
	}
	cm := NewConversationManager(nil, memory, 50)

	_, err := cm.GetConversationContext(context.Background(), "s1", "my specific query", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	if memory.lastQuery != "my specific query" {
		t.Errorf("RetrieveSimilar query = %q, want %q", memory.lastQuery, "my specific query")
	}
}

func TestConversationManager_GetContext_ConcurrentCallsUseSameScope(t *testing.T) {
	// Verify that multiple sequential calls with different tenants don't leak scope
	memory := &mockMemoryForCM{
		entries: []abstraction.MemoryEntry{{Content: "item"}},
	}
	cm := NewConversationManager(nil, memory, 50)

	_, err := cm.GetConversationContext(context.Background(), "s1", "q1", "tenant-A", "u1")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	_, err = cm.GetConversationContext(context.Background(), "s2", "q2", "tenant-B", "u2")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if memory.callCount != 2 {
		t.Errorf("RetrieveSimilar call count = %d, want 2", memory.callCount)
	}
}

// Verify unused import suppression
var _ = time.Second

// mockConvStoreWithMeta supports SummaryMetaProvider for overlap testing
type mockConvStoreWithMeta struct {
	mockConvStoreForCM
	sourceCount int
}

func (m *mockConvStoreWithMeta) GetSummaryMeta(_ context.Context, _, _, _ string) (*SummaryMetadata, error) {
	if m.summary == "" {
		return nil, nil
	}
	return &SummaryMetadata{SourceMessageCount: m.sourceCount}, nil
}

func TestConversationManager_SummaryHistoryNoOverlap(t *testing.T) {
	// 10 messages total, summary covers first 6
	msgs := make([]aitypes.Message, 10)
	for i := range msgs {
		msgs[i] = aitypes.NewTextMessage("user", fmt.Sprintf("message %d", i))
	}
	store := &mockConvStoreWithMeta{
		mockConvStoreForCM: mockConvStoreForCM{
			summary: "Summary of first 6 messages",
			msgs:    msgs,
		},
		sourceCount: 6,
	}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}

	// Should have: 4 history messages only (7-10, skipping 1-6 covered by summary)
	if len(ctx.History) != 4 {
		t.Fatalf("History len = %d, want 4 (summary is in MemoryContext, not history)", len(ctx.History))
	}
	if ctx.Summary == "" {
		t.Error("Summary should be populated")
	}
	// History should be messages 6-9 (index 6,7,8,9)
	for i, msg := range ctx.History {
		text := aitypes.FlattenToText(msg.Contents)
		expected := fmt.Sprintf("message %d", i+6)
		if text != expected {
			t.Errorf("History[%d] = %q, want %q", i+1, text, expected)
		}
	}
}

func TestConversationManager_NoSummaryProvider_FullHistory(t *testing.T) {
	// Without SummaryMetaProvider, all messages are returned (backward compat)
	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", "msg 1"),
		aitypes.NewTextMessage("user", "msg 2"),
	}
	store := &mockConvStoreForCM{
		summary: "Summary",
		msgs:    msgs,
	}
	cm := NewConversationManager(store, nil, 50)

	ctx, err := cm.GetConversationContext(context.Background(), "sess-1", "hello", "t1", "u1")
	if err != nil {
		t.Fatalf("GetConversationContext failed: %v", err)
	}
	// Summary is separate (not in history), all 2 messages in history
	if len(ctx.History) != 2 {
		t.Errorf("History len = %d, want 2 (summary is in MemoryContext)", len(ctx.History))
	}
	if ctx.Summary != "Summary" {
		t.Errorf("Summary = %q, want %q", ctx.Summary, "Summary")
	}
}
