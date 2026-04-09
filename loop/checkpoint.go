package loop

import (
	"context"
	"fmt"
)

// Checkpoint persists intermediate state after each task execution in the outer loop.
type Checkpoint interface {
	Save(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error
}

// PolicyCheckpoint enforces governance gates between task executions in the outer loop.
type PolicyCheckpoint interface {
	Check(ctx context.Context, taskIndex int, metrics *LoopMetrics) error
}

// =============================================================================
// NoOpCheckpoint
// =============================================================================

// NoOpCheckpoint is a Checkpoint that does nothing. Suitable for testing.
type NoOpCheckpoint struct{}

// Save implements Checkpoint as a no-op.
func (c *NoOpCheckpoint) Save(_ context.Context, _ int, _ *TaskResult, _ *LoopMetrics) error {
	return nil
}

// =============================================================================
// NoOpPolicyCheckpoint
// =============================================================================

// NoOpPolicyCheckpoint is a PolicyCheckpoint that always allows continuation.
type NoOpPolicyCheckpoint struct{}

// Check implements PolicyCheckpoint as a no-op.
func (c *NoOpPolicyCheckpoint) Check(_ context.Context, _ int, _ *LoopMetrics) error {
	return nil
}

// =============================================================================
// CompositeCheckpoint
// =============================================================================

// CompositeCheckpoint chains multiple Checkpoint implementations.
// Executes in order; stops and returns the first error encountered.
type CompositeCheckpoint struct {
	checkpoints []Checkpoint
}

// NewCompositeCheckpoint creates a composite from the given checkpoints.
func NewCompositeCheckpoint(cps ...Checkpoint) *CompositeCheckpoint {
	return &CompositeCheckpoint{checkpoints: cps}
}

// Save implements Checkpoint by calling each checkpoint in sequence.
func (c *CompositeCheckpoint) Save(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
	for i, cp := range c.checkpoints {
		if err := cp.Save(ctx, taskIndex, taskResult, metrics); err != nil {
			return fmt.Errorf("checkpoint[%d]: %w", i, err)
		}
	}
	return nil
}
