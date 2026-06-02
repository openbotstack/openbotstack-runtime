package harness

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/registry/skills"
)

// --- Mock infrastructure ---

// mockPlanner records calls and returns configurable plans.
type mockPlanner struct {
	mu        sync.Mutex
	plans     []*execution.ExecutionPlan
	callIdx   int
	errors    []error
	repeatLast bool // if true, repeat the last plan instead of returning empty
}

func (mp *mockPlanner) Plan(ctx context.Context, pCtx *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	idx := mp.callIdx
	mp.callIdx++

	if idx < len(mp.errors) && mp.errors[idx] != nil {
		return nil, mp.errors[idx]
	}
	if idx < len(mp.plans) {
		return mp.plans[idx], nil
	}
	// Repeat last plan if repeatLast is set
	if mp.repeatLast && len(mp.plans) > 0 {
		return mp.plans[len(mp.plans)-1], nil
	}
	// Default: empty plan (planner stopped)
	return &execution.ExecutionPlan{Steps: []execution.ExecutionStep{}}, nil
}

func (mp *mockPlanner) callCount() int {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	return mp.callIdx
}

// mockToolRunner records tool executions.
type mockToolRunner struct {
	mu       sync.Mutex
	calls    []string
	result   map[string]any // tool name → output
	err      map[string]error
	LastArgs map[string]any // arguments from most recent Execute call
}

func newMockToolRunner() *mockToolRunner {
	return &mockToolRunner{
		result: make(map[string]any),
		err:    make(map[string]error),
	}
}

func (m *mockToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, toolName)
	m.LastArgs = args
	if err, ok := m.err[toolName]; ok {
		return nil, err
	}
	if out, ok := m.result[toolName]; ok {
		return &execution.StepResult{StepName: toolName, Output: out}, nil
	}
	return &execution.StepResult{StepName: toolName, Output: fmt.Sprintf("result-%s", toolName)}, nil
}

func (m *mockToolRunner) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// mockSkillExecutor records skill executions.
type mockSkillExecutor struct {
	mu    sync.Mutex
	calls []string
	resp  []byte
	err   error
}

func (m *mockSkillExecutor) Execute(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req.SkillID)
	if m.err != nil {
		return nil, m.err
	}
	status := execution.StatusSuccess
	return &execution.ExecutionResult{Output: m.resp, Status: status}, nil
}

func (m *mockSkillExecutor) CanExecute(ctx context.Context, skillID string) (bool, error) { return true, nil }
func (m *mockSkillExecutor) LoadSkill(ctx context.Context, pkg skills.Skill) error        { return nil }
func (m *mockSkillExecutor) ExecutePlan(ctx context.Context, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) error {
	return nil
}

func (m *mockSkillExecutor) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// helper: create a plan with N tool steps
func makeToolPlan(stepNames ...string) *execution.ExecutionPlan {
	steps := make([]execution.ExecutionStep, len(stepNames))
	for i, name := range stepNames {
		steps[i] = execution.ExecutionStep{
			StepID:    fmt.Sprintf("step-%d", i),
			Name:      name,
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{},
		}
	}
	plan := &execution.ExecutionPlan{Steps: steps}
	_ = plan.Validate() // auto-generate StepIDs + freeze
	return plan
}

// helper: create a frozen plan with LLM step
func makeLLMPlan(name, expectedOutput string) *execution.ExecutionPlan {
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{
				Name:           name,
				Type:           execution.StepTypeLLM,
				Arguments:      map[string]any{},
				ExpectedOutput: expectedOutput,
			},
		},
	}
	_ = plan.Validate()
	return plan
}

// helper: minimal execution context
func testEC() *execution.ExecutionContext {
	return execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
}

// helper: minimal planner context
func testPCtx(request string) *planner.PlannerContext {
	return &planner.PlannerContext{
		UserRequest:   request,
		MemoryContext: []planner.SearchResult{},
	}
}

// --- TDD Group 1: ReasoningLoop behavioral tests ---

func TestReasoningLoop_EmptyPlan_StopsImmediately(t *testing.T) {
	// RED: Planner returns empty plan on first call → loop should stop
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{}},
		},
	}
	rl := NewDefaultReasoningLoop(DefaultReasoningLoopConfig(), mp, nil, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}
	if result.ToolCalls != 0 {
		t.Errorf("ToolCalls = %d, want 0", result.ToolCalls)
	}
}

func TestReasoningLoop_MaxTurnsEnforced(t *testing.T) {
	// RED: Planner always returns a plan → loop should stop at MaxTurns
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false // disable repeat detection

	// Each call returns a plan with one tool step
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{
			makeToolPlan("tool-a"),
			makeToolPlan("tool-b"),
			makeToolPlan("tool-c"),
			makeToolPlan("tool-d"), // 4th plan should not execute
		},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3 (max)", result.TurnCount)
	}
	if result.StopReason != StopReasonMaxTurns {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurns)
	}
	if mp.callCount() != 3 {
		t.Errorf("planner called %d times, want 3", mp.callCount())
	}
}

func TestReasoningLoop_RepeatedPlan_Stops(t *testing.T) {
	// RED: Two consecutive plans with same reasoning → stop
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 10
	cfg.RepeatPlanStop = true

	samePlan := &execution.ExecutionPlan{
		Steps:     []execution.ExecutionStep{{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}}},
		Reasoning: "identical reasoning",
	}
	_ = samePlan.Validate()

	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{samePlan, samePlan},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2 (stopped on repeat)", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}
}

func TestReasoningLoop_ContextCancellation(t *testing.T) {
	// RED: Context cancelled before first plan → immediate stop
	cfg := DefaultReasoningLoopConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{makeToolPlan("tool-a")},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	result, err := rl.Run(ctx, &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on cancellation")
	}
	if result.StopReason != StopReasonContextCanceled {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonContextCanceled)
	}
}

func TestReasoningLoop_ToolCallBudgetExhausted(t *testing.T) {
	// RED: MaxToolCalls=1, but plan has 2 tool steps per turn
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 5
	cfg.MaxToolCalls = 2

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{plan, plan, plan},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ToolCalls > 2 {
		t.Errorf("ToolCalls = %d, want <= 2 (budget)", result.ToolCalls)
	}
	if result.StopReason != StopReasonMaxToolCalls && result.StopReason != StopReasonMaxTurns {
		// Either budget or turns could stop it
		t.Errorf("StopReason = %q, expected max_tool_calls or max_turns", result.StopReason)
	}
}

func TestReasoningLoop_StepOutputPropagation(t *testing.T) {
	// RED: Step A output should be available to Step B via {{step_a}}
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "step-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "step-b", Type: execution.StepTypeTool, Arguments: map[string]any{"input": "{{step-a}}"}},
		},
	}
	_ = plan.Validate()

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}}
	tr := newMockToolRunner()
	tr.result["step-a"] = "42"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.callCount() != 2 {
		t.Errorf("tool calls = %d, want 2", tr.callCount())
	}
	if result.Output == nil {
		t.Error("expected non-nil output")
	}
}

func TestReasoningLoop_NestedLLMStep_Skipped(t *testing.T) {
	// Plan contains LLM step inside reasoning loop → should be skipped
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "nested-llm", Type: execution.StepTypeLLM, Arguments: map[string]any{}},
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	_, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only tool-a should have been called, not nested LLM
	if tr.callCount() != 1 {
		t.Errorf("tool calls = %d, want 1 (nested LLM skipped)", tr.callCount())
	}
}

// --- TDD Group 2: Exception & boundary scenarios ---

func TestReasoningLoop_PlannerError_ContinuesToNextTurn(t *testing.T) {
	// Planner errors on turn 1, succeeds on turn 2 → loop should continue
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{
		plans:  []*execution.ExecutionPlan{nil, plan},
		errors: []error{fmt.Errorf("planner temporarily unavailable")},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Turn 1: planner error (continues), Turn 2: plan + execute, Turn 3: planner stops
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}
}

func TestReasoningLoop_SkillStepExecution(t *testing.T) {
	// Plan with skill steps should execute through SkillExecutor
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "my-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{"input": "test"}},
		},
	}
	_ = plan.Validate()

	skillExec := &mockSkillExecutor{resp: []byte(`{"result":"ok"}`)}
	se := NewStepExecutor(nil, skillExec, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{plans: []*execution.ExecutionPlan{plan}}, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
	if result.Output == nil {
		t.Error("expected non-nil output from skill execution")
	}
}

func TestReasoningLoop_MixedToolAndSkillSteps(t *testing.T) {
	// Plan with both tool and skill steps in same turn
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "my-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{"input": "data"}},
		},
	}
	_ = plan.Validate()

	tr := newMockToolRunner()
	tr.result["tool-a"] = "42"
	skillExec := &mockSkillExecutor{resp: []byte(`{"processed":true}`)}
	se := NewStepExecutor(tr, skillExec, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{plans: []*execution.ExecutionPlan{plan}}, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.callCount() != 1 {
		t.Errorf("tool calls = %d, want 1", tr.callCount())
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
	if result.Output == nil {
		t.Error("expected non-nil output")
	}
}

func TestReasoningLoop_ObservationsPropagateToNextTurn(t *testing.T) {
	// Turn 1 observations should be injected into PlannerContext.MemoryContext for turn 2
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 2

	pCtx := testPCtx("test")
	initialMemLen := len(pCtx.MemoryContext)

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan, {Steps: []execution.ExecutionStep{}}}}
	tr := newMockToolRunner()
	tr.result["tool-a"] = "observed-value"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	_, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MemoryContext should have grown by observations from turn 1
	if len(pCtx.MemoryContext) <= initialMemLen {
		t.Errorf("MemoryContext length = %d, expected > %d (observations not injected)", len(pCtx.MemoryContext), initialMemLen)
	}
}

func TestReasoningLoop_MidStepFailure_ContinuesToNextStep(t *testing.T) {
	// Plan has 3 steps, step 2 fails → step 3 still executes (current behavior: continue on error)
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-ok", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-fail", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-after", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	tr := newMockToolRunner()
	tr.result["tool-ok"] = "success"
	tr.err["tool-fail"] = fmt.Errorf("step failed intentionally")
	tr.result["tool-after"] = "after-fail"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{plans: []*execution.ExecutionPlan{plan}}, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.callCount() != 3 {
		t.Errorf("tool calls = %d, want 3 (all steps attempted)", tr.callCount())
	}
	// The observation for tool-fail should contain error info
	if len(result.Output.(string)) == 0 {
		t.Error("expected non-empty output from last successful step")
	}
}

func TestReasoningLoop_InvalidConfig_ReturnsError(t *testing.T) {
	// MaxTurns=0 or MaxToolCalls=0 should fail immediately
	cfg := ReasoningLoopConfig{MaxTurns: 0, MaxToolCalls: 10}
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{}, nil, nil)

	_, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err == nil {
		t.Fatal("expected error for MaxTurns=0")
	}

	cfg2 := ReasoningLoopConfig{MaxTurns: 5, MaxToolCalls: 0}
	rl2 := NewDefaultReasoningLoop(cfg2, &mockPlanner{}, nil, nil)
	_, err2 := rl2.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err2 == nil {
		t.Fatal("expected error for MaxToolCalls=0")
	}
}

func TestReasoningLoop_ToolBudgetStopsMidTurn(t *testing.T) {
	// Plan has 3 tool steps but budget is 2 → third step should not execute
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 5 // high enough so budget is the stop condition
	cfg.MaxToolCalls = 2

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{plans: []*execution.ExecutionPlan{plan}}, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.callCount() != 2 {
		t.Errorf("tool calls = %d, want 2 (budget exhausted before step 3)", tr.callCount())
	}
	if result.StopReason != StopReasonMaxToolCalls {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxToolCalls)
	}
}

func TestReasoningLoop_ContextCompactorIntegration(t *testing.T) {
	// After 2+ turns, ContextCompactor should be called
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 4
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan, plan, plan, plan}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	compacted := false
	compactor := &mockCompactor{compactFn: func(ctx context.Context, results []TurnResult) ([]TurnResult, error) {
		compacted = true
		return results, nil
	}}
	rl := NewDefaultReasoningLoop(cfg, mp, se, compactor)

	_, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !compacted {
		t.Error("expected ContextCompactor to be called after 2+ turns")
	}
}

func TestReasoningLoop_UnknownStepType_Skipped(t *testing.T) {
	// Plan contains an unknown step type → should be skipped with warning
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 1

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "unknown-step", Type: execution.StepType("unknown"), Arguments: map[string]any{}},
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, &mockPlanner{plans: []*execution.ExecutionPlan{plan}}, se, nil)

	_, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.callCount() != 1 {
		t.Errorf("tool calls = %d, want 1 (unknown step skipped)", tr.callCount())
	}
}

func TestReasoningLoop_AllStepsCompleteThenNextTurn(t *testing.T) {
	// Turn 1: 2-step plan completes, Turn 2: planner returns empty → stop
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{
		plan,
		{Steps: []execution.ExecutionStep{}}, // empty plan = planner done
	}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", result.TurnCount)
	}
	if tr.callCount() != 2 {
		t.Errorf("tool calls = %d, want 2 (both steps in turn 1)", tr.callCount())
	}
}

// mockCompactor for testing ContextCompactor integration
type mockCompactor struct {
	compactFn func(ctx context.Context, results []TurnResult) ([]TurnResult, error)
}

func (mc *mockCompactor) Compact(ctx context.Context, results []TurnResult) ([]TurnResult, error) {
	if mc.compactFn != nil {
		return mc.compactFn(ctx, results)
	}
	return results, nil
}
