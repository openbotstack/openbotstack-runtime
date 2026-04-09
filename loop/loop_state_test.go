package loop

import (
	"testing"
	"time"
)

// =============================================================================
// OuterState enum tests
// =============================================================================

func TestOuterState_AllValuesAreDefined(t *testing.T) {
	states := []OuterState{
		OuterInit,
		OuterTaskSelect,
		OuterTaskExecute,
		OuterCheckpoint,
		OuterNextTask,
		OuterDone,
	}

	for _, s := range states {
		if s == "" {
			t.Error("OuterState value must not be empty")
		}
	}

	if len(states) != 6 {
		t.Errorf("expected 6 OuterState values, got %d", len(states))
	}
}

func TestOuterState_StringValues(t *testing.T) {
	tests := []struct {
		state OuterState
		want  string
	}{
		{OuterInit, "init"},
		{OuterTaskSelect, "task_select"},
		{OuterTaskExecute, "task_execute"},
		{OuterCheckpoint, "checkpoint"},
		{OuterNextTask, "next_task"},
		{OuterDone, "done"},
	}
	for _, tt := range tests {
		if string(tt.state) != tt.want {
			t.Errorf("OuterState %q != expected %q", tt.state, tt.want)
		}
	}
}

func TestOuterState_Uniqueness(t *testing.T) {
	states := []OuterState{
		OuterInit, OuterTaskSelect, OuterTaskExecute,
		OuterCheckpoint, OuterNextTask, OuterDone,
	}
	seen := make(map[OuterState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate OuterState value: %s", s)
		}
		seen[s] = true
	}
}

// =============================================================================
// InnerState enum tests
// =============================================================================

func TestInnerState_AllValuesAreDefined(t *testing.T) {
	states := []InnerState{
		InnerTurnInit,
		InnerPlan,
		InnerAct,
		InnerObserve,
		InnerCheckStop,
		InnerNextTurn,
		InnerDone,
	}

	for _, s := range states {
		if s == "" {
			t.Error("InnerState value must not be empty")
		}
	}

	if len(states) != 7 {
		t.Errorf("expected 7 InnerState values, got %d", len(states))
	}
}

func TestInnerState_StringValues(t *testing.T) {
	tests := []struct {
		state InnerState
		want  string
	}{
		{InnerTurnInit, "turn_init"},
		{InnerPlan, "plan"},
		{InnerAct, "act"},
		{InnerObserve, "observe"},
		{InnerCheckStop, "check_stop"},
		{InnerNextTurn, "next_turn"},
		{InnerDone, "done"},
	}
	for _, tt := range tests {
		if string(tt.state) != tt.want {
			t.Errorf("InnerState %q != expected %q", tt.state, tt.want)
		}
	}
}

func TestInnerState_Uniqueness(t *testing.T) {
	states := []InnerState{
		InnerTurnInit, InnerPlan, InnerAct, InnerObserve,
		InnerCheckStop, InnerNextTurn, InnerDone,
	}
	seen := make(map[InnerState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate InnerState value: %s", s)
		}
		seen[s] = true
	}
}

// =============================================================================
// OuterLoopConfig tests
// =============================================================================

func TestDefaultOuterConfig(t *testing.T) {
	cfg := DefaultOuterConfig()

	if cfg.MaxWorkflowSteps != 5 {
		t.Errorf("MaxWorkflowSteps = %d, want 5", cfg.MaxWorkflowSteps)
	}
	if cfg.MaxSessionRuntime != 30*time.Second {
		t.Errorf("MaxSessionRuntime = %v, want 30s", cfg.MaxSessionRuntime)
	}
}

func TestOuterLoopConfig_ZeroValueIsNotDefault(t *testing.T) {
	var cfg OuterLoopConfig
	if cfg.MaxWorkflowSteps != 0 {
		t.Error("zero value MaxWorkflowSteps must be 0")
	}
	if cfg.MaxSessionRuntime != 0 {
		t.Error("zero value MaxSessionRuntime must be 0")
	}
}

// =============================================================================
// InnerLoopConfig tests
// =============================================================================

func TestDefaultInnerConfig(t *testing.T) {
	cfg := DefaultInnerConfig()

	if cfg.MaxTurns != 8 {
		t.Errorf("MaxTurns = %d, want 8", cfg.MaxTurns)
	}
	if cfg.MaxToolCalls != 20 {
		t.Errorf("MaxToolCalls = %d, want 20", cfg.MaxToolCalls)
	}
	if cfg.MaxTurnRuntime != 15*time.Second {
		t.Errorf("MaxTurnRuntime = %v, want 15s", cfg.MaxTurnRuntime)
	}
}

func TestInnerLoopConfig_ZeroValueIsNotDefault(t *testing.T) {
	var cfg InnerLoopConfig
	if cfg.MaxTurns != 0 {
		t.Error("zero value MaxTurns must be 0")
	}
	if cfg.MaxToolCalls != 0 {
		t.Error("zero value MaxToolCalls must be 0")
	}
	if cfg.MaxTurnRuntime != 0 {
		t.Error("zero value MaxTurnRuntime must be 0")
	}
}

// =============================================================================
// TurnResult tests
// =============================================================================

func TestTurnResult_ZeroValue(t *testing.T) {
	var tr TurnResult
	if tr.PlanText != "" {
		t.Error("zero TurnResult.PlanText must be empty")
	}
	if len(tr.ActionsExecuted) != 0 {
		t.Error("zero TurnResult.ActionsExecuted must be empty")
	}
	if len(tr.Observations) != 0 {
		t.Error("zero TurnResult.Observations must be empty")
	}
	if tr.StopReason != "" {
		t.Error("zero TurnResult.StopReason must be empty")
	}
	if tr.ToolCallsUsed != 0 {
		t.Error("zero TurnResult.ToolCallsUsed must be 0")
	}
	if tr.Duration != 0 {
		t.Error("zero TurnResult.Duration must be 0")
	}
}

// =============================================================================
// TaskResult tests
// =============================================================================

func TestTaskResult_ZeroValue(t *testing.T) {
	var tr TaskResult
	if tr.TurnCount != 0 {
		t.Error("zero TaskResult.TurnCount must be 0")
	}
	if tr.ToolCallsUsed != 0 {
		t.Error("zero TaskResult.ToolCallsUsed must be 0")
	}
	if tr.FinalOutput != nil {
		t.Error("zero TaskResult.FinalOutput must be nil")
	}
	if tr.Error != nil {
		t.Error("zero TaskResult.Error must be nil")
	}
	if tr.StopReason != "" {
		t.Error("zero TaskResult.StopReason must be empty")
	}
	if tr.Duration != 0 {
		t.Error("zero TaskResult.Duration must be 0")
	}
	if len(tr.TurnResults) != 0 {
		t.Error("zero TaskResult.TurnResults must be empty")
	}
}

// =============================================================================
// LoopMetrics tests
// =============================================================================

func TestLoopMetrics_ZeroValue(t *testing.T) {
	var m LoopMetrics
	if m.WorkflowSteps != 0 {
		t.Error("zero LoopMetrics.WorkflowSteps must be 0")
	}
	if m.TotalTurns != 0 {
		t.Error("zero LoopMetrics.TotalTurns must be 0")
	}
	if m.TotalToolCalls != 0 {
		t.Error("zero LoopMetrics.TotalToolCalls must be 0")
	}
	if m.TotalRuntime != 0 {
		t.Error("zero LoopMetrics.TotalRuntime must be 0")
	}
}

// =============================================================================
// TaskInput tests
// =============================================================================

func TestTaskInput_ZeroValue(t *testing.T) {
	var ti TaskInput
	if ti.TaskDescription != "" {
		t.Error("zero TaskInput.TaskDescription must be empty")
	}
	if ti.PlannerContext != nil {
		t.Error("zero TaskInput.PlannerContext must be nil")
	}
}

// =============================================================================
// WorkflowResult tests
// =============================================================================

func TestWorkflowResult_ZeroValue(t *testing.T) {
	var wr WorkflowResult
	if len(wr.TaskResults) != 0 {
		t.Error("zero WorkflowResult.TaskResults must be empty")
	}
	if wr.StopCondition.Stopped {
		t.Error("zero WorkflowResult.StopCondition.Stopped must be false")
	}
}
