package builtin

import (
	"context"
	"testing"
	"time"
)

type mockProvider struct {
	generateFn func(ctx context.Context, messages []providerMessage) (*providerResponse, error)
}

type providerMessage struct {
	Role    string
	Content any
}

type providerResponse struct {
	Content   string
	ModelUsed string
	Tokens    int
}

func TestRestrictedLLMAccess_EnforcesMaxTokens(t *testing.T) {
	called := false
	access := &restrictedLLMAccess{
		maxTokens: 512,
		timeout:   30 * time.Second,
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			called = true
			if req.MaxTokens > 512 {
				t.Errorf("MaxTokens = %d, should be capped at 512", req.MaxTokens)
			}
			return &LLMResponse{
				Content:   "test response",
				ModelUsed: "test",
				Usage:     TokenUsage{TotalTokens: 50},
			}, nil
		},
	}

	resp, err := access.Generate(context.Background(), LLMRequest{
		Contents:  []ContentBlock{NewTextBlock("test")},
		MaxTokens: 9999,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("generate was not called")
	}
	if resp.Content != "test response" {
		t.Errorf("Content = %q", resp.Content)
	}
}

func TestRestrictedLLMAccess_DefaultMaxTokens(t *testing.T) {
	access := &restrictedLLMAccess{
		maxTokens: 2048,
		timeout:   30 * time.Second,
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			if req.MaxTokens != 2048 {
				t.Errorf("MaxTokens = %d, want default 2048", req.MaxTokens)
			}
			return &LLMResponse{Content: "ok"}, nil
		},
	}
	access.Generate(context.Background(), LLMRequest{
		Contents: []ContentBlock{NewTextBlock("test")},
	})
}

func TestRestrictedLLMAccess_EnforcesTimeout(t *testing.T) {
	access := &restrictedLLMAccess{
		maxTokens: 1024,
		timeout:   1 * time.Millisecond,
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	_, err := access.Generate(context.Background(), LLMRequest{
		Contents: []ContentBlock{NewTextBlock("test")},
	})
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestRestrictedLLMAccess_MultimodalPasses(t *testing.T) {
	var captured LLMRequest
	access := &restrictedLLMAccess{
		maxTokens: 2048,
		timeout:   30 * time.Second,
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			captured = req
			return &LLMResponse{Content: "a cat"}, nil
		},
	}

	_, err := access.Generate(context.Background(), LLMRequest{
		SystemPrompt: "You are a vision assistant.",
		Contents: []ContentBlock{
			NewTextBlock("What is this?"),
			NewImageBlock("https://example.com/cat.jpg"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(captured.Contents) != 2 {
		t.Errorf("Contents len = %d, want 2", len(captured.Contents))
	}
	if captured.Contents[1].ImageURL != "https://example.com/cat.jpg" {
		t.Errorf("ImageURL = %q", captured.Contents[1].ImageURL)
	}
}
