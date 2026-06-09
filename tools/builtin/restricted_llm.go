package builtin

import (
	"context"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// restrictedLLMAccess enforces caps on LLM calls from builtin tools.
type restrictedLLMAccess struct {
	maxTokens  int
	timeout    time.Duration
	generateFn func(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error)
}

func (a *restrictedLLMAccess) Generate(ctx context.Context, req aitypes.LLMRequest) (*aitypes.LLMResponse, error) {
	// Enforce max tokens cap.
	if req.MaxTokens <= 0 || req.MaxTokens > a.maxTokens {
		req.MaxTokens = a.maxTokens
	}

	// Enforce timeout.
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	resp, err := a.generateFn(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
