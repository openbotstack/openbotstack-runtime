package harness

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// ---------------------------------------------------------------------------
// E2E Replan Tests — Real LLM + 5x abnormal scenarios
// ---------------------------------------------------------------------------

func newRealPlanner(t *testing.T) *planner.LLMPlanner {
	t.Helper()
	apiKey := os.Getenv("OBS_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("OBS_TEST_API_KEY not set, skipping E2E test")
	}
	baseURL := os.Getenv("OBS_TEST_API_URL")
	if baseURL == "" {
		baseURL = "https://vibe.szyjian.com/v1"
	}
	modelName := os.Getenv("OBS_TEST_MODEL")
	if modelName == "" {
		modelName = "Qwen3.6-35B"
	}
	provider := providers.NewOpenAIProvider(baseURL, apiKey, modelName)
	router := &singleProviderRouter{provider: provider}
	return planner.NewLLMPlanner(router, nil)
}

// singleProviderRouter always routes to one provider.
type singleProviderRouter struct {
	provider providers.ModelProvider
}

func (r *singleProviderRouter) Route(caps []aitypes.CapabilityType, constraints aitypes.ModelConstraints) (providers.ModelProvider, error) {
	return r.provider, nil
}
func (r *singleProviderRouter) Register(p providers.ModelProvider) error { return nil }
func (r *singleProviderRouter) List() []string                          { return nil }

// Helper: create tool step
func toolStep(name string) execution.ExecutionStep {
	return execution.ExecutionStep{Name: name, Type: execution.StepTypeTool}
}

// ============================================================================
// NORMAL SCENARIOS (5 tests)
// ============================================================================

// N1: Plan executes without failure, no replan triggered.
func TestE2E_Replan_Normal_NoFailure(t *testing.T) {
	track := &replanTracker{}

	toolRunner := &selectiveToolRunner{
		results: map[string]any{
			"step1": map[string]any{"data": "hello"},
			"step2": map[string]any{"result": "world"},
		},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      track,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("step2"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReplanCount != 0 {
		t.Errorf("expected 0 replans, got %d", result.ReplanCount)
	}
	if track.calls() != 0 {
		t.Errorf("replanner should not be called, got %d calls", track.calls())
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("expected GoalAchieved, got %s", result.StopCondition.Reason)
	}
}

// N2: Step fails, real LLM replan produces working alternative.
func TestE2E_Replan_Normal_FailureRecovery(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results: map[string]any{
			"fetch_data":    map[string]any{"raw": "some data"},
			"fallback_data": map[string]any{"cached": "result"},
		},
		failures: map[string]bool{"process_data": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("fetch_data"), toolStep("process_data"), toolStep("summarize"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Fetch data, process it, and summarize the results",
		Skills: []aitypes.SkillDescriptor{
			{ID: "fallback_data", Name: "Fallback Data", Description: "Use cached/fallback data when primary fails"},
			{ID: "fetch_data", Name: "Fetch Data", Description: "Fetch data from primary source"},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReplanCount < 1 {
		t.Errorf("expected at least 1 replan, got %d", result.ReplanCount)
	}
	if len(result.PlanIDs) < 2 {
		t.Errorf("expected at least 2 PlanIDs, got %d", len(result.PlanIDs))
	}
	t.Logf("N2 PASS: ReplanCount=%d, Steps=%d, PlanIDs=%v, StopReason=%s",
		result.ReplanCount, result.StepsExecuted, result.PlanIDs, result.StopCondition.Reason)
}

// N3: Tool returns needs_replan=true, triggers real LLM replan.
func TestE2E_Replan_Normal_ExplicitSignal(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &signalToolRunner{
		delegate: &selectiveToolRunner{
			results: map[string]any{
				"step1":  map[string]any{"status": "partial"},
				"step2a": map[string]any{"status": "recovered"},
			},
		},
		signalOn: "step1",
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("step2"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Analyze the data and provide results",
		Skills: []aitypes.SkillDescriptor{
			{ID: "step2a", Name: "Step2A", Description: "Alternative analysis approach"},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReplanCount < 1 {
		t.Errorf("expected at least 1 replan from explicit signal, got %d", result.ReplanCount)
	}
	t.Logf("N3 PASS: Explicit signal triggered replan. ReplanCount=%d", result.ReplanCount)
}

// N4: First step fails, replan from beginning.
func TestE2E_Replan_Normal_FirstStepFailure(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"alt_approach": map[string]any{"data": "alternative"}},
		failures: map[string]bool{"primary_fetch": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("primary_fetch"), toolStep("analyze"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Fetch and analyze data",
		Skills:      testSkills(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ReplanCount < 1 {
		t.Errorf("expected replan on first step failure, got %d", result.ReplanCount)
	}
	t.Logf("N4 PASS: First step failure. ReplanCount=%d", result.ReplanCount)
}

// N5: Last step fails, replan completes.
func TestE2E_Replan_Normal_LastStepFailure(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}, "step2": map[string]any{"ok": true}, "final_alt": map[string]any{"result": "done"}},
		failures: map[string]bool{"step3": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("step2"), toolStep("step3"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Execute 3-step pipeline and produce output",
		Skills:      testSkills(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Logf("N5 PASS: Last step failure. ReplanCount=%d, Results=%d", result.ReplanCount, len(result.StepResults))
}

// ============================================================================
// ABNORMAL SCENARIOS (11 tests)
// ============================================================================

// A1: No skills available → replan should fail gracefully.
func TestE2E_Replan_Abnormal_NoSkills(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Impossible task",
		Skills:      []aitypes.SkillDescriptor{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	t.Logf("A1: err=%v, ReplanCount=%d, StopReason=%s", err, result.ReplanCount, result.StopCondition.Reason)
}

// A2: Replan returns invalid JSON (simulated via error).
func TestE2E_Replan_Abnormal_InvalidJSON(t *testing.T) {
	failingReplanner := &errorReplanner{err: fmt.Errorf("invalid json: could not extract plan")}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      failingReplanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A2: err=%v, ReplanCount=%d", err, result.ReplanCount)
	if failingReplanner.calls() != 1 {
		t.Errorf("expected exactly 1 replan attempt, got %d", failingReplanner.calls())
	}
}

// A3: Context cancellation during replan.
func TestE2E_Replan_Abnormal_ContextCancelled(t *testing.T) {
	slow := &slowReplanner{delay: 5 * time.Second}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      slow,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	t.Logf("A3: err=%v, ReplanCount=%d, StopReason=%s", err, result.ReplanCount, result.StopCondition.Reason)
}

// A4: Max replans reached (all tools fail).
func TestE2E_Replan_Abnormal_MaxReplansReached(t *testing.T) {
	realPlanner := newRealPlanner(t)
	toolRunner := &allFailToolRunner{}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("fetch"), toolStep("process"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Fetch and process data",
		Skills:      testSkills(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	t.Logf("A4: err=%v, ReplanCount=%d, StopReason=%s", err, result.ReplanCount, result.StopCondition.Reason)
	if result.ReplanCount > MaxReplansPerSession {
		t.Errorf("replan count exceeded hard cap: got %d, max %d", result.ReplanCount, MaxReplansPerSession)
	}
}

// A5: Nil planner context on EC.
func TestE2E_Replan_Abnormal_NilPlannerContext(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	// Intentionally NOT setting PlannerContext

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A5: err=%v, ReplanCount=%d", err, result.ReplanCount)
}

// A6: Single step plan that fails.
func TestE2E_Replan_Abnormal_SingleStepPlan(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"only_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("only_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Do one thing",
		Skills:      testSkills(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	t.Logf("A6: err=%v, ReplanCount=%d, Results=%d", err, result.ReplanCount, len(result.StepResults))
	if result.ReplanCount < 1 {
		t.Errorf("expected replan on single step failure, got %d", result.ReplanCount)
	}
}

// A7: Replan returns plan with duplicate step names.
func TestE2E_Replan_Abnormal_DuplicateStepNames(t *testing.T) {
	dupReplanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{
				{Name: "dup", Type: execution.StepTypeTool},
				{Name: "dup", Type: execution.StepTypeTool},
			}},
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      dupReplanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A7: err=%v, ReplanCount=%d", err, result.ReplanCount)
	if result.ReplanCount != 0 {
		t.Errorf("replan with duplicate names should be rejected, got ReplanCount=%d", result.ReplanCount)
	}
}

// A8: Replan returns too many steps — harness truncates at MaxSteps.
func TestE2E_Replan_Abnormal_TooManySteps(t *testing.T) {
	steps := make([]execution.ExecutionStep, 15)
	for i := range steps {
		steps[i] = execution.ExecutionStep{Name: fmt.Sprintf("s_%d", i), Type: execution.StepTypeTool}
	}

	bigReplanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{{Steps: steps}},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	cfg := DefaultHarnessConfig()
	// MaxSteps default is 10; the replan returns 15 steps but harness will stop.

	h := NewExecutionHarness(cfg, toolRunner, nil, HarnessDeps{
		Replanner:      bigReplanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A8: err=%v, ReplanCount=%d, StepsExecuted=%d", err, result.ReplanCount, result.StepsExecuted)
	// Replan is accepted (plan.Validate doesn't check step count), but execution is bounded.
	if result.ReplanCount != 1 {
		t.Errorf("expected 1 replan (validator accepts any count), got ReplanCount=%d", result.ReplanCount)
	}
	if result.StepsExecuted > cfg.MaxSteps {
		t.Errorf("steps executed should not exceed MaxSteps=%d, got %d", cfg.MaxSteps, result.StepsExecuted)
	}
}

// A9: Replanner panics — harness should not crash.
func TestE2E_Replan_Abnormal_ReplannerPanics(t *testing.T) {
	panicR := &panicReplanner{}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      panicR,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	// attemptReplan has recover() — harness should not panic.
	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A9: err=%v, ReplanCount=%d, StopReason=%s", err, result.ReplanCount, result.StopCondition.Reason)
	if result.ReplanCount != 0 {
		t.Errorf("panicked replan should not count, got ReplanCount=%d", result.ReplanCount)
	}
}

// A10: Tight session timeout.
func TestE2E_Replan_Abnormal_TightTimeout(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	cfg := DefaultHarnessConfig()
	cfg.MaxSessionRuntime = 2 * time.Second

	h := NewExecutionHarness(cfg, toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(toolStep("step1"), toolStep("fail_step"))
	ec := makeTestEC()
	ec.StartedAt = time.Now()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Quick task",
		Skills:      testSkills(),
	})

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A10: err=%v, ReplanCount=%d, StopReason=%s", err, result.ReplanCount, result.StopCondition.Reason)
}

// A11: Plan with no ID (frozen without Validate).
func TestE2E_Replan_Abnormal_PlanNoID(t *testing.T) {
	realPlanner := newRealPlanner(t)

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"step1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      realPlanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{toolStep("step1"), toolStep("fail_step")},
	}
	plan.Freeze()

	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{
		AssistantID: "test-asst",
		UserRequest: "Test task",
		Skills:      testSkills(),
	})

	result, err := h.Run(context.Background(), plan, ec)
	t.Logf("A11: err=%v, ReplanCount=%d, PlanID=%s", err, result.ReplanCount, result.PlanID)
}

// ============================================================================
// HELPER TYPES
// ============================================================================

// replanTracker counts replan calls but always returns error.
type replanTracker struct {
	count int32
}

func (r *replanTracker) Replan(ctx context.Context, rCtx *planner.ReplanContext) (*execution.ExecutionPlan, error) {
	atomic.AddInt32(&r.count, 1)
	return nil, fmt.Errorf("tracker: unexpected call")
}
func (r *replanTracker) calls() int32 { return atomic.LoadInt32(&r.count) }

// signalToolRunner returns needs_replan=true for specific steps.
type signalToolRunner struct {
	delegate toolrunner.ToolRunner
	signalOn string
}

func (s *signalToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	if name == s.signalOn {
		return &execution.StepResult{
			StepName: name,
			Type:     "tool",
			Output:   map[string]any{"needs_replan": true, "partial_data": "some value"},
		}, nil
	}
	return s.delegate.Execute(ctx, name, input, ec)
}

// allFailToolRunner fails every tool call.
type allFailToolRunner struct{}

func (a *allFailToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	err := fmt.Errorf("tool %q failed: connection refused", name)
	return &execution.StepResult{StepName: name, Type: "tool", Error: err}, err
}

// errorReplanner always returns the configured error.
type errorReplanner struct {
	mu       sync.Mutex
	err      error
	callCnt  int
}

func (e *errorReplanner) Replan(ctx context.Context, rCtx *planner.ReplanContext) (*execution.ExecutionPlan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callCnt++
	return nil, e.err
}
func (e *errorReplanner) calls() int { e.mu.Lock(); defer e.mu.Unlock(); return e.callCnt }

// slowReplanner delays before returning.
type slowReplanner struct {
	delay time.Duration
}

func (s *slowReplanner) Replan(ctx context.Context, rCtx *planner.ReplanContext) (*execution.ExecutionPlan, error) {
	select {
	case <-time.After(s.delay):
		return makeValidPlan(toolStep("slow_result")), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// panicReplanner panics on every call.
type panicReplanner struct{}

func (p *panicReplanner) Replan(ctx context.Context, rCtx *planner.ReplanContext) (*execution.ExecutionPlan, error) {
	panic("replanner internal error: unexpected state")
}
