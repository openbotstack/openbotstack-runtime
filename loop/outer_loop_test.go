package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// =============================================================================
// Interface conformance
// =============================================================================

func TestDefaultOuterLoop_ImplementsOuterLoop(t *testing.T) {
	var _ OuterLoop = &DefaultOuterLoop{}
}

// =============================================================================
// DefaultOuterLoop execution tests
// =============================================================================

func TestOuterLoop_SingleTaskSuccess(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 2, ToolCallsUsed: 1, FinalOutput: "done1", StopReason: StopReasonPlannerStopped},
		},
	}
	mockCheckpoint := &mockOuterCheckpoint{saveErr: nil}
	mockPolicy := &mockPolicyCheckpoint{checkErr: nil}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, mockCheckpoint, mockPolicy, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "req1", "asst1", "sess1", "ten1", "user1")

	tasks := []TaskInput{
		{TaskDescription: "task 1", PlannerContext: &planner.PlannerContext{}},
	}

	result, err := loop.Run(ctx, tasks, ec)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
	if result.Metrics.WorkflowSteps != 1 {
		t.Errorf("expected 1 step, got %d", result.Metrics.WorkflowSteps)
	}
	if result.Metrics.TotalTurns != 2 {
		t.Errorf("expected 2 turns, got %d", result.Metrics.TotalTurns)
	}
	if len(result.TaskResults) != 1 {
		t.Errorf("expected 1 task result, got %d", len(result.TaskResults))
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("expected stop reason GoalAchieved, got %s", result.StopCondition.Reason)
	}
}

func TestOuterLoop_MultiTaskSuccess(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 2, ToolCallsUsed: 1, StopReason: StopReasonPlannerStopped},
			{TurnCount: 1, ToolCallsUsed: 0, StopReason: StopReasonPlannerStopped},
			{TurnCount: 3, ToolCallsUsed: 2, StopReason: StopReasonPlannerStopped},
		},
	}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")

	tasks := []TaskInput{
		{TaskDescription: "task 1"},
		{TaskDescription: "task 2"},
		{TaskDescription: "task 3"},
	}

	result, err := loop.Run(ctx, tasks, ec)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metrics.WorkflowSteps != 3 {
		t.Errorf("expected 3 steps, got %d", result.Metrics.WorkflowSteps)
	}
	if result.Metrics.TotalTurns != (2 + 1 + 3) {
		t.Errorf("expected 6 turns, got %d", result.Metrics.TotalTurns)
	}
	if result.Metrics.TotalToolCalls != (1 + 0 + 2) {
		t.Errorf("expected 3 tool calls, got %d", result.Metrics.TotalToolCalls)
	}
}

func TestOuterLoop_InnerLoopError_FailsWorkflow(t *testing.T) {
	expectedErr := errors.New("inner loop failed")
	mockInner := &mockInnerLoop{err: expectedErr}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")

	tasks := []TaskInput{{TaskDescription: "fail pls"}}
	result, err := loop.Run(ctx, tasks, ec)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result.StopCondition.Reason != StopReasonError {
		t.Errorf("expected StopReasonError, got %s", result.StopCondition.Reason)
	}
}

func TestOuterLoop_CheckpointError_FailsWorkflow(t *testing.T) {
	mockInner := &mockInnerLoop{results: []*TaskResult{{TurnCount: 1, StopReason: StopReasonPlannerStopped}}}
	expectedErr := errors.New("checkpoint failed")
	mockCheckpoint := &mockOuterCheckpoint{saveErr: expectedErr}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, mockCheckpoint, &NoOpPolicyCheckpoint{}, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")

	tasks := []TaskInput{{TaskDescription: "fail save"}}
	result, err := loop.Run(ctx, tasks, ec)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result.StopCondition.Reason != StopReasonError {
		t.Errorf("expected StopReasonError, got %s", result.StopCondition.Reason)
	}
}

func TestOuterLoop_PolicyCheckpointError_FailsWorkflow(t *testing.T) {
	mockInner := &mockInnerLoop{results: []*TaskResult{{TurnCount: 1, StopReason: StopReasonPlannerStopped}}}
	expectedErr := errors.New("policy violation")
	mockPolicy := &mockPolicyCheckpoint{checkErr: expectedErr}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, mockPolicy, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")

	tasks := []TaskInput{{TaskDescription: "fail policy"}}
	result, err := loop.Run(ctx, tasks, ec)

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result.StopCondition.Reason != StopReasonError {
		t.Errorf("expected StopReasonError, got %s", result.StopCondition.Reason)
	}
}

func TestOuterLoop_MaxStepsReached(t *testing.T) {
	mockInner := &mockInnerLoop{}
	mockInner.dynamicFunc = func(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error) {
		return &TaskResult{TurnCount: 1, ToolCallsUsed: 0, StopReason: StopReasonPlannerStopped}, nil
	}

	cfg := OuterLoopConfig{MaxWorkflowSteps: 2, MaxSessionRuntime: 1 * time.Minute}
	loop := NewDefaultOuterLoop(cfg, mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})

	// Provide 4 tasks, but limit is 2
	tasks := []TaskInput{
		{TaskDescription: "t1"}, {TaskDescription: "t2"},
		{TaskDescription: "t3"}, {TaskDescription: "t4"},
	}

	result, err := loop.Run(context.Background(), tasks, execution.NewExecutionContext(context.Background(), "r", "a", "s", "t", "u"))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metrics.WorkflowSteps != 2 {
		t.Errorf("expected 2 steps completed, got %d", result.Metrics.WorkflowSteps)
	}
	if result.StopCondition.Reason != StopReasonMaxWorkflowSteps {
		t.Errorf("expected reason %s, got %s", StopReasonMaxWorkflowSteps, result.StopCondition.Reason)
	}
}

func TestOuterLoop_ContextCanceled(t *testing.T) {
	mockInner := &mockInnerLoop{results: []*TaskResult{{TurnCount: 1, StopReason: StopReasonPlannerStopped}}}
	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result, err := loop.Run(ctx, []TaskInput{{TaskDescription: "test"}}, execution.NewExecutionContext(context.Background(), "r", "a", "s", "t", "u"))
	
	if err == nil {
		t.Error("expected error due to context cancellation")
	}
	if result.StopCondition.Reason != StopReasonContextCanceled {
		t.Errorf("expected StopReasonContextCanceled, got %s", result.StopCondition.Reason)
	}
}

func TestDefaultOuterLoop_StopsOnInnerBoundary(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 3, ToolCallsUsed: 5, StopReason: StopReasonMaxTurns},
			{TurnCount: 1, ToolCallsUsed: 0, StopReason: StopReasonPlannerStopped},
		},
	}

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")

	tasks := []TaskInput{
		{TaskDescription: "task that hits limit"},
		{TaskDescription: "task that should never run"},
	}

	result, err := loop.Run(ctx, tasks, ec)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Defect 2 Verification: Outer loop should stop on inner boundary
	if result.Metrics.WorkflowSteps != 1 {
		t.Errorf("expected 1 step completed before stopping, got %d", result.Metrics.WorkflowSteps)
	}
	if result.StopCondition.Reason != StopReasonMaxTurns {
		t.Errorf("expected stop reason %s, got %s", StopReasonMaxTurns, result.StopCondition.Reason)
	}
	if len(result.TaskResults) != 1 {
		t.Errorf("expected 1 task result, got %d", len(result.TaskResults))
	}
}

// =============================================================================
// Mocks
// =============================================================================

type mockInnerLoop struct {
	results     []*TaskResult
	err         error
	callCount   int
	dynamicFunc func(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error)
}

func (m *mockInnerLoop) Run(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.dynamicFunc != nil {
		return m.dynamicFunc(ctx, task, ec)
	}
	if m.callCount < len(m.results) {
		res := m.results[m.callCount]
		m.callCount++
		return res, nil
	}
	return &TaskResult{}, nil
}

type mockPolicyCheckpoint struct {
	checkErr error
}

func (m *mockPolicyCheckpoint) Check(ctx context.Context, taskIndex int, metrics *LoopMetrics) error {
	return m.checkErr
}

// Reuse mockCheckpoint from checkpoint_test.go which already has saveFunc.
// We'll reimplement a simple one here for outer_loop specifically so we don't depend on unexported test types.

type mockOuterCheckpoint struct {
	saveErr error
}

func (m *mockOuterCheckpoint) Save(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
	return m.saveErr
}
