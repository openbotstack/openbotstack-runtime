package loop

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// StopReason enum tests
// =============================================================================

func TestStopReason_AllValuesAreDefined(t *testing.T) {
	reasons := []StopReason{
		StopReasonMaxTurns,
		StopReasonMaxToolCalls,
		StopReasonMaxRuntime,
		StopReasonMaxWorkflowSteps,
		StopReasonMaxSessionRuntime,
		StopReasonGoalAchieved,
		StopReasonPlannerStopped,
		StopReasonContextCanceled,
		StopReasonError,
	}

	for _, r := range reasons {
		if r == "" {
			t.Error("StopReason value must not be empty")
		}
	}

	if len(reasons) != 9 {
		t.Errorf("expected 9 StopReason values, got %d", len(reasons))
	}
}

func TestStopReason_Uniqueness(t *testing.T) {
	reasons := []StopReason{
		StopReasonMaxTurns, StopReasonMaxToolCalls, StopReasonMaxRuntime,
		StopReasonMaxWorkflowSteps, StopReasonMaxSessionRuntime,
		StopReasonGoalAchieved, StopReasonPlannerStopped,
		StopReasonContextCanceled, StopReasonError,
	}
	seen := make(map[StopReason]bool)
	for _, r := range reasons {
		if seen[r] {
			t.Errorf("duplicate StopReason value: %s", r)
		}
		seen[r] = true
	}
}

// =============================================================================
// StopCondition tests
// =============================================================================

func TestStopCondition_ZeroValue(t *testing.T) {
	var sc StopCondition
	if sc.Stopped {
		t.Error("zero StopCondition.Stopped must be false")
	}
	if sc.Reason != "" {
		t.Error("zero StopCondition.Reason must be empty")
	}
	if sc.Detail != "" {
		t.Error("zero StopCondition.Detail must be empty")
	}
}

// =============================================================================
// InnerStopEvaluator tests
// =============================================================================

func TestNewInnerStopEvaluator(t *testing.T) {
	cfg := DefaultInnerConfig()
	evaluator := NewInnerStopEvaluator(cfg)
	if evaluator == nil {
		t.Fatal("NewInnerStopEvaluator returned nil")
	}
}

func TestInnerStopEvaluator_NotStopped_WhenWithinBounds(t *testing.T) {
	cfg := DefaultInnerConfig()
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(1, 1, startTime, false, ctx)
	if result.Stopped {
		t.Errorf("should not stop when within bounds, got reason: %s", result.Reason)
	}
}

func TestInnerStopEvaluator_StopsAtMaxTurns(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 3, MaxToolCalls: 20, MaxTurnRuntime: 15 * time.Second}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(3, 0, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop at max turns")
	}
	if result.Reason != StopReasonMaxTurns {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxTurns)
	}
}

func TestInnerStopEvaluator_StopsExceedingMaxTurns(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 3, MaxToolCalls: 20, MaxTurnRuntime: 15 * time.Second}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(5, 0, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop when exceeding max turns")
	}
	if result.Reason != StopReasonMaxTurns {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxTurns)
	}
}

func TestInnerStopEvaluator_StopsAtMaxToolCalls(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 8, MaxToolCalls: 5, MaxTurnRuntime: 15 * time.Second}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(1, 5, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop at max tool calls")
	}
	if result.Reason != StopReasonMaxToolCalls {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxToolCalls)
	}
}

func TestInnerStopEvaluator_StopsExceedingMaxToolCalls(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 8, MaxToolCalls: 5, MaxTurnRuntime: 15 * time.Second}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(1, 10, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop when exceeding max tool calls")
	}
	if result.Reason != StopReasonMaxToolCalls {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxToolCalls)
	}
}

func TestInnerStopEvaluator_StopsOnMaxRuntime(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 8, MaxToolCalls: 20, MaxTurnRuntime: 100 * time.Millisecond}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now().Add(-200 * time.Millisecond) // 200ms ago

	result := eval.Evaluate(1, 0, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop when runtime exceeded")
	}
	if result.Reason != StopReasonMaxRuntime {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxRuntime)
	}
}

func TestInnerStopEvaluator_StopsOnPlannerStopped(t *testing.T) {
	cfg := DefaultInnerConfig()
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(1, 0, startTime, true, ctx)
	if !result.Stopped {
		t.Fatal("should stop when planner stopped")
	}
	if result.Reason != StopReasonPlannerStopped {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonPlannerStopped)
	}
}

func TestInnerStopEvaluator_StopsOnContextCanceled(t *testing.T) {
	cfg := DefaultInnerConfig()
	eval := NewInnerStopEvaluator(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	startTime := time.Now()

	result := eval.Evaluate(1, 0, startTime, false, ctx)
	if !result.Stopped {
		t.Fatal("should stop when context canceled")
	}
	if result.Reason != StopReasonContextCanceled {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonContextCanceled)
	}
}

func TestInnerStopEvaluator_PriorityOrder_ContextFirst(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 1, MaxToolCalls: 1, MaxTurnRuntime: 1 * time.Nanosecond}
	eval := NewInnerStopEvaluator(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startTime := time.Now().Add(-time.Hour)

	// All conditions met, but context cancellation should be checked first
	result := eval.Evaluate(10, 10, startTime, true, ctx)
	if !result.Stopped {
		t.Fatal("should stop")
	}
	if result.Reason != StopReasonContextCanceled {
		t.Errorf("reason = %s, want %s (context should have priority)", result.Reason, StopReasonContextCanceled)
	}
}

func TestInnerStopEvaluator_BoundaryExactlyOneTurnBelow(t *testing.T) {
	cfg := InnerLoopConfig{MaxTurns: 5, MaxToolCalls: 20, MaxTurnRuntime: 15 * time.Second}
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(4, 0, startTime, false, ctx)
	if result.Stopped {
		t.Error("should NOT stop at turns=4 when max=5")
	}
}

func TestInnerStopEvaluator_ZeroTurnsZeroToolCalls(t *testing.T) {
	cfg := DefaultInnerConfig()
	eval := NewInnerStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(0, 0, startTime, false, ctx)
	if result.Stopped {
		t.Error("should not stop at zero turns and zero tool calls")
	}
}

// =============================================================================
// OuterStopEvaluator tests
// =============================================================================

func TestNewOuterStopEvaluator(t *testing.T) {
	cfg := DefaultOuterConfig()
	evaluator := NewOuterStopEvaluator(cfg)
	if evaluator == nil {
		t.Fatal("NewOuterStopEvaluator returned nil")
	}
}

func TestOuterStopEvaluator_NotStopped_WhenWithinBounds(t *testing.T) {
	cfg := DefaultOuterConfig()
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(1, startTime, ctx)
	if result.Stopped {
		t.Errorf("should not stop when within bounds, got reason: %s", result.Reason)
	}
}

func TestOuterStopEvaluator_StopsAtMaxWorkflowSteps(t *testing.T) {
	cfg := OuterLoopConfig{MaxWorkflowSteps: 3, MaxSessionRuntime: 30 * time.Second}
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(3, startTime, ctx)
	if !result.Stopped {
		t.Fatal("should stop at max workflow steps")
	}
	if result.Reason != StopReasonMaxWorkflowSteps {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxWorkflowSteps)
	}
}

func TestOuterStopEvaluator_StopsExceedingMaxWorkflowSteps(t *testing.T) {
	cfg := OuterLoopConfig{MaxWorkflowSteps: 3, MaxSessionRuntime: 30 * time.Second}
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(5, startTime, ctx)
	if !result.Stopped {
		t.Fatal("should stop when exceeding max workflow steps")
	}
}

func TestOuterStopEvaluator_StopsOnMaxSessionRuntime(t *testing.T) {
	cfg := OuterLoopConfig{MaxWorkflowSteps: 5, MaxSessionRuntime: 100 * time.Millisecond}
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now().Add(-200 * time.Millisecond)

	result := eval.Evaluate(1, startTime, ctx)
	if !result.Stopped {
		t.Fatal("should stop when session runtime exceeded")
	}
	if result.Reason != StopReasonMaxSessionRuntime {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonMaxSessionRuntime)
	}
}

func TestOuterStopEvaluator_StopsOnContextCanceled(t *testing.T) {
	cfg := DefaultOuterConfig()
	eval := NewOuterStopEvaluator(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startTime := time.Now()

	result := eval.Evaluate(1, startTime, ctx)
	if !result.Stopped {
		t.Fatal("should stop when context canceled")
	}
	if result.Reason != StopReasonContextCanceled {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonContextCanceled)
	}
}

func TestOuterStopEvaluator_PriorityOrder_ContextFirst(t *testing.T) {
	cfg := OuterLoopConfig{MaxWorkflowSteps: 1, MaxSessionRuntime: 1 * time.Nanosecond}
	eval := NewOuterStopEvaluator(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startTime := time.Now().Add(-time.Hour)

	result := eval.Evaluate(10, startTime, ctx)
	if result.Reason != StopReasonContextCanceled {
		t.Errorf("reason = %s, want %s (context should have priority)", result.Reason, StopReasonContextCanceled)
	}
}

func TestOuterStopEvaluator_BoundaryOneBelowMax(t *testing.T) {
	cfg := OuterLoopConfig{MaxWorkflowSteps: 5, MaxSessionRuntime: 30 * time.Second}
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(4, startTime, ctx)
	if result.Stopped {
		t.Error("should NOT stop at steps=4 when max=5")
	}
}

func TestOuterStopEvaluator_ZeroSteps(t *testing.T) {
	cfg := DefaultOuterConfig()
	eval := NewOuterStopEvaluator(cfg)
	ctx := context.Background()
	startTime := time.Now()

	result := eval.Evaluate(0, startTime, ctx)
	if result.Stopped {
		t.Error("should not stop at zero steps")
	}
}

func TestOuterStopEvaluator_ContextDeadlineExceeded(t *testing.T) {
	cfg := DefaultOuterConfig()
	eval := NewOuterStopEvaluator(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond) // ensure deadline passed

	result := eval.Evaluate(1, time.Now(), ctx)
	if !result.Stopped {
		t.Fatal("should stop on deadline exceeded")
	}
	if result.Reason != StopReasonContextCanceled {
		t.Errorf("reason = %s, want %s", result.Reason, StopReasonContextCanceled)
	}
}
