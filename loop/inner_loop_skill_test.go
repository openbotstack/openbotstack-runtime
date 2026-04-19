package loop

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
)

// mockSkillExecutor records skill execution calls.
type mockSkillExecutor struct {
	calls    []string
	results  map[string]*execution.ExecutionResult
	failures map[string]error
}

func newMockSkillExecutor() *mockSkillExecutor {
	return &mockSkillExecutor{
		results:  make(map[string]*execution.ExecutionResult),
		failures: make(map[string]error),
	}
}

func (m *mockSkillExecutor) Execute(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error) {
	m.calls = append(m.calls, req.SkillID)
	if err, ok := m.failures[req.SkillID]; ok {
		return nil, err
	}
	if res, ok := m.results[req.SkillID]; ok {
		return res, nil
	}
	return &execution.ExecutionResult{Output: []byte(`{"status":"ok"}`), Status: execution.StatusSuccess}, nil
}

func (m *mockSkillExecutor) CanExecute(ctx context.Context, skillID string) (bool, error) {
	return true, nil
}

func (m *mockSkillExecutor) LoadSkill(ctx context.Context, pkg skills.Skill) error {
	return nil
}

func (m *mockSkillExecutor) ExecutePlan(ctx context.Context, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) error {
	return nil
}

func TestInnerLoop_SkillStepExecution(t *testing.T) {
	callCount := 0
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		callCount++
		if callCount > 1 {
			return &execution.ExecutionPlan{Steps: nil}, nil
		}
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "my-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{"key": "value"}},
			},
		}, nil
	}

	skillExec := newMockSkillExecutor()
	skillExec.results["my-skill"] = &execution.ExecutionResult{
		Output: []byte(`{"result": 42}`),
		Status: execution.StatusSuccess,
	}

	cfg := InnerLoopConfig{MaxTurns: 2, MaxToolCalls: 10, MaxTurnRuntime: 5 * time.Second}
	il := NewDefaultInnerLoop(cfg, mp, nil, &noopCompactorForState{}, nil)
	il.SetSkillExecutor(skillExec)

	task := TaskInput{TaskDescription: "test", PlannerContext: &planner.PlannerContext{}}
	ec := execution.NewExecutionContext(context.Background(), "r1", "a1", "s1", "t1", "u1")

	result, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(skillExec.calls) != 1 {
		t.Fatalf("expected 1 skill call, got %d", len(skillExec.calls))
	}
	if skillExec.calls[0] != "my-skill" {
		t.Errorf("skill call = %q, want %q", skillExec.calls[0], "my-skill")
	}

	if len(result.TurnResults) == 0 {
		t.Fatal("expected turn results")
	}
	if len(result.TurnResults[0].ActionsExecuted) != 1 {
		t.Errorf("ActionsExecuted = %v, want 1 item", result.TurnResults[0].ActionsExecuted)
	}
	if result.TurnResults[0].ActionsExecuted[0] != "my-skill" {
		t.Errorf("action = %q, want %q", result.TurnResults[0].ActionsExecuted[0], "my-skill")
	}
}

func TestInnerLoop_MixedToolAndSkill(t *testing.T) {
	callCount := 0
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		callCount++
		if callCount > 1 {
			return &execution.ExecutionPlan{Steps: nil}, nil
		}
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "tool-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
				{Name: "skill-1", Type: execution.StepTypeSkill, Arguments: map[string]any{"x": 1}},
				{Name: "tool-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			},
		}, nil
	}

	skillExec := newMockSkillExecutor()
	toolRun := &mockToolRunner{}

	cfg := InnerLoopConfig{MaxTurns: 2, MaxToolCalls: 10, MaxTurnRuntime: 5 * time.Second}
	il := NewDefaultInnerLoop(cfg, mp, toolRun, &noopCompactorForState{}, nil)
	il.SetSkillExecutor(skillExec)

	task := TaskInput{TaskDescription: "test", PlannerContext: &planner.PlannerContext{}}
	ec := execution.NewExecutionContext(context.Background(), "r1", "a1", "s1", "t1", "u1")

	result, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if len(skillExec.calls) != 1 {
		t.Errorf("expected 1 skill call, got %d", len(skillExec.calls))
	}
	if toolRun.calls != 2 {
		t.Errorf("expected 2 tool calls, got %d", toolRun.calls)
	}

	actions := result.TurnResults[0].ActionsExecuted
	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d: %v", len(actions), actions)
	}
	if actions[0] != "tool-1" || actions[1] != "skill-1" || actions[2] != "tool-2" {
		t.Errorf("actions order = %v, want [tool-1 skill-1 tool-2]", actions)
	}
}

func TestInnerLoop_NilToolRunnerSkipsToolStep(t *testing.T) {
	callCount := 0
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		callCount++
		if callCount == 1 {
			return &execution.ExecutionPlan{
				Steps: []execution.ExecutionStep{
					{Name: "some-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
					{Name: "some-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
				},
			}, nil
		}
		return &execution.ExecutionPlan{Steps: nil}, nil
	}

	skillExec := newMockSkillExecutor()

	cfg := InnerLoopConfig{MaxTurns: 2, MaxToolCalls: 10, MaxTurnRuntime: 5 * time.Second}
	il := NewDefaultInnerLoop(cfg, mp, nil, &noopCompactorForState{}, nil) // toolRunner = nil
	il.SetSkillExecutor(skillExec)

	task := TaskInput{TaskDescription: "test", PlannerContext: &planner.PlannerContext{}}
	ec := execution.NewExecutionContext(context.Background(), "r1", "a1", "s1", "t1", "u1")

	result, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run should not fail, got: %v", err)
	}

	// Tool step should be skipped, only skill executed
	if len(skillExec.calls) != 1 {
		t.Errorf("expected 1 skill call, got %d", len(skillExec.calls))
	}

	// Only skill action recorded, tool skipped
	actions := result.TurnResults[0].ActionsExecuted
	if len(actions) != 1 || actions[0] != "some-skill" {
		t.Errorf("actions = %v, want [some-skill] (tool skipped)", actions)
	}
}

func TestInnerLoop_SkillError(t *testing.T) {
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		return &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "failing-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
			},
		}, nil
	}

	skillExec := newMockSkillExecutor()
	skillExec.failures["failing-skill"] = fmt.Errorf("skill exploded")

	cfg := InnerLoopConfig{MaxTurns: 2, MaxToolCalls: 10, MaxTurnRuntime: 5 * time.Second}
	il := NewDefaultInnerLoop(cfg, mp, nil, &noopCompactorForState{}, nil)
	il.SetSkillExecutor(skillExec)

	task := TaskInput{TaskDescription: "test", PlannerContext: &planner.PlannerContext{}}
	ec := execution.NewExecutionContext(context.Background(), "r1", "a1", "s1", "t1", "u1")

	_, err := il.Run(context.Background(), task, ec)
	if err == nil {
		t.Fatal("expected error from failing skill")
	}
	if result := err.Error(); result != "skill execution failed: skill exploded" {
		t.Errorf("error = %q, want skill execution error", result)
	}
}

func TestInnerLoop_SkillStepWithoutExecutor(t *testing.T) {
	// If skill steps are planned but no executor is set, they should be skipped gracefully.
	callCount := 0
	mp := &mockExecutionPlanner{}
	mp.dynamicFunc = func(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
		callCount++
		if callCount == 1 {
			return &execution.ExecutionPlan{
				Steps: []execution.ExecutionStep{
					{Name: "orphan-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
				},
			}, nil
		}
		return &execution.ExecutionPlan{Steps: nil}, nil
	}

	cfg := InnerLoopConfig{MaxTurns: 2, MaxToolCalls: 10, MaxTurnRuntime: 5 * time.Second}
	il := NewDefaultInnerLoop(cfg, mp, nil, &noopCompactorForState{}, nil)
	// No SetSkillExecutor called

	task := TaskInput{TaskDescription: "test", PlannerContext: &planner.PlannerContext{}}
	ec := execution.NewExecutionContext(context.Background(), "r1", "a1", "s1", "t1", "u1")

	result, err := il.Run(context.Background(), task, ec)
	if err != nil {
		t.Fatalf("Run should not fail when skill executor is nil, got: %v", err)
	}

	// Skill step skipped, no actions recorded
	if len(result.TurnResults[0].ActionsExecuted) != 0 {
		t.Errorf("expected 0 actions (skill skipped), got %v", result.TurnResults[0].ActionsExecuted)
	}
}
