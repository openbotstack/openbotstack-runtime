package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/ai/types"
)

// RuntimeLLMAccess connects builtin tools to the real ModelRouter
// with enforced constraints.
// Implements types.LLMAccess directly — no type conversion needed.
type RuntimeLLMAccess struct {
	router    *router.DefaultRouter
	maxTokens int
	timeout   time.Duration
}

func NewRuntimeLLMAccess(r *router.DefaultRouter, maxTokens int, timeout time.Duration) *RuntimeLLMAccess {
	return &RuntimeLLMAccess{router: r, maxTokens: maxTokens, timeout: timeout}
}

func (a *RuntimeLLMAccess) Generate(ctx context.Context, req types.LLMRequest) (*types.LLMResponse, error) {
	if a.router == nil {
		return nil, fmt.Errorf("vision: model router not available")
	}

	// Enforce max tokens cap.
	maxTokens := req.MaxTokens
	if maxTokens <= 0 || maxTokens > a.maxTokens {
		maxTokens = a.maxTokens
	}

	// Enforce timeout.
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Route to vision-capable model.
	provider, err := a.router.Route([]types.CapabilityType{types.CapVision}, types.ModelConstraints{})
	if err != nil {
		return nil, fmt.Errorf("vision: no vision-capable model available: %w", err)
	}

	// Build the provider request — Contents are already types.ContentBlock.
	messages := []types.Message{
		{Role: "system", Contents: []types.ContentBlock{types.NewTextBlock(req.SystemPrompt)}},
		{Role: "user", Contents: req.Contents},
	}

	genReq := types.GenerateRequest{
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}

	// Retry with exponential backoff for transient errors (429, 500, 502, 503).
	// maxRetries=3 means 3 total attempts (initial + 2 retries).
	const maxAttempts = 3
	var resp *types.GenerateResponse
	start := time.Now()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err = provider.Generate(ctx, genReq)
		if err == nil {
			break
		}
		if !isRetryableError(err) || attempt == maxAttempts-1 {
			return nil, fmt.Errorf("vision: LLM generation failed: %w", err)
		}
		backoff := time.Duration(attempt+1) * 2 * time.Second
		slog.Warn("vision: retryable error, backing off",
			"attempt", attempt+1, "backoff", backoff, "error", err)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, fmt.Errorf("vision: context cancelled during retry: %w", ctx.Err())
		}
	}

	modelName := extractModelName(provider)
	slog.Info("vision: analysis complete",
		"model", modelName,
		"tokens", resp.Usage.TotalTokens,
		"latency", time.Since(start),
	)

	return &types.LLMResponse{
		Content:   resp.Content,
		Usage:     resp.Usage,
		ModelUsed: modelName,
		Latency:   resp.Latency,
	}, nil
}

// isRetryableError checks if the error is transient and worth retrying.
func isRetryableError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "RateLimit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "500") ||
		strings.Contains(msg, "502") ||
		strings.Contains(msg, "503") ||
		strings.Contains(msg, "ServiceUnavailable") ||
		strings.Contains(msg, "Internal Server Error")
}

func extractModelName(p providers.ModelProvider) string {
	if sp, ok := p.(interface{ ModelName() string }); ok {
		return sp.ModelName()
	}
	return "unknown"
}
