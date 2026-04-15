package memory

import (
	"context"
	"fmt"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/control/skills"
)

// EmbeddingService generates vector embeddings via ModelProvider.
// It routes embedding requests through the ModelRouter to find a
// provider that supports the CapEmbedding capability.
type EmbeddingService struct {
	router     providers.ModelRouter
	model      string // e.g. "text-embedding-3-small"
	dimensions int    // e.g. 512
}

// NewEmbeddingService creates a new embedding service.
func NewEmbeddingService(router providers.ModelRouter, model string, dimensions int) *EmbeddingService {
	return &EmbeddingService{
		router:     router,
		model:      model,
		dimensions: dimensions,
	}
}

// Embed generates a vector embedding for a single text.
func (s *EmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := s.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("embedding service: empty result")
	}
	return results[0], nil
}

// EmbedBatch generates vector embeddings for multiple texts in a single API call.
func (s *EmbeddingService) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	provider, err := s.router.Route(
		[]skills.CapabilityType{skills.CapEmbedding},
		skills.ModelConstraints{},
	)
	if err != nil {
		return nil, fmt.Errorf("embedding service: no embedding provider available: %w", err)
	}

	return provider.Embed(ctx, texts)
}
