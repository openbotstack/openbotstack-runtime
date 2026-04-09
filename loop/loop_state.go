// Package loop implements the dual bounded loop kernel for OpenBotStack.
//
// The kernel consists of two nested, bounded loops:
//   - Outer Loop: task/workflow orchestration (INIT → TASK_SELECT → TASK_EXECUTE → CHECKPOINT → NEXT_TASK → DONE)
//   - Inner Loop: reasoning turn loop (TURN_INIT → PLAN → ACT → OBSERVE → CHECK_STOP → NEXT_TURN → DONE)
//
// All loops are bounded by explicit step/turn counters and wall-clock timeouts.
// See ADR-012 for design rationale.
package loop

import (
	"time"

	"github.com/openbotstack/openbotstack-core/planner"
)

// =============================================================================
// Outer Loop State
// =============================================================================

// OuterState represents a state in the outer task/workflow loop.
type OuterState string

const (
	OuterInit        OuterState = "init"
	OuterTaskSelect  OuterState = "task_select"
	OuterTaskExecute OuterState = "task_execute"
	OuterCheckpoint  OuterState = "checkpoint"
	OuterNextTask    OuterState = "next_task"
	OuterDone        OuterState = "done"
)

// =============================================================================
// Inner Loop State
// =============================================================================

// InnerState represents a state in the inner reasoning turn loop.
type InnerState string

const (
	InnerTurnInit  InnerState = "turn_init"
	InnerPlan      InnerState = "plan"
	InnerAct       InnerState = "act"
	InnerObserve   InnerState = "observe"
	InnerCheckStop InnerState = "check_stop"
	InnerNextTurn  InnerState = "next_turn"
	InnerDone      InnerState = "done"
)

// =============================================================================
// Configuration
// =============================================================================

// OuterLoopConfig holds bounds for the outer task/workflow loop.
type OuterLoopConfig struct {
	MaxWorkflowSteps int
	MaxSessionRuntime time.Duration
}

// DefaultOuterConfig returns the standard outer loop configuration.
func DefaultOuterConfig() OuterLoopConfig {
	return OuterLoopConfig{
		MaxWorkflowSteps:  5,
		MaxSessionRuntime: 30 * time.Second,
	}
}

// InnerLoopConfig holds bounds for the inner reasoning turn loop.
type InnerLoopConfig struct {
	MaxTurns      int
	MaxToolCalls  int
	MaxTurnRuntime time.Duration
}

// DefaultInnerConfig returns the standard inner loop configuration.
func DefaultInnerConfig() InnerLoopConfig {
	return InnerLoopConfig{
		MaxTurns:       8,
		MaxToolCalls:   20,
		MaxTurnRuntime: 15 * time.Second,
	}
}

// =============================================================================
// Result Types
// =============================================================================

// TurnResult captures the outcome of a single reasoning turn in the inner loop.
type TurnResult struct {
	TurnNumber      int
	PlanText        string
	ActionsExecuted []string
	Observations    []string
	StopReason      StopReason
	ToolCallsUsed   int
	Duration        time.Duration
}

// TaskResult captures the outcome of executing a single task via the inner loop.
type TaskResult struct {
	TurnCount     int
	ToolCallsUsed int
	FinalOutput   any
	Error         error
	StopReason    StopReason
	Duration      time.Duration
	TurnResults   []TurnResult
}

// WorkflowResult captures the outcome of the entire outer loop execution.
type WorkflowResult struct {
	TaskResults   []*TaskResult
	Metrics       LoopMetrics
	StopCondition StopCondition
}

// LoopMetrics tracks aggregate counters across the entire dual-loop execution.
type LoopMetrics struct {
	WorkflowSteps int
	TotalTurns    int
	TotalToolCalls int
	TotalRuntime   time.Duration
}

// =============================================================================
// Input Types
// =============================================================================

// TaskInput provides the input for a single task in the outer loop.
type TaskInput struct {
	TaskDescription string
	PlannerContext  *planner.PlannerContext
}
