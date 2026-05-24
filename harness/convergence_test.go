package harness

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// --- Phase 2: Convergence Testing ---
//
// Verify system ALWAYS terminates under every stop condition.
// No infinite loops, no near-infinite loops, bounded by max limits.

// Convergence 1: Always terminates via MaxTurns
func TestConvergence_MaxTurns_Terminates(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false

	// Planner always returns a non-empty plan → loop must stop at MaxTurns
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{makeToolPlan("tool-a")}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	start := time.Now()
	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount > 3 {
		t.Errorf("TurnCount = %d, want <= 3 (MaxTurns)", result.TurnCount)
	}
	if result.StopReason != StopReasonMaxTurns {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurns)
	}
	// Must complete quickly (no real work, just mock calls)
	if elapsed > 1*time.Second {
		t.Errorf("elapsed = %v, should be < 1s for mock execution", elapsed)
	}
}

// Convergence 2: Always terminates via MaxToolCalls
func TestConvergence_MaxToolCalls_Terminates(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 100
	cfg.MaxToolCalls = 5
	cfg.RepeatPlanStop = false

	// Each turn has 3 tool steps → budget exhausted during turn 2
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "t1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "t2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "t3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ToolCalls > 5 {
		t.Errorf("ToolCalls = %d, want <= 5 (MaxToolCalls)", result.ToolCalls)
	}
	if result.StopReason != StopReasonMaxToolCalls {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxToolCalls)
	}
}

// Convergence 3: Always terminates via context cancellation
func TestConvergence_ContextCancel_Terminates(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 100
	cfg.MaxToolCalls = 100
	cfg.RepeatPlanStop = false

	// Use a slow planner that takes 200ms per call so cancel can interrupt it
	mp := &slowMockPlanner{delay: 200 * time.Millisecond, plan: makeToolPlan("tool-a")}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after 50ms — should interrupt the slow planner
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := rl.Run(ctx, &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.StopReason != StopReasonContextCanceled {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonContextCanceled)
	}
}

// Convergence 4: Always terminates via planner stops
func TestConvergence_PlannerStops_Terminates(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 100

	// Planner returns plan on turns 1-2, empty on turn 3
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{
		makeToolPlan("tool-a"),
		makeToolPlan("tool-b"),
		{Steps: []execution.ExecutionStep{}}, // planner stops
	}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TurnCount > 3 {
		t.Errorf("TurnCount = %d, want <= 3", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}
}

// Convergence 5: Always terminates via MaxSessionRuntime
func TestConvergence_MaxSessionRuntime_Terminates(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 100
	cfg.MaxSessionRuntime = 100 * time.Millisecond

	// Slow tool runner — each call takes 30ms
	tr := &slowToolRunner{delay: 30 * time.Millisecond}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	// 10-step plan — should be stopped by session runtime after ~3-4 steps
	steps := make([]execution.ExecutionStep, 10)
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
	// Should NOT have executed all 10 steps
	if result.StepsExecuted >= 10 {
		t.Errorf("StepsExecuted = %d, should be < 10 (session runtime limit)", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonMaxSessionRuntime {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonMaxSessionRuntime)
	}
}

// Convergence 6: Duration bounded by config
func TestConvergence_DurationBounded(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 5
	cfg.MaxTurnRuntime = 200 * time.Millisecond
	cfg.RepeatPlanStop = false

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{makeToolPlan("tool-a")}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 5 turns * 200ms max = 1s ceiling, with generous margin
	maxExpected := time.Duration(cfg.MaxTurns) * cfg.MaxTurnRuntime * 2
	if result.Duration > maxExpected {
		t.Errorf("Duration = %v, expected <= %v", result.Duration, maxExpected)
	}
	if result.TurnCount > cfg.MaxTurns {
		t.Errorf("TurnCount = %d, MaxTurns = %d", result.TurnCount, cfg.MaxTurns)
	}
}

// Convergence 7: Safety valve — 2x MaxTurns hard limit
func TestConvergence_SafetyValve(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{makeToolPlan("tool-a")}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Even with safety valve, should never exceed 2x MaxTurns = 6
	absoluteMax := 2 * cfg.MaxTurns
	if result.TurnCount > absoluteMax {
		t.Errorf("TurnCount = %d, exceeds safety valve limit of 2*MaxTurns = %d", result.TurnCount, absoluteMax)
	}
}

// Convergence 8: Race condition stress test — 100 concurrent executions
func TestConcurrency_100ParallelExecutions(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 5

	tr := newMockToolRunner()
	tr.result["step-a"] = "result-a"
	tr.result["step-b"] = "result-b"

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	allCompleted := make(chan struct{})

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
			plan := makeFrozenPlan(
				execution.ExecutionStep{Name: "step-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
				execution.ExecutionStep{Name: "step-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			)
			ec := testEC()

			result, err := h.Run(context.Background(), plan, ec)
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %v", idx, err)
				return
			}
			if result.StepsExecuted != 2 {
				errors <- fmt.Errorf("goroutine %d: StepsExecuted=%d, want 2", idx, result.StepsExecuted)
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(allCompleted)
	}()

	select {
	case <-allCompleted:
		// All completed
	case err := <-errors:
		t.Fatalf("concurrent execution failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent executions did not complete within 10s — possible deadlock")
	}

	// Check for any accumulated errors
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

// Convergence 9: ExecutionHarness always terminates via context cancel
func TestConvergence_HarnessContextCancel(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 100

	tr := &slowToolRunner{delay: 50 * time.Millisecond}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	steps := make([]execution.ExecutionStep, 20)
	for i := range steps {
		steps[i] = execution.ExecutionStep{
			Name: fmt.Sprintf("step-%d", i), Type: execution.StepTypeTool, Arguments: map[string]any{},
		}
	}
	plan := makeFrozenPlan(steps...)
	ec := testEC()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond) // cancel after ~1-2 steps
		cancel()
	}()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonContextCanceled {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonContextCanceled)
	}
	// Should not have executed all 20 steps
	if result.StepsExecuted >= 20 {
		t.Errorf("StepsExecuted = %d, should be < 20 (context cancelled)", result.StepsExecuted)
	}
}

// --- Convergence test helpers ---

// slowMockPlanner wraps a plan with a configurable delay per call.
type slowMockPlanner struct {
	delay time.Duration
	plan  *execution.ExecutionPlan
}

func (s *slowMockPlanner) Plan(ctx context.Context, pCtx *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	select {
	case <-time.After(s.delay):
		return s.plan, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// slowToolRunner adds a configurable delay to each execution.
type slowToolRunner struct {
	delay  time.Duration
	calls  int
}

func (s *slowToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	s.calls++
	select {
	case <-time.After(s.delay):
		return &execution.StepResult{StepName: toolName, Output: fmt.Sprintf("result-%s", toolName)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowToolRunner) callCount() int { return s.calls }

// Ensure unused imports are satisfied (same package)
var _ planner.ExecutionPlanner = (*mockPlanner)(nil)
