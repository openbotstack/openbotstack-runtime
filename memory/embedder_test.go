package memory_test

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestEmbedderCreate(t *testing.T) {
	e := memory.NewLocalEmbedder()
	if e == nil {
		t.Fatal("NewLocalEmbedder returned nil")
	}
}

func TestEmbedderEmbed(t *testing.T) {
	e := memory.NewLocalEmbedder()
	ctx := context.Background()

	vec, err := e.Embed(ctx, "Hello, world!")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(vec) != 384 {
		t.Errorf("Expected 384 dimensions, got %d", len(vec))
	}
}

func TestEmbedderEmbedBatch(t *testing.T) {
	e := memory.NewLocalEmbedder()
	ctx := context.Background()

	texts := []string{"Hello", "World", "Test"}
	vecs, err := e.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(vecs) != 3 {
		t.Errorf("Expected 3 embeddings, got %d", len(vecs))
	}
}

func TestSummarizerCreate(t *testing.T) {
	s := memory.NewLLMSummarizer()
	if s == nil {
		t.Fatal("NewLLMSummarizer returned nil")
	}
}

func TestSummarizerSummarize(t *testing.T) {
	s := memory.NewLLMSummarizer()
	ctx := context.Background()

	messages := []memory.ChatMessage{
		{Role: "user", Content: "What is the capital of France?"},
		{Role: "assistant", Content: "The capital of France is Paris."},
		{Role: "user", Content: "What is its population?"},
		{Role: "assistant", Content: "Paris has about 2.1 million people."},
	}

	summary, err := s.Summarize(ctx, messages)
	if err != nil {
		t.Fatalf("Summarize failed: %v", err)
	}

	if summary == "" {
		t.Error("Expected non-empty summary")
	}
}

func TestSummarizerShouldSummarize(t *testing.T) {
	s := memory.NewLLMSummarizer()

	// Few messages - no need
	fewMsgs := make([]memory.ChatMessage, 5)
	if s.ShouldSummarize(fewMsgs) {
		t.Error("Should not summarize few messages")
	}

	// Many messages - should summarize
	manyMsgs := make([]memory.ChatMessage, 20)
	if !s.ShouldSummarize(manyMsgs) {
		t.Error("Should summarize many messages")
	}
}
