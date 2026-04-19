package loop

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// TestInnerLoop_StateTracking verifies that the inner loop tracks its state
// through the state machine and ends at InnerDone.
func TestInnerLoop_StateTracking(t *testing.T) {
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "test_tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			},
		}, nil
	}

	cfg := InnerLoopConfig{
		MaxTurns:       2,
		MaxToolCalls:   10,
		MaxTurnRuntime: 5 * time.Second,
	}

	il := NewDefaultInnerLoop(cfg, mp, &mockToolRunner{}, &noopCompactorForState{}, nil)

	// Initial state should be empty
	if s := il.State(); s != "" {
		t.Errorf("initial state = %q, want empty", s)
	}

	task := TaskInput{
		TaskDescription: "test",
		PlannerContext:  &planner.PlannerContext{},
	}
	ec := execution.NewExecutionContext(context.Background(), "req1", "asst1", "sess1", "tenant1", "user1")

	_, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// After completion, state should be "done"
	if s := il.State(); s != InnerDone {
		t.Errorf("final state = %q, want %q", s, InnerDone)
	}
}

// TestInnerLoop_StateTracking_SingleTurn verifies state with max_turns=1.
func TestInnerLoop_StateTracking_SingleTurn(t *testing.T) {
	callCount := 0
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		callCount++
		if callCount == 1 {
			return &execution.ExecutionPlan{
				Steps: []execution.ExecutionStep{
					{Name: "tool1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
				},
			}, nil
		}
		// Second call returns empty → planner stopped
		return &execution.ExecutionPlan{Steps: nil}, nil
	}

	cfg := InnerLoopConfig{
		MaxTurns:       2, // allow 2 turns: 1 for execution, 1 for planner to return empty
		MaxToolCalls:   10,
		MaxTurnRuntime: 5 * time.Second,
	}

	il := NewDefaultInnerLoop(cfg, mp, &mockToolRunner{}, &noopCompactorForState{}, nil)

	task := TaskInput{
		TaskDescription: "test",
		PlannerContext:  &planner.PlannerContext{},
	}
	ec := execution.NewExecutionContext(context.Background(), "req1", "asst1", "sess1", "tenant1", "user1")

	result, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// With MaxTurns=2 and planner returning empty on 2nd call, stop reason is planner_stopped
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}

	if s := il.State(); s != InnerDone {
		t.Errorf("final state = %q, want %q", s, InnerDone)
	}
}

// TestOuterLoop_StateTracking verifies that the outer loop tracks its state.
func TestOuterLoop_StateTracking(t *testing.T) {
	mockInner := &stateTestInnerLoop{result: &TaskResult{
		StopReason: StopReasonPlannerStopped,
	}}

	cfg := OuterLoopConfig{
		MaxWorkflowSteps:  5,
		MaxSessionRuntime: 30 * time.Second,
	}

	ol := NewDefaultOuterLoop(cfg, mockInner, &noopCheckpointForState{}, nil, nil)

	// Initial state should be empty
	if s := ol.State(); s != "" {
		t.Errorf("initial state = %q, want empty", s)
	}

	tasks := []TaskInput{
		{TaskDescription: "task1", PlannerContext: &planner.PlannerContext{}},
		{TaskDescription: "task2", PlannerContext: &planner.PlannerContext{}},
	}
	ec := execution.NewExecutionContext(context.Background(), "req1", "asst1", "sess1", "tenant1", "user1")

	_, err := ol.Run(context.Background(), tasks, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// After completion, state should be "done"
	if s := ol.State(); s != OuterDone {
		t.Errorf("final state = %q, want %q", s, OuterDone)
	}
}

// noopCompactorForState avoids name collision with existing test types.
type noopCompactorForState struct{}

func (n *noopCompactorForState) Compact(ctx context.Context, turns []TurnResult) ([]TurnResult, error) {
	return turns, nil
}

// noopCheckpointForState avoids name collision.
type noopCheckpointForState struct{}

func (n *noopCheckpointForState) Save(ctx context.Context, taskIdx int, result *TaskResult, metrics *LoopMetrics) error {
	return nil
}

// stateTestInnerLoop avoids name collision with outer_loop_test.go's mockInnerLoop.
type stateTestInnerLoop struct {
	result *TaskResult
	err    error
}

func (m *stateTestInnerLoop) Run(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error) {
	return m.result, m.err
}
