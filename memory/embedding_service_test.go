package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/openbotstack/openbotstack-core/ai"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/control/skills"
)

// mockEmbedProvider is a mock provider that supports CapEmbedding.
type mockEmbedProvider struct {
	embeddings [][]float32
	err        error
}

func (m *mockEmbedProvider) ID() string { return "mock/embed" }

func (m *mockEmbedProvider) Capabilities() []skills.CapabilityType {
	return []skills.CapabilityType{skills.CapEmbedding}
}

func (m *mockEmbedProvider) Generate(_ context.Context, _ skills.GenerateRequest) (*skills.GenerateResponse, error) {
	return nil, ai.ErrCapabilityNotSupported
}

func (m *mockEmbedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	// Respect context cancellation (mirrors real provider behavior)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if m.err != nil {
		return nil, m.err
	}
	// Return a deterministic embedding for each text
	results := make([][]float32, len(texts))
	for i := range texts {
		if i < len(m.embeddings) {
			results[i] = m.embeddings[i]
		} else {
			results[i] = []float32{0.1, 0.2, 0.3}
		}
	}
	return results, nil
}

// mockRouter routes to the mockEmbedProvider.
type mockRouter struct {
	provider providers.ModelProvider
}

func (r *mockRouter) Route(requirements []skills.CapabilityType, _ skills.ModelConstraints) (providers.ModelProvider, error) {
	if r.provider == nil {
		return nil, ai.ErrNoMatchingProvider
	}
	caps := r.provider.Capabilities()
	capSet := make(map[skills.CapabilityType]bool)
	for _, c := range caps {
		capSet[c] = true
	}
	for _, req := range requirements {
		if !capSet[req] {
			return nil, fmt.Errorf("missing capability: %s", req)
		}
	}
	return r.provider, nil
}

func (r *mockRouter) Register(_ providers.ModelProvider) error { return nil }
func (r *mockRouter) List() []string {
	if r.provider == nil {
		return nil
	}
	return []string{r.provider.ID()}
}

func TestEmbeddingService_Embed_Single(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.5, 0.6, 0.7}},
		},
	}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 3)

	vec, err := svc.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(vec))
	}
	if vec[0] != 0.5 || vec[1] != 0.6 || vec[2] != 0.7 {
		t.Errorf("unexpected embedding: %v", vec)
	}
}

func TestEmbeddingService_EmbedBatch(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{
				{0.1, 0.2},
				{0.3, 0.4},
			},
		},
	}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 2)

	vecs, err := svc.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 results, got %d", len(vecs))
	}
	if vecs[0][0] != 0.1 || vecs[1][0] != 0.3 {
		t.Errorf("unexpected embeddings: %v", vecs)
	}
}

func TestEmbeddingService_Embed_Empty(t *testing.T) {
	router := &mockRouter{provider: &mockEmbedProvider{}}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 3)

	vecs, err := svc.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch(nil) should not error: %v", err)
	}
	if vecs != nil {
		t.Errorf("expected nil for empty input, got %v", vecs)
	}
}

func TestEmbeddingService_NoProvider(t *testing.T) {
	router := &mockRouter{provider: nil} // no provider
	svc := NewEmbeddingService(router, "text-embedding-3-small", 3)

	_, err := svc.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error when no provider available")
	}
}

func TestEmbeddingService_ProviderError(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			err: fmt.Errorf("API error"),
		},
	}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 3)

	_, err := svc.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from provider")
	}
}

func TestNewEmbeddingService(t *testing.T) {
	svc := NewEmbeddingService(nil, "text-embedding-3-small", 512)
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if svc.model != "text-embedding-3-small" {
		t.Errorf("expected model text-embedding-3-small, got %q", svc.model)
	}
	if svc.dimensions != 512 {
		t.Errorf("expected dimensions 512, got %d", svc.dimensions)
	}
}

func TestEmbeddingService_ContextCancellation(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2}},
		},
	}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 2)

	// Cancel context before calling
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Embed(ctx, "test")
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}

func TestEmbeddingService_EmbedBatchContextCancellation(t *testing.T) {
	router := &mockRouter{
		provider: &mockEmbedProvider{
			embeddings: [][]float32{{0.1, 0.2}},
		},
	}
	svc := NewEmbeddingService(router, "text-embedding-3-small", 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.EmbedBatch(ctx, []string{"test"})
	if err == nil {
		t.Error("expected error with cancelled context for EmbedBatch")
	}
}
