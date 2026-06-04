package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/ai/types"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
)

// RuntimeLLMAccess connects builtin tools to the real ModelRouter
// with enforced constraints.
type RuntimeLLMAccess struct {
	router    *router.DefaultRouter
	maxTokens int
	timeout   time.Duration
}

func NewRuntimeLLMAccess(r *router.DefaultRouter, maxTokens int, timeout time.Duration) *RuntimeLLMAccess {
	return &RuntimeLLMAccess{router: r, maxTokens: maxTokens, timeout: timeout}
}

func (a *RuntimeLLMAccess) Generate(ctx context.Context, req builtintools.LLMRequest) (*builtintools.LLMResponse, error) {
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

	// Convert builtin Contents → types.Message for the provider.
	var contents []types.ContentBlock
	for _, c := range req.Contents {
		contents = append(contents, types.ContentBlock{
			Type:     c.Type,
			Text:     c.Text,
			ImageURL: c.ImageURL,
		})
	}

	messages := []types.Message{
		{Role: "system", Contents: []types.ContentBlock{types.NewTextBlock(req.SystemPrompt)}},
		{Role: "user", Contents: contents},
	}

	genReq := types.GenerateRequest{
		Messages:   messages,
		MaxTokens:  maxTokens,
		Temperature: req.Temperature,
	}

	start := time.Now()
	resp, err := provider.Generate(ctx, genReq)
	if err != nil {
		return nil, fmt.Errorf("vision: LLM generation failed: %w", err)
	}

	modelName := extractModelName(provider)
	slog.Info("vision: analysis complete",
		"model", modelName,
		"tokens", resp.Usage.TotalTokens,
		"latency", time.Since(start),
	)

	return &builtintools.LLMResponse{
		Content:   resp.Content,
		Usage:     builtintools.TokenUsage{PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens, TotalTokens: resp.Usage.TotalTokens},
		ModelUsed: modelName,
		Latency:   resp.Latency,
	}, nil
}

func extractModelName(p providers.ModelProvider) string {
	if sp, ok := p.(interface{ ModelName() string }); ok {
		return sp.ModelName()
	}
	return "unknown"
}
