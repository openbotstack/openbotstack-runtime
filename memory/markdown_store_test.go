package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

func testStore(t *testing.T) *memory.MarkdownMemoryStore {
	t.Helper()
	dir := t.TempDir()
	store, err := memory.NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("NewMarkdownMemoryStore: %v", err)
	}
	return store
}

func TestNewMarkdownMemoryStoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	store, err := memory.NewMarkdownMemoryStore(dir)
	if err != nil {
		t.Fatalf("NewMarkdownMemoryStore: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected data directory to be created")
	}
	_ = store
}

func TestAppendAndGetHistory(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	msgs := []agent.SessionMessage{
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "Hello", Timestamp: "2026-04-14T10:00:00Z"},
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "assistant", Content: "Hi there!", Timestamp: "2026-04-14T10:00:05Z"},
		{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "How are you?", Timestamp: "2026-04-14T10:00:10Z"},
	}

	for _, msg := range msgs {
		if err := store.AppendMessage(ctx, msg); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	history, err := store.GetHistory(ctx, "t1", "u1", "s1", 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(history))
	}
	if history[0].Role != "user" || history[0].Content != "Hello" {
		t.Errorf("first message = %+v, want user/Hello", history[0])
	}
	if history[2].Role != "user" || history[2].Content != "How are you?" {
		t.Errorf("third message = %+v, want user/How are you?", history[2])
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	history, err := store.GetHistory(ctx, "t1", "u1", "nonexistent", 0)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}
	if history == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(history) != 0 {
		t.Errorf("expected 0 messages, got %d", len(history))
	}
}

func TestGetHistoryMaxMessages(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		msg := agent.SessionMessage{
			TenantID: "t1", UserID: "u1", SessionID: "s1",
			Role: "user", Content: string(rune('a' + i)),
			Timestamp: "2026-04-14T10:00:00Z",
		}
		if err := store.AppendMessage(ctx, msg); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}

	history, err := store.GetHistory(ctx, "t1", "u1", "s1", 5)
	if err != nil {
		t.Fatalf("GetHistory: %v", err)
	}

	if len(history) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(history))
	}
	// Should return the LAST 5 messages (f, g, h, i, j)
	if history[0].Content != "f" {
		t.Errorf("expected first message content 'f', got %q", history[0].Content)
	}
	if history[4].Content != "j" {
		t.Errorf("expected last message content 'j', got %q", history[4].Content)
	}
}

func TestTenantIsolation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	msg1 := agent.SessionMessage{TenantID: "tenantA", UserID: "u1", SessionID: "s1", Role: "user", Content: "secret A", Timestamp: "2026-04-14T10:00:00Z"}
	msg2 := agent.SessionMessage{TenantID: "tenantB", UserID: "u1", SessionID: "s1", Role: "user", Content: "secret B", Timestamp: "2026-04-14T10:00:00Z"}

	if err := store.AppendMessage(ctx, msg1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(ctx, msg2); err != nil {
		t.Fatal(err)
	}

	historyA, _ := store.GetHistory(ctx, "tenantA", "u1", "s1", 0)
	historyB, _ := store.GetHistory(ctx, "tenantB", "u1", "s1", 0)

	if len(historyA) != 1 || historyA[0].Content != "secret A" {
		t.Errorf("tenantA should see 'secret A', got %v", historyA)
	}
	if len(historyB) != 1 || historyB[0].Content != "secret B" {
		t.Errorf("tenantB should see 'secret B', got %v", historyB)
	}
}

func TestStoreAndGetSummary(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	// No summary initially
	summary, err := store.GetSummary(ctx, "t1", "u1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" {
		t.Errorf("expected empty summary, got %q", summary)
	}

	// Store summary
	if err := store.StoreSummary(ctx, "t1", "u1", "s1", "User asked about the weather."); err != nil {
		t.Fatal(err)
	}

	summary, err = store.GetSummary(ctx, "t1", "u1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if summary != "User asked about the weather." {
		t.Errorf("expected summary, got %q", summary)
	}
}

func TestClearSession(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	msg := agent.SessionMessage{TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "user", Content: "hello", Timestamp: "2026-04-14T10:00:00Z"}
	if err := store.AppendMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if err := store.StoreSummary(ctx, "t1", "u1", "s1", "test summary"); err != nil {
		t.Fatal(err)
	}

	if err := store.ClearSession(ctx, "t1", "u1", "s1"); err != nil {
		t.Fatal(err)
	}

	history, _ := store.GetHistory(ctx, "t1", "u1", "s1", 0)
	if len(history) != 0 {
		t.Errorf("expected empty history after clear, got %d messages", len(history))
	}

	summary, _ := store.GetSummary(ctx, "t1", "u1", "s1")
	if summary != "" {
		t.Errorf("expected empty summary after clear, got %q", summary)
	}
}

func TestSanitizePathTraversal(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tests := []struct {
		name      string
		tenantID  string
		sessionID string
		wantErr   bool
	}{
		{"valid", "t1", "s1", false},
		{"dotdot", "t1", "../etc/passwd", true},
		{"slash", "t1", "s1/sub", true},
		{"backslash", "t1", "s1\\sub", true},
		{"empty tenant", "", "s1", true},
		{"empty session", "t1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.AppendMessage(ctx, agent.SessionMessage{
				TenantID: tt.tenantID, UserID: "u1", SessionID: tt.sessionID,
				Role: "user", Content: "test", Timestamp: "2026-04-14T10:00:00Z",
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("AppendMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConcurrentAppend(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := agent.SessionMessage{
				TenantID: "t1", UserID: "u1", SessionID: "s1",
				Role: "user", Content: string(rune('a' + n%26)),
				Timestamp: "2026-04-14T10:00:00Z",
			}
			if err := store.AppendMessage(ctx, msg); err != nil {
				t.Errorf("AppendMessage %d: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	history, err := store.GetHistory(ctx, "t1", "u1", "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 20 {
		t.Errorf("expected 20 messages, got %d", len(history))
	}
}

func TestLargeMessageWithMarkdown(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	content := "Here is some code:\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\nAnd a list:\n- item 1\n- item 2"

	msg := agent.SessionMessage{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Role: "assistant",
		Content: content, Timestamp: "2026-04-14T10:00:00Z",
	}
	if err := store.AppendMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}

	history, err := store.GetHistory(ctx, "t1", "u1", "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 message, got %d", len(history))
	}
	if history[0].Content != content {
		t.Errorf("content mismatch:\ngot:      %q\nexpected: %q", history[0].Content, content)
	}
}
