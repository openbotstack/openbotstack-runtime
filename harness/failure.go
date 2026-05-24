package harness

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// maxRetryCap is a harness-level hard limit on retries per step.
// Individual step policies may request higher, but the harness enforces this cap.
const maxRetryCap = 3

// FailureHandler implements per-step retry and fallback behavior.
type FailureHandler struct {
	defaultPolicy execution.RetryPolicy
	stepPolicies  map[string]execution.RetryPolicy
}

// NewFailureHandler creates a failure handler with the default policy.
func NewFailureHandler(defaultPolicy execution.RetryPolicy) *FailureHandler {
	return &FailureHandler{
		defaultPolicy: defaultPolicy,
		stepPolicies:  make(map[string]execution.RetryPolicy),
	}
}

// SetStepPolicy overrides the retry policy for a specific step.
func (fh *FailureHandler) SetStepPolicy(stepID string, policy execution.RetryPolicy) {
	fh.stepPolicies[stepID] = policy
}

func (fh *FailureHandler) policyForStep(stepID string) execution.RetryPolicy {
	if p, ok := fh.stepPolicies[stepID]; ok {
		return p
	}
	return fh.defaultPolicy
}

// FallbackToolFor returns the configured fallback tool name for a step, or "" if none.
func (fh *FailureHandler) FallbackToolFor(stepID string) string {
	return fh.policyForStep(stepID).FallbackTool
}

// Handle evaluates a step failure and retries or falls back.
// executor is called for each retry attempt.
func (fh *FailureHandler) Handle(
	ctx context.Context,
	step *execution.ExecutionStep,
	initialErr error,
	executor func() (*execution.StepResult, error),
) (*execution.StepResult, error) {
	policy := fh.policyForStep(step.StepID)

	// Harness-level hard cap: never retry more than maxRetryCap times.
	effectiveMaxRetries := policy.MaxRetries
	if effectiveMaxRetries > maxRetryCap {
		effectiveMaxRetries = maxRetryCap
	}

	for attempt := 1; attempt <= effectiveMaxRetries; attempt++ {
		backoff := fh.backoff(attempt, policy)

		slog.WarnContext(ctx, "step failed, retrying",
			"step", step.Name,
			"attempt", attempt,
			"max_retries", effectiveMaxRetries,
			"backoff", backoff,
			"error", initialErr,
		)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		result, err := executor()
		if err == nil {
			result.Retries = attempt
			return result, nil
		}
		initialErr = err
	}

	// All retries exhausted
	if policy.FallbackTool != "" {
		slog.InfoContext(ctx, "retries exhausted, executing fallback",
			"step", step.Name,
			"fallback", policy.FallbackTool,
		)
		// Fallback execution would be handled by the harness calling
		// the fallback tool separately. Here we signal that fallback is needed.
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type: string(step.Type),
			Error:    initialErr,
			Retries:  effectiveMaxRetries,
			Fallback: true,
		}, nil
	}

	if policy.FailFast {
		return nil, fmt.Errorf("step %q failed after %d retries: %w (fail-fast)", step.Name, effectiveMaxRetries, initialErr)
	}

	return &execution.StepResult{
		StepID:   step.StepID,
		StepName: step.Name,
		Type: string(step.Type),
		Error:    initialErr,
		Retries:  effectiveMaxRetries,
	}, nil
}

func (fh *FailureHandler) backoff(attempt int, policy execution.RetryPolicy) time.Duration {
	d := policy.InitialBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d > policy.MaxBackoff {
			d = policy.MaxBackoff
		}
	}
	return d
}
