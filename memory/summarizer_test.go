package memory

import (
	"context"
	"sync/atomic"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

type mockSummarizerStore struct {
	coreagent.ConversationStore
	history      []aitypes.Message
	summary      string
	summaryCount atomic.Int32
}

func (m *mockSummarizerStore) AppendMessage(_ context.Context, msg coreagent.SessionMessage) error {
	m.history = append(m.history, aitypes.Message{Role: msg.Role, Contents: []aitypes.ContentBlock{aitypes.NewTextBlock(msg.Content)}})
	return nil
}

func (m *mockSummarizerStore) GetHistory(_ context.Context, _, _, _ string, _ int) ([]aitypes.Message, error) {
	return m.history, nil
}

func (m *mockSummarizerStore) GetSummary(_ context.Context, _, _, _ string) (string, error) {
	return m.summary, nil
}

func (m *mockSummarizerStore) StoreSummary(_ context.Context, _, _, _, summary string) error {
	m.summary = summary
	m.summaryCount.Add(1)
	return nil
}

func (m *mockSummarizerStore) ClearSession(_ context.Context, _, _, _ string) error {
	m.history = nil
	m.summary = ""
	return nil
}

func TestSummarizingStore_ClearSessionCleansUpCounts(t *testing.T) {
	store := &mockSummarizerStore{}
	s := NewSummarizingConversationStore(store, NewConversationSummarizer(store, nil, 10))

	s.mu.Lock()
	s.counts["session-a"] = 42
	s.counts["session-b"] = 15
	s.mu.Unlock()

	_ = s.ClearSession(context.Background(), "t1", "u1", "session-a")

	s.mu.Lock()
	_, hasA := s.counts["session-a"]
	countB := s.counts["session-b"]
	s.mu.Unlock()

	if hasA {
		t.Error("ClearSession should remove session from counts map")
	}
	if countB != 15 {
		t.Errorf("session-b count should be preserved, got %d", countB)
	}
}

func TestSummarizingStore_ClearSessionCleansUpPending(t *testing.T) {
	store := &mockSummarizerStore{}
	s := NewSummarizingConversationStore(store, NewConversationSummarizer(store, nil, 10))

	s.mu.Lock()
	s.pending["session-a"] = struct{}{}
	s.mu.Unlock()

	_ = s.ClearSession(context.Background(), "t1", "u1", "session-a")

	s.mu.Lock()
	_, hasPending := s.pending["session-a"]
	s.mu.Unlock()

	if hasPending {
		t.Error("ClearSession should remove session from pending map")
	}
}
