package adapters

import (
	"context"
	"fmt"
	"strings"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/control/skills"
)

// LLMTextGenerator adapts a ModelRouter to the executor.TextGenerator interface.
type LLMTextGenerator struct {
	Router providers.ModelRouter
}

func (g *LLMTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	provider, err := g.Router.Route([]skills.CapabilityType{skills.CapTextGeneration}, skills.ModelConstraints{})
	if err != nil {
		return "", fmt.Errorf("no text generation provider: %w", err)
	}
	resp, err := provider.Generate(ctx, skills.GenerateRequest{
		Messages: []skills.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// ParseLogLevel converts a log level string to slog.Level value.
func ParseLogLevel(s string) int {
	switch strings.ToLower(s) {
	case "debug":
		return -4
	case "warn":
		return 4
	case "error":
		return 8
	default:
		return 0
	}
}
