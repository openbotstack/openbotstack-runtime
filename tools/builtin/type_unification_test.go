package builtin

import (
	"context"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// TestLLMAccessTypeUnification verifies that the builtin package uses
// core/ai/types directly — no dual definitions, no manual conversion.
func TestLLMAccessTypeUnification(t *testing.T) {
	// Verify types.LLMAccess is the same interface used by builtin tools.
	// If this fails to compile, the unification is incomplete.
	var _ aitypes.LLMAccess = aitypes.LLMAccessFunc(
		func(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error) {
			return &aitypes.LLMResponse{Content: "ok"}, nil
		},
	)

	// Verify restrictedLLMAccess satisfies types.LLMAccess.
	restricted := &restrictedLLMAccess{
		maxTokens: 1024,
		timeout:   5 * time.Second,
		generateFn: func(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error) {
			return &aitypes.LLMResponse{Content: "test"}, nil
		},
	}
	var _ aitypes.LLMAccess = restricted

	// Verify VisionAnalyzeTool uses types.LLMRequest and types.ContentBlock.
	tool := &VisionAnalyzeTool{}
	var captured aitypes.LLMRequest
	tool.SetLLMAccess(aitypes.LLMAccessFunc(
		func(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error) {
			captured = req
			return &aitypes.LLMResponse{
				Content:   "ok",
				Usage:     aitypes.TokenUsage{TotalTokens: 5},
				ModelUsed: "test",
				Latency:   10 * time.Millisecond,
			}, nil
		},
	))

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_url":   "https://example.com/img.png",
		"instruction": "describe it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the request was built with types.ContentBlock.
	if len(captured.Contents) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(captured.Contents))
	}
	if captured.Contents[0] != aitypes.NewTextBlock("describe it") {
		t.Errorf("first block = %v, want text block", captured.Contents[0])
	}
	if captured.Contents[1] != aitypes.NewImageBlock("https://example.com/img.png") {
		t.Errorf("second block = %v, want image block", captured.Contents[1])
	}
}

// TestBuiltinToolRunnerAcceptsTypesLLMAccess verifies that BuiltinToolRunner
// injects types.LLMAccess into LLMAwareTools.
func TestBuiltinToolRunnerAcceptsTypesLLMAccess(t *testing.T) {
	runner := NewBuiltinToolRunner()

	var injected aitypes.LLMAccess = aitypes.LLMAccessFunc(
		func(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error) {
			return &aitypes.LLMResponse{Content: "test"}, nil
		},
	)

	// This must compile: SetLLMAccess accepts types.LLMAccess.
	runner.SetLLMAccess(injected)
}
