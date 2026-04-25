package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestHistoryProvider_GetSessionHistory_FallbackToMarkdown(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := memory.NewMarkdownMemoryStore(mdDir)

	// Write a message directly to Markdown
	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	mdStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "hello", Timestamp: ts,
	})

	hp := NewHistoryProvider(mdStore, nil)
	msgs, err := hp.GetSessionHistory(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSessionHistory: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("messages = %v, want 1 message with 'hello'", msgs)
	}
}

func TestHistoryProvider_DeleteSession(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := memory.NewMarkdownMemoryStore(mdDir)

	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	mdStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "delete me", Timestamp: ts,
	})

	hp := NewHistoryProvider(mdStore, nil)
	if err := hp.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	msgs, _ := hp.GetSessionHistory(ctx, "s1")
	if len(msgs) != 0 {
		t.Errorf("after delete, messages = %d, want 0", len(msgs))
	}
}

func TestHistoryProvider_ListSessions_NoTenant(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := memory.NewMarkdownMemoryStore(mdDir)

	ctx := context.Background()
	hp := NewHistoryProvider(mdStore, nil)

	sessions, err := hp.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("empty store should return 0 sessions, got %d", len(sessions))
	}
}

func TestHistoryProvider_ListSessions_WithTenantContext(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := memory.NewMarkdownMemoryStore(mdDir)

	// Write a session
	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	mdStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "hello", Timestamp: ts,
	})

	// Create context with user (tenant isolation)
	user := &auth.User{ID: "u1", TenantID: "t1"}
	ctxWithUser := middleware.WithUser(ctx, user)

	hp := NewHistoryProvider(mdStore, nil)
	sessions, err := hp.ListSessions(ctxWithUser)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}

func TestHistoryProvider_GetSessionHistory_ConvertMessageFormat(t *testing.T) {
	mdDir := t.TempDir()
	mdStore, _ := memory.NewMarkdownMemoryStore(mdDir)

	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	mdStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "user", Content: "hi", Timestamp: ts,
	})
	mdStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1",
		Role: "assistant", Content: "hello", Timestamp: ts,
	})

	hp := NewHistoryProvider(mdStore, nil)
	msgs, _ := hp.GetSessionHistory(ctx, "s1")

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("msg[0].Role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[1].Role != "assistant" {
		t.Errorf("msg[1].Role = %q, want %q", msgs[1].Role, "assistant")
	}
}

// Verify the returned type satisfies the interface
func TestHistoryProvider_ImplementsInterface(t *testing.T) {
	var _ api.HistoryProvider = (*HistoryProvider)(nil)
}
