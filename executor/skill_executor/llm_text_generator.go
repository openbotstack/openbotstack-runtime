package skill_executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/types"
)

// LLMTextGenerator adapts a ModelRouter to the TextGenerator/StreamingTextGenerator interfaces.
type LLMTextGenerator struct {
	Router providers.ModelRouter
}

func (g *LLMTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	provider, err := g.Router.Route([]types.CapabilityType{types.CapTextGeneration}, types.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("no text generation provider: %w", err)
	}
	resp, err := provider.Generate(ctx, types.GenerateRequest{
		Messages: []types.Message{
			types.NewTextMessage("user", prompt),
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// GenerateStreamText performs token-level streaming generation.
// It uses the provider's GenerateStream when available, falling back to Generate.
func (g *LLMTextGenerator) GenerateStreamText(ctx context.Context, prompt string, tokenFn func(string)) (string, error) {
	provider, err := g.Router.Route([]types.CapabilityType{types.CapTextGeneration}, types.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("no text generation provider: %w", err)
	}

	req := types.GenerateRequest{
		Messages: []types.Message{
			types.NewTextMessage("user", prompt),
		},
	}

	// Try streaming provider first
	if sp, ok := provider.(providers.StreamingModelProvider); ok {
		ch, err := sp.GenerateStream(ctx, req)
		if err != nil {
			return "", err
		}
		var sb strings.Builder
		for chunk := range ch {
			if chunk.Error != nil {
				return sb.String(), chunk.Error
			}
			if chunk.Content != "" {
				tokenFn(chunk.Content)
				sb.WriteString(chunk.Content)
			}
		}
		return sb.String(), nil
	}

	// Fallback: non-streaming, send full output as one token
	resp, err := provider.Generate(ctx, req)
	if err != nil {
		return "", err
	}
	if resp.Content != "" {
		tokenFn(resp.Content)
	}
	return resp.Content, nil
}
