package harness

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- Phase 6: Failure Handling ---
//
// Test tool timeout, invalid response, partial execution, retry bounds,
// fail-fast, and fallback behavior.

// Failure 1: Tool timeout with retry — fails first 2, succeeds on 3rd
func TestFailure_RetryWithEventualSuccess(t *testing.T) {
	cfg := DefaultHarnessConfig()

	var attempts int32
	tr := &retryTestToolRunner{
		failUntil: 2, // fail first 2 attempts, succeed on 3rd
		attempts:  &attempts,
		successResult: "recovered",
	}

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "flaky-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}

	// The step should have been retried and eventually succeeded
	if result.StepResults[0].Error != nil {
		t.Errorf("step error = %v, want nil (recovered via retry)", result.StepResults[0].Error)
	}

	// Verify retry count
	if result.StepResults[0].Retries != 2 {
		t.Errorf("Retries = %d, want 2", result.StepResults[0].Retries)
	}
}

// Failure 2: Invalid response from tool
func TestFailure_InvalidToolResponse(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.err["bad-tool"] = fmt.Errorf("invalid response: malformed JSON")

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "bad-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "after-bad", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error at harness level: %v", err)
	}

	// Both steps should execute (no fail-fast by default)
	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
	}

	// First step has error
	if result.StepResults[0].Error == nil {
		t.Error("expected error from bad-tool")
	}

	// Second step should execute normally
	if result.StepResults[1].Error != nil {
		t.Errorf("after-bad step should succeed, got error: %v", result.StepResults[1].Error)
	}
}

// Failure 3: Partial execution with fail-fast=true
func TestFailure_PartialExecution_FailFast(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.result["step-2"] = "ok"
	tr.err["step-3"] = fmt.Errorf("permanent failure")

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       true,
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-4", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-5", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error for fail-fast")
	}

	// Should stop at step 3 (fail-fast)
	if result.StepsExecuted > 3 {
		t.Errorf("StepsExecuted = %d, want <= 3 (fail-fast at step 3)", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonFailFast {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonFailFast)
	}

	// Partial results preserved
	if len(result.StepResults) < 3 {
		t.Errorf("should have at least 3 step results (partial), got %d", len(result.StepResults))
	}
}

// Failure 3b: Partial execution with fail-fast=false (continues)
func TestFailure_PartialExecution_ContinueOnError(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.err["step-2"] = fmt.Errorf("transient error")
	tr.result["step-3"] = "ok"

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 steps should have been attempted
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}

	// Step 2 has error but execution continued
	if result.StepResults[0].Error != nil {
		t.Error("step-1 should succeed")
	}
	if result.StepResults[2].Error != nil {
		t.Error("step-3 should succeed despite step-2 failure")
	}
}

// Failure 4: Retry logic bounded — exactly MaxRetries attempts
func TestFailure_RetryBounded(t *testing.T) {
	cfg := DefaultHarnessConfig()

	var attempts int32
	tr := &retryTestToolRunner{
		failUntil: 999, // always fail
		attempts:  &attempts,
	}

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     2,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "always-fail", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	// Harness should not crash — error captured per-step
	if err != nil {
		t.Fatalf("unexpected error at harness level: %v", err)
	}

	// Step should have error
	if result.StepResults[0].Error == nil {
		t.Error("expected error from always-fail tool")
	}

	// Verify exactly MaxRetries attempts
	totalAttempts := atomic.LoadInt32(&attempts)
	if totalAttempts != 3 { // 1 initial + 2 retries
		t.Errorf("total attempts = %d, want 3 (1 initial + 2 retries)", totalAttempts)
	}

	// Execution time should be bounded
	maxExpectedTime := time.Duration(2+4) * time.Millisecond // 2 retries: 1ms + 2ms backoff + execution
	if result.Duration > maxExpectedTime+50*time.Millisecond {
		t.Errorf("Duration = %v, expected bounded by retries", result.Duration)
	}
}

// Failure 5: Fail-fast stops immediately
func TestFailure_FailFastStopsImmediately(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 10

	tr := newMockToolRunner()

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     0,
		InitialBackoff: 0,
		MaxBackoff:     0,
		FailFast:       true,
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	// 10-step plan, all steps fail
	steps := make([]execution.ExecutionStep, 10)
	for i := range steps {
		name := fmt.Sprintf("fail-step-%d", i)
		steps[i] = execution.ExecutionStep{
			Name:      name,
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{},
		}
		tr.err[name] = fmt.Errorf("fatal error")
	}
	plan := makeFrozenPlan(steps...)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error for fail-fast")
	}

	if result.StopCondition.Reason != StopReasonFailFast {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonFailFast)
	}
	// Only 1 step executed (fail-fast stopped after first failure)
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

// Failure 6: Fallback tool execution
func TestFailure_FallbackExecution(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.err["primary-tool"] = fmt.Errorf("primary failed")
	tr.result["fallback-tool"] = "fallback-success"

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
		FallbackTool:   "fallback-tool",
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetFailureHandler(fh)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "primary-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step should have been executed (with fallback)
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}

	// Verify fallback was triggered (result has Fallback=true)
	if result.StepResults[0].Fallback != true {
		t.Error("expected Fallback=true in step result")
	}
}

// --- Failure test helpers ---

// retryTestToolRunner fails for the first N attempts, then succeeds.
type retryTestToolRunner struct {
	failUntil     int32
	attempts      *int32
	successResult string
}

func (r *retryTestToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	curr := atomic.AddInt32(r.attempts, 1)
	if curr <= r.failUntil {
		return nil, fmt.Errorf("attempt %d: transient failure", curr)
	}
	return &execution.StepResult{StepName: toolName, Output: r.successResult}, nil
}
func (r *retryTestToolRunner) callCount() int { return int(atomic.LoadInt32(r.attempts)) }
