package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- ExecutionHarness exception & boundary tests ---

func makeFrozenPlan(steps ...execution.ExecutionStep) *execution.ExecutionPlan {
	plan := &execution.ExecutionPlan{Steps: steps}
	_ = plan.Validate()
	return plan
}

func TestHarness_MaxStepsLimit(t *testing.T) {
	// Plan has 5 steps but MaxSteps=3 → only 3 should execute
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 3

	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	steps := make([]execution.ExecutionStep, 5)
	for i := range steps {
		steps[i] = execution.ExecutionStep{
			Name:      fmt.Sprintf("step-%d", i),
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{},
		}
	}
	plan := makeFrozenPlan(steps...)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonMaxSteps {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonMaxSteps)
	}
}

func TestHarness_MaxSessionRuntime(t *testing.T) {
	// Set very short session runtime so it expires immediately
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 10
	cfg.MaxSessionRuntime = 1 * time.Nanosecond

	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.StopCondition.Stopped {
		t.Error("expected execution to be stopped")
	}
	if result.StopCondition.Reason != StopReasonMaxSessionRuntime {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonMaxSessionRuntime)
	}
}

func TestHarness_ContextCancellation(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonContextCanceled {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonContextCanceled)
	}
}

func TestHarness_HookDeny(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	hm := NewHookManager()
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return &execution.HookResult{Deny: true, Reason: "blocked by policy"}, nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "blocked-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonHookDenied {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonHookDenied)
	}
	if result.StepsExecuted != 0 {
		t.Errorf("StepsExecuted = %d, want 0 (denied before execution)", result.StepsExecuted)
	}
}

func TestHarness_PermissionDeny(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	pc := NewPermissionChecker(&execution.PermissionConfig{
		DeniedTools: map[string]bool{"dangerous-tool": true},
	}, nil)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "dangerous-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonHookDenied {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonHookDenied)
	}
}

func TestHarness_LLMStepWithoutReasoningLoop(t *testing.T) {
	cfg := DefaultHarnessConfig()
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})
	// No reasoning loop configured

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "reason-step", Type: execution.StepTypeLLM, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have 1 step executed (with error recorded)
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	// The step result should contain the error
	if len(result.StepResults) != 1 || result.StepResults[0].Error == nil {
		t.Error("expected step error for missing reasoning loop")
	}
}

func TestHarness_LLMStepWithReasoningLoop(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{makeToolPlan("inner-tool")},
	}
	tr := newMockToolRunner()
	innerSE := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rlCfg := DefaultReasoningLoopConfig()
	rlCfg.MaxTurns = 1
	rl := NewDefaultReasoningLoop(rlCfg, mp, innerSE, nil)
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{ReasoningLoop: rl})

	outerPlan := makeFrozenPlan(
		execution.ExecutionStep{Name: "reason-step", Type: execution.StepTypeLLM, Arguments: map[string]any{}, ExpectedOutput: "test task"},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), outerPlan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.Metrics.TotalLLMTurns != 1 {
		t.Errorf("TotalLLMTurns = %d, want 1", result.Metrics.TotalLLMTurns)
	}
}

func TestHarness_StepResultInterpolation(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 5

	tr := newMockToolRunner()
	tr.result["step-a"] = "hello-from-a"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-b", Type: execution.StepTypeTool, Arguments: map[string]any{"input": "{{step-a}}"}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}
}

func TestHarness_FailureHandlerRetry(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.err["flaky-tool"] = fmt.Errorf("transient error")
	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     2,
		InitialBackoff: 1 * time.Millisecond, // fast for tests
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{FailureHandler: fh})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "flaky-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Step should have been retried but ultimately failed (mock always returns same error)
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

func TestHarness_FailureHandlerFallback(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.err["fail-tool"] = fmt.Errorf("permanent error")
	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       false,
		FallbackTool:   "fail-tool", // fallback to same tool (which also fails, but tests the path)
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{FailureHandler: fh})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fail-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	// Should not error at harness level — failure handler consumed the error
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

func TestHarness_FailureHandlerFailFast(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.err["fail-tool"] = fmt.Errorf("fatal error")
	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     1,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
		FailFast:       true,
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{FailureHandler: fh})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fail-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "never-reached", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error for fail-fast")
	}
	if result.StopCondition.Reason != StopReasonFailFast {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonFailFast)
	}
	// Second step should not execute
	if result.StepsExecuted > 1 {
		t.Errorf("StepsExecuted = %d, want <= 1 (fail-fast should stop)", result.StepsExecuted)
	}
}

func TestHarness_RunFromTask_PlannerError(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{errors: []error{fmt.Errorf("planner crashed")}}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	ec := testEC()
	_, err := PlanAndRun(ctx2(), mp, h, TaskInput{TaskDescription: "test task"}, ec)
	if err == nil {
		t.Fatal("expected error from planner")
	}
}

func TestHarness_RunFromTask_EmptyPlan(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{{Steps: []execution.ExecutionStep{}}}}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	ec := testEC()
	result, err := PlanAndRun(ctx2(), mp, h, TaskInput{TaskDescription: "test"}, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonPlannerStopped)
	}
}

func TestHarness_SkillStepExecution(t *testing.T) {
	cfg := DefaultHarnessConfig()
	skillExec := &mockSkillExecutor{resp: []byte(`{"processed":true}`)}
	h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "my-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{"input": "data"}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
}

func TestHarness_MultipleStepsSequencing(t *testing.T) {
	// Verify steps execute in order and results accumulate
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 10

	tr := newMockToolRunner()
	tr.result["first"] = "1"
	tr.result["second"] = "2"
	tr.result["third"] = "3"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "first", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "second", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "third", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}
	// Verify ordering
	names := []string{"first", "second", "third"}
	for i, name := range names {
		if result.StepResults[i].StepName != name {
			t.Errorf("step[%d] = %q, want %q", i, result.StepResults[i].StepName, name)
		}
	}
}

func TestHarness_ProgressCallback(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	var events []ProgressEvent
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		ProgressCB: func(event ProgressEvent) {
			events = append(events, event)
		},
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected progress events to be emitted")
	}
	if events[len(events)-1].Content != "step-1" {
		t.Errorf("last event content = %q, want 'step-1'", events[len(events)-1].Content)
	}
}

func TestHarness_RunFromTask_NilPlannerContext(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{{Steps: []execution.ExecutionStep{}}}}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	ec := testEC()
	// PlannerContext is nil — RunFromTask should auto-create it
	result, err := PlanAndRun(ctx2(), mp, h, TaskInput{TaskDescription: "test without context"}, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonPlannerStopped)
	}
}

// Ensure mockSkillExecutor satisfies SkillExecutor at compile time in this file too
var _ execution.SkillExecutor = (*mockSkillExecutor)(nil)

func ctx2() context.Context { return context.Background() }
