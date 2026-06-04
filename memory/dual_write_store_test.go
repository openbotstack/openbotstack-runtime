package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	agent "github.com/openbotstack/openbotstack-core/control/agent"
)

// mockSessionStateStore is a test mock for SessionStateStore.
type mockSessionStateStore struct {
	sessions map[string]SessionMeta
	upsertErr error
	deleteErr error
}

func newMockSessionStateStore() *mockSessionStateStore {
	return &mockSessionStateStore{sessions: make(map[string]SessionMeta)}
}

func (m *mockSessionStateStore) UpsertSession(_ context.Context, meta SessionMeta) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	m.sessions[meta.SessionID] = meta
	return nil
}

func (m *mockSessionStateStore) ListSessions(_ context.Context) ([]SessionInfo, error) {
	var result []SessionInfo
	for _, meta := range m.sessions {
		result = append(result, SessionInfo{
			SessionID:  meta.SessionID,
			TenantID:   meta.TenantID,
			EntryCount: meta.MessageCount,
			LastEntry:  meta.LastMessagePreview,
			CreatedAt:  meta.CreatedAt,
			UpdatedAt:  meta.UpdatedAt,
		})
	}
	return result, nil
}

func (m *mockSessionStateStore) GetSession(_ context.Context, sessionID string) (*SessionInfo, error) {
	meta, ok := m.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return &SessionInfo{
		SessionID:  meta.SessionID,
		TenantID:   meta.TenantID,
		EntryCount: meta.MessageCount,
		LastEntry:  meta.LastMessagePreview,
		CreatedAt:  meta.CreatedAt,
		UpdatedAt:  meta.UpdatedAt,
	}, nil
}

func (m *mockSessionStateStore) DeleteSession(_ context.Context, sessionID string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.sessions, sessionID)
	return nil
}

func TestDualWrite_AppendMessage_WritesToBoth(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, err := NewMarkdownMemoryStore(mdDir)
	if err != nil {
		t.Fatalf("create md store: %v", err)
	}
	mockState := newMockSessionStateStore()
	dw := NewDualWriteConversationStore(mdStore, mockState)

	ctx := context.Background()
	msg := agent.SessionMessage{
		TenantID:  "tenant-a",
		UserID:    "user-1",
		SessionID: "sess-1",
		Role:      "user",
		Content:   "Hello world",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := dw.AppendMessage(ctx, msg); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Verify Markdown was written
	history, err := mdStore.GetHistory(ctx, "tenant-a", "user-1", "sess-1", 10)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 1 || aitypes.FlattenToText(history[0].Contents) != "Hello world" {
		t.Errorf("Markdown history = %v, want 1 message 'Hello world'", history)
	}

	// Verify SQLite metadata was written
	if len(mockState.sessions) != 1 {
		t.Fatalf("SQLite sessions = %d, want 1", len(mockState.sessions))
	}
	meta := mockState.sessions["sess-1"]
	if meta.SessionID != "sess-1" {
		t.Errorf("meta.SessionID = %q, want %q", meta.SessionID, "sess-1")
	}
	if meta.TenantID != "tenant-a" {
		t.Errorf("meta.TenantID = %q, want %q", meta.TenantID, "tenant-a")
	}
}

func TestDualWrite_AppendMessage_SQLiteFailureGraceful(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := NewMarkdownMemoryStore(mdDir)
	mockState := newMockSessionStateStore()
	mockState.upsertErr = errors.New("sqlite down")
	dw := NewDualWriteConversationStore(mdStore, mockState)

	ctx := context.Background()
	msg := agent.SessionMessage{
		TenantID:  "tenant-a",
		UserID:    "user-1",
		SessionID: "sess-1",
		Role:      "user",
		Content:   "Hello",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Should NOT fail — SQLite failure is best-effort
	if err := dw.AppendMessage(ctx, msg); err != nil {
		t.Fatalf("AppendMessage should not fail on SQLite error: %v", err)
	}

	// Markdown should still have the message
	history, _ := mdStore.GetHistory(ctx, "tenant-a", "user-1", "sess-1", 10)
	if len(history) != 1 {
		t.Errorf("Markdown should have 1 message despite SQLite failure, got %d", len(history))
	}
}

func TestDualWrite_GetHistory_DelegatesToInner(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := NewMarkdownMemoryStore(mdDir)
	mockState := newMockSessionStateStore()
	dw := NewDualWriteConversationStore(mdStore, mockState)

	ctx := context.Background()
	msg := agent.SessionMessage{
		TenantID:  "tenant-a", UserID: "user-1", SessionID: "sess-1",
		Role: "user", Content: "Test message",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	dw.AppendMessage(ctx, msg)

	history, err := dw.GetHistory(ctx, "tenant-a", "user-1", "sess-1", 10)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("GetHistory returned %d messages, want 1", len(history))
	}
}

func TestDualWrite_ClearSession_DeletesFromBoth(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := NewMarkdownMemoryStore(mdDir)
	mockState := newMockSessionStateStore()
	dw := NewDualWriteConversationStore(mdStore, mockState)

	ctx := context.Background()
	msg := agent.SessionMessage{
		TenantID:  "tenant-a", UserID: "user-1", SessionID: "sess-1",
		Role: "user", Content: "To be deleted",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	dw.AppendMessage(ctx, msg)

	if err := dw.ClearSession(ctx, "tenant-a", "user-1", "sess-1"); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	// Markdown should be cleared
	history, _ := mdStore.GetHistory(ctx, "tenant-a", "user-1", "sess-1", 10)
	if len(history) != 0 {
		t.Errorf("Markdown should be empty after clear, got %d messages", len(history))
	}

	// SQLite metadata should be cleared
	if len(mockState.sessions) != 0 {
		t.Errorf("SQLite should have 0 sessions after clear, got %d", len(mockState.sessions))
	}
}

func TestDualWrite_LongPreviewTruncated(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := NewMarkdownMemoryStore(mdDir)
	mockState := newMockSessionStateStore()
	dw := NewDualWriteConversationStore(mdStore, mockState)

	longContent := make([]byte, 300)
	for i := range longContent {
		longContent[i] = 'A'
	}

	ctx := context.Background()
	msg := agent.SessionMessage{
		TenantID:  "tenant-a", UserID: "user-1", SessionID: "sess-1",
		Role: "user", Content: string(longContent),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}
	dw.AppendMessage(ctx, msg)

	meta := mockState.sessions["sess-1"]
	if len(meta.LastMessagePreview) > 200 {
		t.Errorf("preview length = %d, should be <= 200", len(meta.LastMessagePreview))
	}
}

