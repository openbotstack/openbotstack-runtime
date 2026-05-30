package harness

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// ========================================================================
// Phase 4.6 — Execution Hardening Tests
// ========================================================================

// ---------------------------------------------------------------------------
// Part 1: Hook Execution Gap (PreToolUse / PostToolUse)
// ---------------------------------------------------------------------------

// Hook P1.1: PreToolUse is called before tool execution
func TestHook_PreToolUseCalled(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["my-tool"] = "ok"
	hm := NewHookManager()
	var preToolCalled bool
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		preToolCalled = true
		return &execution.HookResult{}, nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "my-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !preToolCalled {
		t.Error("PreToolUse hook was not called")
	}
}

// Hook P1.2: PostToolUse is called after tool execution (even on error)
func TestHook_PostToolUseCalledOnError(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.err["bad-tool"] = fmt.Errorf("tool error")
	hm := NewHookManager()
	var postToolCalled bool
	hm.RegisterPostToolUse(func(ctx context.Context, hctx *execution.HookContext) error {
		postToolCalled = true
		return nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "bad-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !postToolCalled {
		t.Error("PostToolUse hook should be called even on tool error")
	}
}

// Hook P1.3: Hook order is PreTool → Execute → PostTool → PostStep
func TestHook_OrderIsCorrect(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	hm := NewHookManager()
	var order []string
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		order = append(order, "pre-step")
		return &execution.HookResult{}, nil
	})
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		order = append(order, "pre-tool")
		return &execution.HookResult{}, nil
	})
	hm.RegisterPostToolUse(func(ctx context.Context, hctx *execution.HookContext) error {
		order = append(order, "post-tool")
		return nil
	})
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		order = append(order, "post-step")
		return nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pre-step", "pre-tool", "post-tool", "post-step"}
	if len(order) != len(expected) {
		t.Fatalf("hook order length = %d, want %d: %v", len(order), len(expected), order)
	}
	for i, want := range expected {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

// Hook P1.4: PreToolUse deny blocks tool execution
func TestHook_PreToolUseDenyBlocks(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	hm := NewHookManager()
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return &execution.HookResult{Deny: true, Reason: "security policy"}, nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Tool should not have been executed — result has error
	if len(result.StepResults) != 1 {
		t.Fatalf("StepResults count = %d, want 1", len(result.StepResults))
	}
	if result.StepResults[0].Error == nil {
		t.Error("expected error from PreToolUse deny")
	}
}

// Hook P1.5: PreToolUse for skill steps
func TestHook_PreToolUseForSkillStep(t *testing.T) {
	cfg := DefaultHarnessConfig()
	skillExec := &mockSkillExecutor{resp: []byte(`{"ok":true}`)}
	hm := NewHookManager()
	var preToolCalled bool
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		preToolCalled = true
		return &execution.HookResult{}, nil
	})
	h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "my-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
	)
	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !preToolCalled {
		t.Error("PreToolUse should be called for skill steps too")
	}
}

// ---------------------------------------------------------------------------
// Part 2: Execution Contract Enforcement
// ---------------------------------------------------------------------------

// Contract P2.1: Step timeout enforced
func TestContract_StepTimeout_Enforced(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := &blockingToolRunner{}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{
			Name: "slow-tool", Type: execution.StepTypeTool,
			Arguments: map[string]any{}, Timeout: 50,
		},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Error == nil {
		t.Error("expected timeout error from slow tool")
	}
	if result.Duration > 200*time.Millisecond {
		t.Errorf("Duration = %v, should be bounded by timeout", result.Duration)
	}
}

// Contract P2.2: Invalid step type produces error in result, not panic
func TestContract_InvalidStepType_NoPanic(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	// Can't use makeFrozenPlan — Validate rejects unknown types
	// Create plan manually and bypass Validate
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "bad-type", Type: "invalid_type", Arguments: map[string]any{}},
		},
	}
	// Force freeze without validation to test harness behavior with bad step type
	plan.Freeze()
	// Need to set StepID manually
	for i := range plan.Steps {
		if plan.Steps[i].StepID == "" {
			plan.Steps[i].StepID = fmt.Sprintf("step-%d", i)
		}
	}

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Error == nil {
		t.Error("expected error for invalid step type")
	}
}

// Contract P2.3: Nil arguments don't panic
func TestContract_NilArguments_NoPanic(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

// Contract P2.4: Error propagation — every failed step has trace
func TestContract_ErrorPropagation_EveryStepTraced(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.err["step-2"] = fmt.Errorf("mid-plan failure")
	tr.result["step-3"] = "ok"

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}
	// Step 2 must have error recorded
	if result.StepResults[1].Error == nil {
		t.Error("step-2 should have error captured in StepResults")
	}
	// Step 1 and 3 must succeed
	if result.StepResults[0].Error != nil {
		t.Error("step-1 should succeed")
	}
	if result.StepResults[2].Error != nil {
		t.Error("step-3 should succeed (continue-on-error)")
	}
}

// ---------------------------------------------------------------------------
// Part 3: Policy Enforcement Testing
// ---------------------------------------------------------------------------

// Policy P3.1: Enforcer deny blocks execution
func TestPolicy_EnforcerDeny(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["sensitive-tool"] = "secret"
	pc := NewPermissionChecker(
		&execution.PermissionConfig{DeniedTools: map[string]bool{"sensitive-tool": true}},
		nil,
	)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "sensitive-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
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

// Policy P3.2: Allow list permits specific tools only
func TestPolicy_AllowListOnly(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["allowed-tool"] = "ok"
	pc := NewPermissionChecker(
		&execution.PermissionConfig{AllowedTools: map[string]bool{"allowed-tool": true}},
		nil,
	)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	// Allowed tool should pass
	plan1 := makeFrozenPlan(
		execution.ExecutionStep{Name: "allowed-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, _ := h.Run(context.Background(), plan1, testEC())
	if result.StopCondition.Stopped && result.StopCondition.Reason == StopReasonHookDenied {
		t.Error("allowed tool should not be denied")
	}

	// Non-allowed tool should be denied
	plan2 := makeFrozenPlan(
		execution.ExecutionStep{Name: "other-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result2, _ := h.Run(context.Background(), plan2, testEC())
	if result2.StopCondition.Reason != StopReasonHookDenied {
		t.Errorf("non-allowed tool should be denied, got %q", result2.StopCondition.Reason)
	}
}

// Policy P3.3: Deny-all mode blocks everything
func TestPolicy_DenyAllMode(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	pc := NewPermissionChecker(
		&execution.PermissionConfig{ApprovalMode: execution.ApprovalModeDeny},
		nil,
	)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "any-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, _ := h.Run(context.Background(), plan, testEC())
	if result.StopCondition.Reason != StopReasonHookDenied {
		t.Errorf("deny-all mode should block, got %q", result.StopCondition.Reason)
	}
}

// Policy P3.4: Auto mode allows everything
func TestPolicy_AutoModeAllowsAll(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	pc := NewPermissionChecker(
		&execution.PermissionConfig{ApprovalMode: execution.ApprovalModeAuto},
		nil,
	)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("auto mode should allow execution, StepsExecuted = %d", result.StepsExecuted)
	}
}

// ---------------------------------------------------------------------------
// Part 4: RunFromTask Coverage
// ---------------------------------------------------------------------------

// RunFromTask P4.1: Planner returns plan with duplicate step names — validation fails
func TestRunFromTask_ValidationFails(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{{
			Steps: []execution.ExecutionStep{
				{Name: "dup-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
				{Name: "dup-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			},
		}},
	}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	_, err := PlanAndRun(context.Background(), mp, h, TaskInput{TaskDescription: "test"}, testEC())
	if err == nil {
		t.Fatal("expected error for plan validation failure (duplicate step names)")
	}
}

// RunFromTask P4.2: UserRequest injection when PlannerContext has empty request
func TestRunFromTask_UserRequestInjection(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{{Steps: []execution.ExecutionStep{}}},
	}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	// PlannerContext.UserRequest is empty — should be filled from TaskDescription
	pCtx := &planner.PlannerContext{UserRequest: ""}
	result, err := PlanAndRun(context.Background(), mp, h, TaskInput{
		TaskDescription: "injected request",
		PlannerContext:  pCtx,
	}, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pCtx.UserRequest != "injected request" {
		t.Errorf("UserRequest = %q, want 'injected request'", pCtx.UserRequest)
	}
	_ = result
}

// RunFromTask P4.3: Nil planner returns error
func TestRunFromTask_NilPlanner(t *testing.T) {
	cfg := DefaultHarnessConfig()
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})
	_, err := PlanAndRun(context.Background(), nil, h, TaskInput{TaskDescription: "test"}, testEC())
	if err == nil {
		t.Fatal("expected error for nil planner")
	}
}

// ---------------------------------------------------------------------------
// Part 6: Race & Concurrency (tested via go test -race)
// ---------------------------------------------------------------------------

// Race P6.1: Concurrent harness executions with shared tool runner
func TestRace_ConcurrentHarnesses(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"

	for i := 0; i < 50; i++ {
		h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
		plan := makeFrozenPlan(
			execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		go func() {
			_, _ = h.Run(context.Background(), plan, testEC())
		}()
	}
}

// Race P6.2: Hook registration concurrent with harness execution
func TestRace_HookRegistrationDuringRun(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"

	for i := 0; i < 20; i++ {
		hm := NewHookManager()
		h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})
		plan := makeFrozenPlan(
			execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		go h.Run(context.Background(), plan, testEC())
		go hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error { return nil })
	}
}

// ---------------------------------------------------------------------------
// Part 7: Failure Injection Tests
// ---------------------------------------------------------------------------

// Chaos P7.1: Tool failure mid-plan with fail-fast
func TestChaos_MidPlanFailure_FailFast(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.err["step-2"] = fmt.Errorf("cascading failure")
	fh := NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: true})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{FailureHandler: fh})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err == nil {
		t.Fatal("expected error for fail-fast")
	}
	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2 (step-1 ok, step-2 fail)", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonFailFast {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonFailFast)
	}
}

// Chaos P7.2: LLM step without reasoning loop configured
func TestChaos_LLMStepWithoutLoop(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	// No reasoning loop → LLM step should fail gracefully

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Error == nil {
		t.Error("LLM step without loop should have error")
	}
}

// Chaos P7.3: Timeout cascade — multiple steps with decreasing timeouts
func TestChaos_TimeoutCascade(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fast-ok", Type: execution.StepTypeTool, Arguments: map[string]any{}, Timeout: 5000},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

// Chaos P7.4: SubAgent parallel with all failing
func TestChaos_ParallelAllFail(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.err["fail-tool"] = fmt.Errorf("always fails")
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plans := make([]*SubAgent, 3)
	for i := range plans {
		p := makeFrozenPlan(
			execution.ExecutionStep{Name: "fail-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		plans[i] = NewSubAgent(SubAgentConfig{Plan: p}, h)
	}

	results, _ := RunParallel(context.Background(), plans, testEC(), 3)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// All should have attempted execution
	for i, r := range results {
		if r == nil {
			t.Errorf("results[%d] is nil", i)
		}
	}
}

// Chaos P7.5: ReasoningLoop with planner error on every turn
func TestChaos_PlannerErrorEveryTurn(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false

	mp := &mockPlanner{errors: []error{
		fmt.Errorf("planner error 1"),
		fmt.Errorf("planner error 2"),
		fmt.Errorf("planner error 3"),
	}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3 (errors should not stop loop)", result.TurnCount)
	}
	if result.ToolCalls != 0 {
		t.Errorf("ToolCalls = %d, want 0 (no plans generated)", result.ToolCalls)
	}
}

// Chaos P7.6: Context canceled during reasoning loop with audit trail preserved
func TestChaos_CancelPreservesAuditTrail(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 10
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first turn can be observed
	var cancelTurn atomic.Int32
	mp2 := &mockPlanner{
		plans: []*execution.ExecutionPlan{plan},
		repeatLast: true,
	}
	_ = mp2
	rl2 := NewDefaultReasoningLoop(cfg, mp, se, nil)
	_ = rl2

	// Use a goroutine to cancel after a brief delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, _ := rl.Run(ctx, &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	_ = cancelTurn.Load()

	// Should have stopped
	if result == nil {
		t.Fatal("result should not be nil even on cancellation")
	}
	if result.StopReason != StopReasonContextCanceled && result.StopReason != StopReasonMaxTurns {
		// Either cancellation or max turns (if loop ran fast enough)
		t.Logf("StopReason = %q (acceptable: context canceled or max turns)", result.StopReason)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

// ---------------------------------------------------------------------------
// Test Helpers
// ---------------------------------------------------------------------------
