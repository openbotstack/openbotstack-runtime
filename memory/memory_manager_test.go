package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestMemoryManagerStoreAndRecall(t *testing.T) {
	embedder := memory.NewLocalEmbedder()
	store := memory.NewMilvusStore()
	summarizer := memory.NewLLMSummarizer()
	mgr := memory.NewMemoryManager(embedder, store, summarizer)

	ctx := context.Background()

	if err := mgr.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Store a message
	err := mgr.OnMessage(ctx, "sess-1", "tenant-1", "user-1", "user", "What is the weather like today?")
	if err != nil {
		t.Fatalf("OnMessage failed: %v", err)
	}

	// Recall should find the stored message
	docs, err := mgr.Recall(ctx, "weather today", 5)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(docs) == 0 {
		t.Fatal("Expected at least 1 document from recall, got 0")
	}

	found := false
	for _, doc := range docs {
		if doc.Content == "What is the weather like today?" {
			found = true
			if doc.Metadata["session_id"] != "sess-1" {
				t.Errorf("Expected session_id=sess-1, got %s", doc.Metadata["session_id"])
			}
			if doc.Metadata["tenant_id"] != "tenant-1" {
				t.Errorf("Expected tenant_id=tenant-1, got %s", doc.Metadata["tenant_id"])
			}
		}
	}
	if !found {
		t.Error("Stored message not found in recall results")
	}
}

func TestMemoryManagerMultipleMessages(t *testing.T) {
	embedder := memory.NewLocalEmbedder()
	store := memory.NewMilvusStore()
	summarizer := memory.NewLLMSummarizer()
	mgr := memory.NewMemoryManager(embedder, store, summarizer)

	ctx := context.Background()
	_ = mgr.Initialize(ctx)

	messages := []struct {
		role    string
		content string
	}{
		{"user", "Hello"},
		{"assistant", "Hi there! How can I help?"},
		{"user", "What is 2+2?"},
		{"assistant", "4"},
	}

	for _, m := range messages {
		if err := mgr.OnMessage(ctx, "sess-2", "t1", "u1", m.role, m.content); err != nil {
			t.Fatalf("OnMessage failed: %v", err)
		}
	}

	docs, err := mgr.Recall(ctx, "math question", 10)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(docs) < 4 {
		t.Errorf("Expected at least 4 documents, got %d", len(docs))
	}
}

func TestMemoryManagerSummarizationTriggered(t *testing.T) {
	embedder := memory.NewLocalEmbedder()
	store := memory.NewMilvusStore()
	summarizer := memory.NewLLMSummarizer()
	mgr := memory.NewMemoryManager(embedder, store, summarizer)

	ctx := context.Background()
	_ = mgr.Initialize(ctx)

	// Send 12 messages to trigger summarization (threshold = 10)
	for i := 0; i < 12; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		_ = mgr.OnMessage(ctx, "sess-3", "t1", "u1", role, "message content for turn")
	}

	// Wait for async summarization
	time.Sleep(100 * time.Millisecond)

	// Recall should include the summary
	docs, err := mgr.Recall(ctx, "summary of conversation", 20)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	foundSummary := false
	for _, doc := range docs {
		if doc.Metadata["type"] == "summary" {
			foundSummary = true
			if doc.Content == "" {
				t.Error("Summary content is empty")
			}
		}
	}

	if !foundSummary {
		t.Error("Expected summary document after threshold reached")
	}
}

func TestMemoryManagerEmptyRecall(t *testing.T) {
	embedder := memory.NewLocalEmbedder()
	store := memory.NewMilvusStore()
	summarizer := memory.NewLLMSummarizer()
	mgr := memory.NewMemoryManager(embedder, store, summarizer)

	ctx := context.Background()
	_ = mgr.Initialize(ctx)

	// Recall with no stored data
	docs, err := mgr.Recall(ctx, "anything", 5)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}

	if len(docs) != 0 {
		t.Errorf("Expected 0 documents, got %d", len(docs))
	}
}
