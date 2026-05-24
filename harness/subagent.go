package harness

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// SubAgentConfig configures a SubAgent execution context.
type SubAgentConfig struct {
	Plan        *execution.ExecutionPlan
	MaxSteps    int
	Timeout     time.Duration               `json:"timeout,omitempty"`
	Permissions *execution.PermissionConfig
}

// SubAgentResult captures the output of a SubAgent execution.
type SubAgentResult struct {
	Output   any
	Error    error
	Duration time.Duration
	StepsRun int
}

// SubAgent executes a plan in an isolated execution context.
type SubAgent struct {
	config  SubAgentConfig
	harness *ExecutionHarness
}

// NewSubAgent creates a SubAgent with its own isolated ExecutionContext.
func NewSubAgent(config SubAgentConfig, harness *ExecutionHarness) *SubAgent {
	return &SubAgent{config: config, harness: harness}
}

// Run executes the SubAgent's plan in isolation.
// Returns only the final result, not intermediate step outputs.
func (sa *SubAgent) Run(ctx context.Context, parentEC *execution.ExecutionContext) (*SubAgentResult, error) {
	if sa.config.Plan == nil {
		return nil, fmt.Errorf("subagent: no plan configured")
	}
	if !sa.config.Plan.IsFrozen() {
		return nil, fmt.Errorf("subagent: plan must be frozen before execution")
	}

	// Create isolated ExecutionContext — no shared history
	isolatedEC := execution.NewExecutionContext(
		ctx,
		parentEC.RequestID+"-sub",
		parentEC.AssistantID,
		parentEC.SessionID,
		parentEC.TenantID,
		parentEC.UserID,
	)

	result, err := sa.harness.Run(ctx, sa.config.Plan, isolatedEC)
	if err != nil {
		return &SubAgentResult{Error: err}, err
	}

	// Extract only the final output
	var output any
	if len(result.StepResults) > 0 {
		output = result.StepResults[len(result.StepResults)-1].Output
	}

	return &SubAgentResult{
		Output:   output,
		Duration: result.Duration,
		StepsRun: result.StepsExecuted,
	}, nil
}

// RunParallel executes multiple SubAgents concurrently with shared
// deadline and cancellation. Bounded by a semaphore of size maxConcurrency.
func RunParallel(ctx context.Context, subs []*SubAgent, parentEC *execution.ExecutionContext, maxConcurrency int) ([]*SubAgentResult, error) {
	if len(subs) == 0 {
		return nil, nil
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 3
	}

	// Shared cancellation: if any SubAgent fails fast, cancel siblings
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, maxConcurrency)
	results := make([]*SubAgentResult, len(subs))
	errs := make([]error, len(subs))

	var wg sync.WaitGroup
	for i, sub := range subs {
		wg.Add(1)
		go func(idx int, s *SubAgent) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			result, err := s.Run(ctx, parentEC)
			results[idx] = result
			errs[idx] = err

			if err != nil {
				slog.WarnContext(ctx, "subagent failed", "index", idx, "error", err)
				// Cancel siblings on failure
				cancel()
			}
		}(i, sub)
	}
	wg.Wait()

	// Collect first error
	for _, err := range errs {
		if err != nil && ctx.Err() != nil {
			return results, err
		}
	}

	return results, nil
}
