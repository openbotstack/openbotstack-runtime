package harness

import (
	"context"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// HarnessState represents a state in the execution harness state machine.
type HarnessState string

const (
	HarnessInit     HarnessState = "init"
	HarnessHookPre  HarnessState = "hook_pre_step"
	HarnessStepExec HarnessState = "step_execute"
	HarnessHookPost HarnessState = "hook_post_step"
	HarnessRetry    HarnessState = "retry"
	HarnessFallback HarnessState = "fallback"
	HarnessDone     HarnessState = "done"
)

// StopReason indicates why execution stopped.
type StopReason string

const (
	StopReasonMaxTurns          StopReason = "max_turns"
	StopReasonMaxToolCalls      StopReason = "max_tool_calls"
	StopReasonMaxRuntime        StopReason = "max_runtime"
	StopReasonMaxSteps          StopReason = "max_steps"
	StopReasonMaxSessionRuntime StopReason = "max_session_runtime"
	StopReasonGoalAchieved      StopReason = "goal_achieved"
	StopReasonPlannerStopped    StopReason = "planner_stopped"
	StopReasonContextCanceled   StopReason = "context_canceled"
	StopReasonError             StopReason = "error"
	StopReasonFailFast          StopReason = "fail_fast"
	StopReasonHookDenied        StopReason = "hook_denied"
	StopReasonApprovalRequired  StopReason = "approval_required"
	StopReasonApprovalDenied    StopReason = "approval_denied"
	StopReasonApprovalExpired   StopReason = "approval_expired"
	StopReasonApprovalTimeout   StopReason = "approval_timeout"
)

// StopCondition captures why execution stopped.
type StopCondition struct {
	Stopped bool
	Reason  StopReason
	Detail  string
}

// HarnessConfig holds bounds for the execution harness.
type HarnessConfig struct {
	MaxSteps          int
	MaxSessionRuntime time.Duration
	MaxParallelSubs   int
	DefaultStepTimeout time.Duration
}

// DefaultHarnessConfig returns standard configuration.
func DefaultHarnessConfig() HarnessConfig {
	return HarnessConfig{
		MaxSteps:           10,
		MaxSessionRuntime:  600 * time.Second,
		MaxParallelSubs:    3,
		DefaultStepTimeout: 120 * time.Second,
	}
}

// ReasoningLoopConfig bounds the iterative LLM reasoning loop.
type ReasoningLoopConfig struct {
	MaxTurns       int
	MaxToolCalls   int
	MaxTurnRuntime time.Duration
	RepeatPlanStop bool
}

// DefaultReasoningLoopConfig returns standard bounds (max 5 turns).
func DefaultReasoningLoopConfig() ReasoningLoopConfig {
	return ReasoningLoopConfig{
		MaxTurns:       5,
		MaxToolCalls:   10,
		MaxTurnRuntime: 180 * time.Second,
		RepeatPlanStop: true,
	}
}

// TaskInput provides input for a single task.
type TaskInput struct {
	TaskDescription string
	PlannerContext  *planner.PlannerContext
}

// HarnessResult captures the outcome of an execution harness run.
type HarnessResult struct {
	PlanID        string
	StepsExecuted int
	StepResults   []execution.StepResult
	StopCondition StopCondition
	Metrics       HarnessMetrics
	Duration      time.Duration
}

// ReasoningResult captures the outcome of a reasoning loop.
type ReasoningResult struct {
	Output     any
	TurnCount  int
	ToolCalls  int
	StopReason StopReason
	Duration   time.Duration
}

// HarnessMetrics tracks aggregate execution counters.
type HarnessMetrics struct {
	TotalSteps     int
	TotalToolCalls int
	TotalLLMTurns  int
	TotalRuntime   time.Duration
}

// TurnResult captures data from a single reasoning turn.
type TurnResult struct {
	TurnNumber      int
	PlanText        string
	ActionsExecuted []string
	Observations    []string
	StopReason      StopReason
	ToolCallsUsed   int
	Duration        time.Duration
}

// ProgressEvent represents a progress event emitted during execution.
type ProgressEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
	Turn    int    `json:"turn,omitempty"`
	Tool    string `json:"tool,omitempty"`
}

// ProgressCallback is the signature for progress callbacks.
type ProgressCallback func(event ProgressEvent)

// ContextCompactor compacts turn results to manage context window size.
type ContextCompactor interface {
	Compact(ctx context.Context, turnResults []TurnResult) ([]TurnResult, error)
}

// ReasoningStorer stores reasoning audit trails keyed by execution ID.
type ReasoningStorer interface {
	StoreTrail(executionID string, entries []audit.AuditEvent)
}
