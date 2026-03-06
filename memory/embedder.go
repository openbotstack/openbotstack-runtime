package memory

import (
	"context"
	"hash/fnv"
	"strings"
)

// Embedder generates vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
}

// LocalEmbedder is a stub embedder for V1 (deterministic hash-based).
// Replace with OpenAI/local model in production.
type LocalEmbedder struct {
	dimension int
}

// NewLocalEmbedder creates a stub embedder.
func NewLocalEmbedder() *LocalEmbedder {
	return &LocalEmbedder{
		dimension: 384,
	}
}

// Embed generates a deterministic embedding from text.
func (e *LocalEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Deterministic hash-based embedding (for testing only)
	h := fnv.New64a()
	h.Write([]byte(text))
	seed := h.Sum64()

	vec := make([]float32, e.dimension)
	for i := range vec {
		// Pseudo-random based on seed and position
		seed = seed*6364136223846793005 + 1442695040888963407
		vec[i] = float32(seed%1000) / 1000.0
	}

	return vec, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (e *LocalEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}

// ChatMessage represents a conversation message.
type ChatMessage struct {
	Role    string
	Content string
}

// Summarizer compresses conversation history.
type Summarizer interface {
	Summarize(ctx context.Context, messages []ChatMessage) (string, error)
	ShouldSummarize(messages []ChatMessage) bool
}

// LLMSummarizer uses an LLM to summarize conversations.
type LLMSummarizer struct {
	threshold int
}

// NewLLMSummarizer creates a summarizer.
func NewLLMSummarizer() *LLMSummarizer {
	return &LLMSummarizer{
		threshold: 10, // Summarize after 10 messages
	}
}

// Summarize compresses messages into a summary.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []ChatMessage) (string, error) {
	// Stub: concatenate key points
	var parts []string
	for _, m := range messages {
		if m.Role == "assistant" && len(m.Content) > 0 {
			// Take first 50 chars of each assistant response
			content := m.Content
			if len(content) > 50 {
				content = content[:50] + "..."
			}
			parts = append(parts, content)
		}
	}

	if len(parts) == 0 {
		return "No conversation to summarize.", nil
	}

	return "Summary: " + strings.Join(parts, " | "), nil
}

// ShouldSummarize returns true if conversation is long enough.
func (s *LLMSummarizer) ShouldSummarize(messages []ChatMessage) bool {
	return len(messages) >= s.threshold
}
