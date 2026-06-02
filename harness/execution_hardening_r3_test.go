package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/control/policy"
	"github.com/openbotstack/openbotstack-core/execution"
)

// ========================================================================
// Phase 4.6 Round 3 — Execution Hardening
// ========================================================================
//
// Targets remaining P2 gaps:
//   1. permission.go:Check — policy.Enforcer layer (70% → target 100%)
//   2. audit.go:RecordStep — logger failure path (75% → 100%)
//   3. subagent.go:RunParallel — error collection edge (85.7% → 100%)
//   4. hooks.go:PreToolUse/PostToolUse — error path coverage (85-90% → 100%)
//   5. reasoning_loop.go:Run — compaction + planner error combined path
//   6. step_executor.go:ExecuteSkill — failed status path
//   7. harness.go:Run — all remaining branches

// ---------------------------------------------------------------------------
// Part 3 Deep: Policy Enforcer Integration
// ---------------------------------------------------------------------------

// Enforcer P3.5: Real policy.Enforcer denies tool execution
func TestPolicy_EnforcerDenyWithRealEnforcer(t *testing.T) {
	enforcer := policy.NewEnforcer()
	enforcer.AddRule(policy.PolicyRule{
		ID:       "deny-sensitive",
		TenantID: "tenant-1",
		Effect:   "deny",
		Action:   "skill.execute",
		Resource: "skill/secret-tool",
		Priority: 10,
	})

	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["secret-tool"] = "classified"
	pc := NewPermissionChecker(nil, enforcer)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "secret-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonHookDenied {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonHookDenied)
	}
	if result.StepsExecuted != 0 {
		t.Errorf("StepsExecuted = %d, want 0 (denied by policy)", result.StepsExecuted)
	}
}

// Enforcer P3.6: Real enforcer allows non-matching tool
func TestPolicy_EnforcerAllowsNonMatching(t *testing.T) {
	enforcer := policy.NewEnforcer()
	enforcer.AddRule(policy.PolicyRule{
		ID:       "deny-secret",
		TenantID: "tenant-1",
		Effect:   "deny",
		Action:   "skill.execute",
		Resource: "skill/secret-tool",
		Priority: 10,
	})

	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["safe-tool"] = "ok"
	pc := NewPermissionChecker(nil, enforcer)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "safe-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1 (allowed by policy)", result.StepsExecuted)
	}
}

// Enforcer P3.7: Enforcer with allow-override rule
func TestPolicy_EnforcerAllowOverrideDeny(t *testing.T) {
	enforcer := policy.NewEnforcer()
	// Deny all tools for tenant
	enforcer.AddRule(policy.PolicyRule{
		ID: "deny-all", TenantID: "tenant-1", Effect: "deny",
		Action: "*", Resource: "*", Priority: 1,
	})
	// Allow specific tool (higher priority overrides)
	enforcer.AddRule(policy.PolicyRule{
		ID: "allow-safe", TenantID: "tenant-1", Effect: "allow",
		Action: "skill.execute", Resource: "skill/safe-tool", Priority: 10,
	})

	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["safe-tool"] = "ok"
	pc := NewPermissionChecker(nil, enforcer)
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{PermChecker: pc})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "safe-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1 (allow override)", result.StepsExecuted)
	}
}

// Enforcer P3.8: PermissionCheck with empty tenantID skips enforcer
func TestPolicy_EmptyTenantIDSkipsEnforcer(t *testing.T) {
	enforcer := policy.NewEnforcer()
	enforcer.AddRule(policy.PolicyRule{
		ID: "deny-all", TenantID: "tenant-1", Effect: "deny",
		Action: "*", Resource: "*", Priority: 10,
	})

	pc := NewPermissionChecker(nil, enforcer)
	// Empty tenantID → enforcer is skipped
	err := pc.Check(context.Background(), "any-tool", "")
	if err != nil {
		t.Errorf("empty tenantID should skip enforcer: %v", err)
	}
}

// Enforcer P3.9: Nil enforcer + nil config = allow all
func TestPolicy_NilBothAllowsAll(t *testing.T) {
	pc := NewPermissionChecker(nil, nil)
	if err := pc.Check(context.Background(), "any-tool", "tenant-1"); err != nil {
		t.Errorf("nil config + nil enforcer should allow all: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Audit: RecordStep basic paths
// ---------------------------------------------------------------------------

// Audit P5.4: RecordStep succeeds
func TestAudit_RecordStep_Succeeds(t *testing.T) {
	al := NewAuditLayer()

	err := al.RecordStep(context.Background(), audit.AuditEvent{
		StepName: "test-step",
		StepType: string(execution.StepTypeTool),
		Status:   "success",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if al.TrailSize() != 1 {
		t.Errorf("TrailSize = %d, want 1", al.TrailSize())
	}
}

// Audit P5.5: RecordStep auto-generates TraceID and Timestamp
func TestAudit_RecordStep_AutoGeneratedFields(t *testing.T) {
	al := NewAuditLayer()

	err := al.RecordStep(context.Background(), audit.AuditEvent{
		StepName: "test-step",
		StepType: string(execution.StepTypeTool),
		Status:   "success",
		// TraceID and Timestamp left empty — should be auto-generated
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trail := al.Trail()
	if trail[0].TraceID == "" {
		t.Error("TraceID should be auto-generated when empty")
	}
	if trail[0].Timestamp.IsZero() {
		t.Error("Timestamp should be auto-generated when zero")
	}
}

// ---------------------------------------------------------------------------
// Hook Error Paths
// ---------------------------------------------------------------------------

// Hook R3.1: PreToolUse hook error for tool step
func TestHook_PreToolUseHookError_ReturnsError(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	hm := NewHookManager()
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return nil, fmt.Errorf("pre-tool hook runtime error")
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, _ := h.Run(context.Background(), plan, testEC())
	// Tool should not execute — error recorded
	if len(result.StepResults) != 1 {
		t.Fatalf("StepResults count = %d, want 1", len(result.StepResults))
	}
	if result.StepResults[0].Error == nil {
		t.Error("expected error from PreToolUse hook failure")
	}
}

// Hook R3.2: PostToolUse hook error is logged but non-fatal
func TestHook_PostToolUseHookError_NonFatal(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	hm := NewHookManager()
	hm.RegisterPostToolUse(func(ctx context.Context, hctx *execution.HookContext) error {
		return fmt.Errorf("post-tool hook runtime error")
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("post-tool hook error should not fail harness: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
}

// Hook R3.3: PreToolUse deny for skill step
func TestHook_PreToolUseDeny_SkillStep(t *testing.T) {
	cfg := DefaultHarnessConfig()
	skillExec := &mockSkillExecutor{resp: []byte(`{"ok":true}`)}
	hm := NewHookManager()
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return &execution.HookResult{Deny: true, Reason: "skill not allowed"}, nil
	})
	h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "denied-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
	)
	result, _ := h.Run(context.Background(), plan, testEC())
	if result.StepResults[0].Error == nil {
		t.Error("expected error from PreToolUse deny for skill")
	}
	if skillExec.callCount() != 0 {
		t.Error("skill should not execute when denied by hook")
	}
}

// ---------------------------------------------------------------------------
// SubAgent Edge Cases
// ---------------------------------------------------------------------------

// SubAgent R3.1: RunParallel with context cancellation returns results
func TestSubAgent_RunParallel_ContextCancel(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	subs := []*SubAgent{
		NewSubAgent(SubAgentConfig{Plan: plan}, h),
		NewSubAgent(SubAgentConfig{Plan: plan}, h),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	results, _ := RunParallel(ctx, subs, testEC(), 2)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Both should have context error or completed before cancel
	for i, r := range results {
		if r == nil {
			t.Errorf("results[%d] is nil", i)
		}
	}
}

// SubAgent R3.2: Run extracts final output from last step
func TestSubAgent_Run_ExtractsFinalOutput(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["step-1"] = "intermediate"
	tr.result["step-2"] = "final-answer"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	sa := NewSubAgent(SubAgentConfig{Plan: plan}, h)
	result, err := sa.Run(context.Background(), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "final-answer" {
		t.Errorf("Output = %v, want 'final-answer'", result.Output)
	}
	if result.StepsRun != 2 {
		t.Errorf("StepsRun = %d, want 2", result.StepsRun)
	}
}

// ---------------------------------------------------------------------------
// ExecuteSkill Edge Cases
// ---------------------------------------------------------------------------

// Skill R3.1: Skill execution with error from skill executor
func TestSkill_ExecutionWithFailedStatus(t *testing.T) {
	cfg := DefaultHarnessConfig()
	skillExec := &mockSkillExecutor{
		err: fmt.Errorf("skill processing failed"),
	}
	h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "failing-skill", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Error == nil {
		t.Error("expected error from failed skill execution")
	}
}

// ---------------------------------------------------------------------------
// ReasoningLoop Combined Paths
// ---------------------------------------------------------------------------

// RL R3.1: Compaction triggered and loop continues
func TestReasoningLoop_CompactionThenContinue(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 6
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	var compactCalled bool
	compactor := &mockCompactor{compactFn: func(ctx context.Context, results []TurnResult) ([]TurnResult, error) {
		compactCalled = true
		if len(results) <= 3 {
			return results, nil
		}
		return append([]TurnResult{results[0]}, results[len(results)-2:]...), nil
	}}

	rl := NewDefaultReasoningLoop(cfg, mp, se, compactor)
	pCtx := &planner.PlannerContext{
		UserRequest:   "test",
		MemoryContext: []planner.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Compaction reduces turn history which may affect planner context
	// The key assertion: loop terminates and compaction was called
	if result.TurnCount < 3 {
		t.Errorf("TurnCount = %d, want >= 3", result.TurnCount)
	}
	if !compactCalled {
		t.Error("expected compaction to be triggered")
	}
	if result.StopReason == "" {
		t.Error("StopReason should not be empty")
	}
}

// RL R3.2: Compaction error is logged but doesn't stop loop
func TestReasoningLoop_CompactionErrorDoesNotStopLoop(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 4
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	compactor := &mockCompactor{compactFn: func(ctx context.Context, results []TurnResult) ([]TurnResult, error) {
		return nil, fmt.Errorf("compaction service unavailable")
	}}

	rl := NewDefaultReasoningLoop(cfg, mp, se, compactor)
	pCtx := testPCtx("test")

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount != 4 {
		t.Errorf("TurnCount = %d, want 4 (compaction error should not stop loop)", result.TurnCount)
	}
}

// ---------------------------------------------------------------------------
// Harness.go Remaining Branches
// ---------------------------------------------------------------------------

// Harness R3.1: PreStepExecute hook error returns nil result
func TestHarness_PreStepHookError_ReturnsNilResult(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	hm := NewHookManager()
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return nil, fmt.Errorf("hook runtime crash")
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	_, err := h.Run(context.Background(), plan, testEC())
	if err == nil {
		t.Fatal("expected error from pre-step hook failure")
	}
}

// Harness R3.2: Multiple steps with mixed success and no hook manager
func TestHarness_MultipleSteps_NoHookManager(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.result["step-2"] = "ok"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	// No hook manager — should still work fine

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	result, err := h.Run(context.Background(), plan, testEC())
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

// Harness R3.3: HarnessMetrics TotalRuntime defaults to zero
func TestHarness_Metrics_TotalRuntimeDefault(t *testing.T) {
	result := &HarnessResult{}
	if result.Metrics.TotalRuntime != 0 {
		t.Error("TotalRuntime default should be 0")
	}
}

// ---------------------------------------------------------------------------
// FailureHandler Remaining Branches
// ---------------------------------------------------------------------------

// Failure R3.1: Handle with context cancellation during retry
func TestFailure_Handle_ContextCancellation(t *testing.T) {
	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:     5,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     5 * time.Second,
		FailFast:       false,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	step := &execution.ExecutionStep{
		StepID: "step-1", Name: "test-step", Type: execution.StepTypeTool,
		Arguments: map[string]any{},
	}

	_, err := fh.Handle(ctx, step, fmt.Errorf("initial error"), func() (*execution.StepResult, error) {
		return nil, fmt.Errorf("retry error")
	})

	if err == nil {
		t.Fatal("expected error from context cancellation")
	}
	if ctx.Err() == nil {
		t.Error("context should be cancelled")
	}
}
