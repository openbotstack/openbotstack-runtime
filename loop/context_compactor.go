package loop

import (
	"context"
	"fmt"
)

// ContextCompactor compresses turn history to keep the context window bounded.
type ContextCompactor interface {
	// Compact reduces the turn results slice while preserving essential context.
	Compact(ctx context.Context, turnResults []TurnResult) ([]TurnResult, error)
}

// =============================================================================
// DefaultContextCompactor
// =============================================================================

// DefaultContextCompactor implements truncation-based compaction.
// It retains the first turn (for initial context) and the last N-1 turns
// (for recency), dropping intermediate turns when the total exceeds maxRetainedTurns.
type DefaultContextCompactor struct {
	maxRetainedTurns int
}

// NewDefaultContextCompactor creates a compactor that retains at most maxRetained turns.
func NewDefaultContextCompactor(maxRetained int) *DefaultContextCompactor {
	if maxRetained < 1 {
		maxRetained = 1
	}
	return &DefaultContextCompactor{maxRetainedTurns: maxRetained}
}

// Compact implements ContextCompactor.
func (c *DefaultContextCompactor) Compact(ctx context.Context, turnResults []TurnResult) ([]TurnResult, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("context_compactor: %w", ctx.Err())
	}

	if len(turnResults) <= c.maxRetainedTurns {
		// Nothing to compact — return a defensive copy
		result := make([]TurnResult, len(turnResults))
		copy(result, turnResults)
		return result, nil
	}

	result := make([]TurnResult, 0, c.maxRetainedTurns)

	if c.maxRetainedTurns == 1 {
		// Special case: only keep the last element
		result = append(result, turnResults[len(turnResults)-1])
		return result, nil
	}

	// Keep the first element for initial context
	result = append(result, turnResults[0])

	// Keep the last (maxRetainedTurns - 1) elements for recency
	tailStart := len(turnResults) - (c.maxRetainedTurns - 1)
	result = append(result, turnResults[tailStart:]...)

	return result, nil
}

// =============================================================================
// NoOpCompactor
// =============================================================================

// NoOpCompactor retains all turn results without compaction.
// Suitable for testing or workloads with few turns.
type NoOpCompactor struct{}

// Compact implements ContextCompactor by returning a copy of the input unchanged.
func (c *NoOpCompactor) Compact(_ context.Context, turnResults []TurnResult) ([]TurnResult, error) {
	result := make([]TurnResult, len(turnResults))
	copy(result, turnResults)
	return result, nil
}
