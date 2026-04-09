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

func TestDefaultInnerLoop_ImplementsInnerLoop(t *testing.T) {
	var _ InnerLoop = &DefaultInnerLoop{}
}

// =============================================================================
// DefaultInnerLoop execution tests
// =============================================================================

func TestInnerLoop_SingleTurnSuccess(t *testing.T) {
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{{Type: execution.StepTypeTool, Name: "mock_tool"}}},
			{Steps: []execution.ExecutionStep{}}, // empty plan = stop
		},
	}
	mockRunner := &mockToolRunner{
		results: []any{"success"},
	}
	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, mockRunner, &NoOpCompactor{}, &mockLogger{})

	ctx := context.Background()
	task := TaskInput{
		TaskDescription: "do something",
		PlannerContext:  &planner.PlannerContext{},
	}
	ec := execution.NewExecutionContext(ctx, "req1", "asst1", "sess1", "ten1", "user1")

	result, err := loop.Run(ctx, task, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	if result.TurnCount != 2 {
		t.Errorf("expected 2 turns (1 act, 1 stop), got %d", result.TurnCount)
	}
	if result.ToolCallsUsed != 1 {
		t.Errorf("expected 1 tool call, got %d", result.ToolCallsUsed)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("expected planner stopped, got %s", result.StopReason)
	}
	if len(result.TurnResults) != 2 {
		t.Errorf("expected 2 turn results, got %d", len(result.TurnResults))
	}
}

func TestInnerLoop_PlannerError(t *testing.T) {
	expectedErr := errors.New("planner failed")
	mockPlanner := &mockExecutionPlanner{err: expectedErr}
	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, nil, &NoOpCompactor{}, &mockLogger{})

	result, err := loop.Run(context.Background(), TaskInput{}, execution.NewExecutionContext(context.Background(), "req1", "asst1", "sess1", "ten1", "user1"))

	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	if result == nil {
		t.Fatal("expected partial result even on error")
	}
	if result.StopReason != StopReasonError {
		t.Errorf("expected stop reason error, got %s", result.StopReason)
	}
}

func TestInnerLoop_ToolError(t *testing.T) {
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{{Type: execution.StepTypeTool, Name: "failing_tool"}}},
		},
	}
	expectedErr := errors.New("tool failed")
	mockRunner := &mockToolRunner{err: expectedErr}
	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, mockRunner, &NoOpCompactor{}, &mockLogger{})

	result, err := loop.Run(context.Background(), TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})

	// Outer loop should probably receive an error conceptually, but our loop continues and records it as an observation unless we abort
	// For V1, we will abort strictly on tool evaluation errors during execute.
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected tool error %v, got %v", expectedErr, err)
	}
	if result.StopReason != StopReasonError {
		t.Errorf("expected error reason, got %s", result.StopReason)
	}
}

func TestInnerLoop_MaxTurnsReached(t *testing.T) {
	mockPlanner := &mockExecutionPlanner{
		// always return a plan with 0 tool calls so we just spin turns
		plans: []*execution.ExecutionPlan{{Steps: []execution.ExecutionStep{}}},
		// we override the behavior below to just keep returning non-empty plans
	}
	mockPlanner.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{{Type: execution.StepTypeSkill, Name: "just_thinking"}},
		}, nil
	}

	cfg := InnerLoopConfig{MaxTurns: 3, MaxToolCalls: 20, MaxTurnRuntime: 1 * time.Minute}
	loop := NewDefaultInnerLoop(cfg, mockPlanner, &mockToolRunner{}, &NoOpCompactor{}, &mockLogger{})

	result, err := loop.Run(context.Background(), TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != StopReasonMaxTurns {
		t.Errorf("expected max turns, got %s", result.StopReason)
	}
	if result.TurnCount != 3 {
		t.Errorf("expected exactly 3 turns, got %d", result.TurnCount)
	}
}

func TestInnerLoop_MaxToolCallsReached(t *testing.T) {
	mockPlanner := &mockExecutionPlanner{}
	mockPlanner.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		// Provide a plan with 3 tool calls each turn
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Type: execution.StepTypeTool, Name: "t1"},
				{Type: execution.StepTypeTool, Name: "t2"},
				{Type: execution.StepTypeTool, Name: "t3"},
			},
		}, nil
	}

	cfg := InnerLoopConfig{MaxTurns: 10, MaxToolCalls: 5, MaxTurnRuntime: 1 * time.Minute}
	loop := NewDefaultInnerLoop(cfg, mockPlanner, &mockToolRunner{}, &NoOpCompactor{}, &mockLogger{})

	result, err := loop.Run(context.Background(), TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != StopReasonMaxToolCalls {
		t.Errorf("expected max tool calls, got %s", result.StopReason)
	}
	// Turn 1: +3 (total 3)
	// Turn 2: +3 (total 6) -> stops
	if result.TurnCount != 2 {
		t.Errorf("expected 2 turns, got %d", result.TurnCount)
	}
	if result.ToolCallsUsed != 6 {
		t.Errorf("expected 6 tool calls used, got %d", result.ToolCallsUsed)
	}
}

func TestInnerLoop_ContextCanceled(t *testing.T) {
	mockPlanner := &mockExecutionPlanner{}
	mockPlanner.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		return &execution.ExecutionPlan{Steps: []execution.ExecutionStep{{Type: execution.StepTypeTool, Name: "t1"}}}, nil
	}

	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, &mockToolRunner{}, &NoOpCompactor{}, &mockLogger{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := loop.Run(ctx, TaskInput{PlannerContext: &planner.PlannerContext{}}, execution.NewExecutionContext(context.Background(), "req1", "asst1", "sess1", "ten1", "user1"))
	if result == nil {
		t.Fatal("expected result")
	}
	if result.StopReason != StopReasonContextCanceled {
		t.Errorf("expected context canceled, got %s", result.StopReason)
	}
	// Error could be context.Canceled directly depending on where it hits,
	// but generally we expect the loop to just gracefully stop and return the result.
	// Actually, context cancellation often results in an error being returned by the loop itself.
	if err == nil {
		t.Errorf("expected error due to context cancellation")
	}
}

// =============================================================================
// Mocks
// =============================================================================

type mockExecutionPlanner struct {
	plans       []*execution.ExecutionPlan
	err         error
	callCount   int
	dynamicFunc func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error)
}

func (m *mockExecutionPlanner) Plan(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.dynamicFunc != nil {
		return m.dynamicFunc(ctx, pc)
	}
	if m.callCount < len(m.plans) {
		p := m.plans[m.callCount]
		m.callCount++
		return p, nil
	}
	return &execution.ExecutionPlan{}, nil
}

type mockToolRunner struct {
	results []any
	err     error
	calls   int
}

func (m *mockToolRunner) Execute(ctx context.Context, toolID string, parameters map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	res := "mock_result"
	if m.calls < len(m.results) {
		res = m.results[m.calls].(string)
	}
	m.calls++
	return &execution.StepResult{Output: res}, nil
}

type mockLogger struct{}

func (m *mockLogger) LogStep(ctx context.Context, record execution.ExecutionLogRecord) error {
	return nil
}
func (m *mockLogger) LogPlanStart(ctx context.Context, requestID, assistantID string, plan execution.ExecutionPlan) error {
	return nil
}
func (m *mockLogger) LogPlanEnd(ctx context.Context, requestID, assistantID string, err error) error {
	return nil
}
