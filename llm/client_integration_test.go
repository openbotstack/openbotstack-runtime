//go:build integration
// +build integration

// Package llm provides integration tests for real LLM providers.
// Run with: go test -tags=integration ./llm/...
package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestModelScopeIntegration(t *testing.T) {
	apiKey := os.Getenv("MODELSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("MODELSCOPE_API_KEY not set, skipping integration test")
	}

	client := NewClient(
		"https://api-inference.modelscope.cn/v1",
		apiKey,
		"qwen/Qwen2.5-Coder-32B-Instruct",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Generate(ctx, "Say 'Hello from ModelScope' and nothing else")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	t.Logf("Response: %s", resp)

	if len(resp) == 0 {
		t.Error("Empty response")
	}
}

func TestModelScopeSentimentAnalysis(t *testing.T) {
	apiKey := os.Getenv("MODELSCOPE_API_KEY")
	if apiKey == "" {
		t.Skip("MODELSCOPE_API_KEY not set, skipping integration test")
	}

	client := NewClient(
		"https://api-inference.modelscope.cn/v1",
		apiKey,
		"qwen/Qwen2.5-Coder-32B-Instruct",
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := `Analyze the sentiment of this text and respond with ONLY one word: positive, negative, or neutral.

Text: "I absolutely love this product! It's amazing and exceeded all my expectations."

Sentiment:`

	resp, err := client.Generate(ctx, prompt)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	t.Logf("Sentiment response: %s", resp)

	// Should contain positive sentiment
	if len(resp) == 0 {
		t.Error("Empty response")
	}
}
