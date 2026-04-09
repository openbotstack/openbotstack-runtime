package loop

import (
	"context"
	"fmt"
	"time"
)

// =============================================================================
// StopReason
// =============================================================================

// StopReason identifies why a loop stopped.
type StopReason string

const (
	StopReasonMaxTurns          StopReason = "max_turns"
	StopReasonMaxToolCalls      StopReason = "max_tool_calls"
	StopReasonMaxRuntime        StopReason = "max_runtime"
	StopReasonMaxWorkflowSteps  StopReason = "max_workflow_steps"
	StopReasonMaxSessionRuntime StopReason = "max_session_runtime"
	StopReasonGoalAchieved      StopReason = "goal_achieved"
	StopReasonPlannerStopped    StopReason = "planner_stopped"
	StopReasonContextCanceled   StopReason = "context_canceled"
	StopReasonError             StopReason = "error"
)

// =============================================================================
// StopCondition
// =============================================================================

// StopCondition represents the result of evaluating whether a loop should stop.
type StopCondition struct {
	Stopped bool
	Reason  StopReason
	Detail  string
}

// =============================================================================
// InnerStopEvaluator
// =============================================================================

// InnerStopEvaluator evaluates stop conditions for the inner reasoning turn loop.
type InnerStopEvaluator struct {
	config InnerLoopConfig
}

// NewInnerStopEvaluator creates a new evaluator with the given configuration.
func NewInnerStopEvaluator(config InnerLoopConfig) *InnerStopEvaluator {
	return &InnerStopEvaluator{config: config}
}

// Evaluate checks all inner loop stop conditions in priority order:
// 1. Context cancellation (highest priority — external signal)
// 2. Planner stopped (cooperative stop from LLM)
// 3. Max runtime exceeded
// 4. Max turns reached
// 5. Max tool calls reached
func (e *InnerStopEvaluator) Evaluate(turnsElapsed int, toolCallsUsed int, startTime time.Time, plannerStopped bool, ctx context.Context) StopCondition {
	// Priority 1: Context cancellation
	if ctx.Err() != nil {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonContextCanceled,
			Detail:  fmt.Sprintf("context error: %v", ctx.Err()),
		}
	}

	// Priority 2: Planner stopped
	if plannerStopped {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonPlannerStopped,
			Detail:  "planner signaled completion",
		}
	}

	// Priority 3: Max runtime exceeded
	elapsed := time.Since(startTime)
	if elapsed >= e.config.MaxTurnRuntime {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonMaxRuntime,
			Detail:  fmt.Sprintf("runtime %v exceeded limit %v", elapsed.Truncate(time.Millisecond), e.config.MaxTurnRuntime),
		}
	}

	// Priority 4: Max turns reached
	if turnsElapsed >= e.config.MaxTurns {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonMaxTurns,
			Detail:  fmt.Sprintf("turns %d reached limit %d", turnsElapsed, e.config.MaxTurns),
		}
	}

	// Priority 5: Max tool calls reached
	if toolCallsUsed >= e.config.MaxToolCalls {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonMaxToolCalls,
			Detail:  fmt.Sprintf("tool calls %d reached limit %d", toolCallsUsed, e.config.MaxToolCalls),
		}
	}

	return StopCondition{Stopped: false}
}

// =============================================================================
// OuterStopEvaluator
// =============================================================================

// OuterStopEvaluator evaluates stop conditions for the outer task/workflow loop.
type OuterStopEvaluator struct {
	config OuterLoopConfig
}

// NewOuterStopEvaluator creates a new evaluator with the given configuration.
func NewOuterStopEvaluator(config OuterLoopConfig) *OuterStopEvaluator {
	return &OuterStopEvaluator{config: config}
}

// Evaluate checks all outer loop stop conditions in priority order:
// 1. Context cancellation (highest priority — external signal)
// 2. Max session runtime exceeded
// 3. Max workflow steps reached
func (e *OuterStopEvaluator) Evaluate(stepsElapsed int, startTime time.Time, ctx context.Context) StopCondition {
	// Priority 1: Context cancellation
	if ctx.Err() != nil {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonContextCanceled,
			Detail:  fmt.Sprintf("context error: %v", ctx.Err()),
		}
	}

	// Priority 2: Max session runtime exceeded
	elapsed := time.Since(startTime)
	if elapsed >= e.config.MaxSessionRuntime {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonMaxSessionRuntime,
			Detail:  fmt.Sprintf("session runtime %v exceeded limit %v", elapsed.Truncate(time.Millisecond), e.config.MaxSessionRuntime),
		}
	}

	// Priority 3: Max workflow steps reached
	if stepsElapsed >= e.config.MaxWorkflowSteps {
		return StopCondition{
			Stopped: true,
			Reason:  StopReasonMaxWorkflowSteps,
			Detail:  fmt.Sprintf("workflow steps %d reached limit %d", stepsElapsed, e.config.MaxWorkflowSteps),
		}
	}

	return StopCondition{Stopped: false}
}
