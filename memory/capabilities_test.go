package memory

import (
	"context"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

// mockPlainStore implements ConversationStore only — no optional interfaces.
type mockPlainStore struct{}

func (m *mockPlainStore) AppendMessage(ctx context.Context, msg coreagent.SessionMessage) error {
	return nil
}
func (m *mockPlainStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]aitypes.Message, error) {
	return nil, nil
}
func (m *mockPlainStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return "", nil
}
func (m *mockPlainStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return nil
}
func (m *mockPlainStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	return nil
}

// mockSessionState implements SessionStateStore.
type mockSessionState struct{}

func (m *mockSessionState) UpsertSession(ctx context.Context, meta SessionMeta) error { return nil }
func (m *mockSessionState) ListSessions(ctx context.Context) ([]SessionInfo, error)    { return nil, nil }
func (m *mockSessionState) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	return nil, nil
}
func (m *mockSessionState) DeleteSession(ctx context.Context, sessionID string) error { return nil }

// --- ConversationManager: capabilities resolved at construction ---

func TestConversationManager_CapabilitiesFromMarkdownStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	cm := NewConversationManager(store, nil, 50)

	if cm.zonedProvider == nil {
		t.Error("zonedProvider should be resolved at construction — MarkdownMemoryStore implements ZonedHistoryProvider")
	}
	if cm.summaryMetaProvider == nil {
		t.Error("summaryMetaProvider should be resolved at construction — MarkdownMemoryStore implements SummaryMetaProvider")
	}
}

func TestConversationManager_CapabilitiesFromPlainStore(t *testing.T) {
	store := &mockPlainStore{}
	cm := NewConversationManager(store, nil, 50)

	if cm.zonedProvider != nil {
		t.Error("zonedProvider should be nil — mockPlainStore does not implement ZonedHistoryProvider")
	}
	if cm.summaryMetaProvider != nil {
		t.Error("summaryMetaProvider should be nil — mockPlainStore does not implement SummaryMetaProvider")
	}
}

// --- SummarizingConversationStore: capabilities resolved at construction ---

func TestSummarizingStore_CapabilitiesFromMarkdownStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	summarizer := &ConversationSummarizer{store: store, threshold: 10}
	ss := NewSummarizingConversationStore(store, summarizer)

	if ss.innerSummaryMeta == nil {
		t.Error("innerSummaryMeta should be resolved at construction")
	}
	if ss.innerMessageCounter != nil {
		// MarkdownMemoryStore does NOT implement MessageCountProvider
		t.Error("innerMessageCounter should be nil — MarkdownMemoryStore does not implement MessageCountProvider")
	}
}

// --- DualWriteConversationStore: capabilities resolved at construction ---

func TestDualWriteStore_CapabilitiesFromMarkdownStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	dw := NewDualWriteConversationStore(store, &mockSessionState{})

	if dw.innerSummaryMeta == nil {
		t.Error("innerSummaryMeta should be resolved at construction")
	}
	if dw.innerZoned == nil {
		t.Error("innerZoned should be resolved at construction — MarkdownMemoryStore implements ZonedHistoryProvider")
	}
}

func TestDualWriteStore_CapabilitiesFromPlainStore(t *testing.T) {
	store := &mockPlainStore{}
	dw := NewDualWriteConversationStore(store, &mockSessionState{})

	if dw.innerSummaryMeta != nil {
		t.Error("innerSummaryMeta should be nil — mockPlainStore does not implement SummaryMetaProvider")
	}
	if dw.innerZoned != nil {
		t.Error("innerZoned should be nil — mockPlainStore does not implement ZonedHistoryProvider")
	}
}
