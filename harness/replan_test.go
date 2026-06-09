package harness

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// mockReplanner implements planner.Replanner for testing.
type mockReplanner struct {
	mu       sync.Mutex
	plans    []*execution.ExecutionPlan
	callIdx  int
	errors   []error
	captured []*planner.ReplanContext
}

func (mr *mockReplanner) Replan(ctx context.Context, rCtx *planner.ReplanContext) (*execution.ExecutionPlan, error) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if mr.captured == nil {
		mr.captured = make([]*planner.ReplanContext, 0)
	}
	mr.captured = append(mr.captured, rCtx)

	idx := mr.callIdx
	mr.callIdx++

	if idx < len(mr.errors) && mr.errors[idx] != nil {
		return nil, mr.errors[idx]
	}
	if idx < len(mr.plans) {
		return mr.plans[idx], nil
	}
	return nil, fmt.Errorf("mock replanner: no plan configured for call %d", idx)
}

func (mr *mockReplanner) callCount() int {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	return mr.callIdx
}

// helper: create a validated plan with the given steps.
func makeValidPlan(steps ...execution.ExecutionStep) *execution.ExecutionPlan {
	plan := &execution.ExecutionPlan{Steps: steps}
	_ = plan.Validate()
	return plan
}

// helper: create a basic execution context.
func makeTestEC() *execution.ExecutionContext {
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	ec.StartedAt = time.Now()
	return ec
}

// helper: create a step that always fails.
func failingToolStep(name string) execution.ExecutionStep {
	return execution.ExecutionStep{Name: name, Type: execution.StepTypeTool}
}

// helper: create a step that always succeeds.
func succeedingStep(name string) execution.ExecutionStep {
	return execution.ExecutionStep{Name: name, Type: execution.StepTypeTool}
}

// helper: minimal skills list for PlannerContext.
func testSkills() []aitypes.SkillDescriptor {
	return []aitypes.SkillDescriptor{
		{ID: "recovery", Name: "Recovery", Description: "Recovery skill"},
		{ID: "new_step", Name: "New Step", Description: "New step"},
		{ID: "recovery_step", Name: "Recovery Step", Description: "Recovery step"},
	}
}

// -- Tests --

func TestReplan_DisabledWhenReplannerNil(t *testing.T) {
	toolRunner := &failingToolRunner{err: fmt.Errorf("boom")}
	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		FailureHandler: NewFailureHandler(execution.DefaultRetryPolicy()),
	})

	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
		succeedingStep("s3"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{})

	_, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error from failing step")
	}
}

func TestReplan_OnToolFailure(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery_step")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results: map[string]any{
			"s1":            map[string]any{"ok": true},
			"recovery_step": map[string]any{"status": "recovered"},
		},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
		succeedingStep("s3"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replanner.callCount() != 1 {
		t.Errorf("expected 1 replan call, got %d", replanner.callCount())
	}
	if result.ReplanCount != 1 {
		t.Errorf("expected ReplanCount=1, got %d", result.ReplanCount)
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("expected GoalAchieved, got %s", result.StopCondition.Reason)
	}
}

func TestReplan_ReplacesRemainingSteps(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(execution.ExecutionStep{Name: "new_step", Type: execution.StepTypeTool}),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "new_step": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	// 4-step plan, step 2 (index 1) fails, replan returns 1 step.
	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
		succeedingStep("s3"),
		succeedingStep("s4"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should execute: s1, fail_step(failed/recovered), new_step = 3 step results
	if len(result.StepResults) != 3 {
		t.Errorf("expected 3 step results, got %d", len(result.StepResults))
	}
}

func TestReplan_RespectsMaxLimit(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(failingToolStep("fail_again")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true, "fail_again": true},
	}

	cfg := DefaultHarnessConfig()
	h := NewExecutionHarness(cfg, toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})
	h.replanConfig.MaxReplans = 1

	plan := makeValidPlan(
		failingToolStep("fail_step"),
		succeedingStep("never_reached"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	// Replan fires once (fail_step → fail_again). MaxReplans=1 prevents a second replan.
	// fail_again still fails, so execution may error or continue depending on remaining steps.
	if replanner.callCount() != 1 {
		t.Errorf("expected exactly 1 replan call, got %d", replanner.callCount())
	}
	if result.ReplanCount != 1 {
		t.Errorf("expected ReplanCount=1, got %d", result.ReplanCount)
	}
	_ = err // error expected since fail_again also fails with no more replans
}

func TestReplan_DoesNotMutateOriginalPlan(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
		succeedingStep("s3"),
	)
	originalStepCount := len(plan.Steps)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Steps) != originalStepCount {
		t.Errorf("original plan steps mutated: had %d, now %d", originalStepCount, len(plan.Steps))
	}
}

func TestReplan_SetsParentID(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(replanner.captured) == 0 {
		t.Fatal("expected replan to be called")
	}
	captured := replanner.captured[0]
	if captured.OriginalPlan == nil {
		t.Fatal("OriginalPlan should not be nil")
	}
	if captured.OriginalPlan.ID != plan.ID {
		t.Errorf("OriginalPlan.ID = %q, want %q", captured.OriginalPlan.ID, plan.ID)
	}
}

func TestReplan_PlanLineage(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(
		succeedingStep("s1"),
		failingToolStep("fail_step"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.PlanIDs) < 2 {
		t.Errorf("expected at least 2 PlanIDs (original + replan), got %d", len(result.PlanIDs))
	}
	if result.PlanIDs[0] != plan.ID {
		t.Errorf("first PlanID should be original, got %q", result.PlanIDs[0])
	}
}

func TestReplan_Fails_FallsThrough(t *testing.T) {
	replanner := &mockReplanner{
		errors: []error{fmt.Errorf("replan LLM unavailable")},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: true}),
	})

	plan := makeValidPlan(
		failingToolStep("fail_step"),
	)
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	_, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error when replan fails")
	}
}

func TestReplan_EmitsAudit(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	var loggedEvents []audit.AuditEvent
	auditLogger := &capturingAuditLogger{events: &loggedEvents}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})
	h.auditLogger = auditLogger

	plan := makeValidPlan(succeedingStep("s1"), failingToolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var replanEvent *audit.AuditEvent
	for i := range loggedEvents {
		if loggedEvents[i].Source == audit.SourceReplan {
			replanEvent = &loggedEvents[i]
			break
		}
	}
	if replanEvent == nil {
		t.Fatal("expected replan audit event")
	}
	if replanEvent.Action != "harness.replan" {
		t.Errorf("expected action=harness.replan, got %s", replanEvent.Action)
	}
	if replanEvent.Metadata["original_plan_id"] != plan.ID {
		t.Errorf("expected original_plan_id=%s, got %s", plan.ID, replanEvent.Metadata["original_plan_id"])
	}
	if replanEvent.Metadata["new_plan_id"] == "" {
		t.Error("expected non-empty new_plan_id")
	}
	if replanEvent.Metadata["failed_step"] != "fail_step" {
		t.Errorf("expected failed_step=fail_step, got %s", replanEvent.Metadata["failed_step"])
	}
}

func TestReplan_AuditIncludesTrigger(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			makeValidPlan(succeedingStep("recovery")),
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}, "recovery": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	var loggedEvents []audit.AuditEvent
	auditLogger := &capturingAuditLogger{events: &loggedEvents}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})
	h.auditLogger = auditLogger

	plan := makeValidPlan(succeedingStep("s1"), failingToolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var replanEvent *audit.AuditEvent
	for i := range loggedEvents {
		if loggedEvents[i].Source == audit.SourceReplan {
			replanEvent = &loggedEvents[i]
			break
		}
	}
	if replanEvent == nil {
		t.Fatal("expected replan audit event")
	}
	trigger := replanEvent.Metadata["trigger"]
	if trigger != string(planner.ReplanTriggerToolFailure) {
		t.Errorf("expected trigger=%s, got %s", planner.ReplanTriggerToolFailure, trigger)
	}
}

func TestReplan_EmptyPlan_FallsThrough(t *testing.T) {
	replanner := &mockReplanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{}},
		},
	}

	toolRunner := &selectiveToolRunner{
		results:  map[string]any{"s1": map[string]any{"ok": true}},
		failures: map[string]bool{"fail_step": true},
	}

	h := NewExecutionHarness(DefaultHarnessConfig(), toolRunner, nil, HarnessDeps{
		Replanner:      replanner,
		FailureHandler: NewFailureHandler(execution.RetryPolicy{MaxRetries: 0, FailFast: false}),
	})

	plan := makeValidPlan(succeedingStep("s1"), failingToolStep("fail_step"))
	ec := makeTestEC()
	ec.SetPlannerContext(&planner.PlannerContext{Skills: testSkills()})

	result, err := h.Run(context.Background(), plan, ec)
	// Empty plan from replanner → replan fails → fall through to normal handling
	if result.ReplanCount != 0 {
		t.Errorf("expected ReplanCount=0 (empty plan rejected), got %d", result.ReplanCount)
	}
	_ = err
}

// capturingAuditLogger captures audit events for test assertions.
type capturingAuditLogger struct {
	events *[]audit.AuditEvent
}

func (c *capturingAuditLogger) Log(ctx context.Context, event audit.AuditEvent) error {
	*c.events = append(*c.events, event)
	return nil
}
func (c *capturingAuditLogger) Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	return nil, nil
}
func (c *capturingAuditLogger) Count(ctx context.Context, filter execution_logs.QueryFilter) (int, error) {
	return len(*c.events), nil
}

// -- Mock helpers --

// failingToolRunner always returns an error.
type failingToolRunner struct {
	err error
}

func (f *failingToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	return &execution.StepResult{StepName: name, Type: "tool", Error: f.err}, f.err
}

// selectiveToolRunner returns success or failure based on the step name.
type selectiveToolRunner struct {
	results  map[string]any
	failures map[string]bool
}

func (s *selectiveToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	if s.failures[name] {
		err := fmt.Errorf("tool %q failed", name)
		return &execution.StepResult{StepName: name, Type: "tool", Error: err}, err
	}
	if result, ok := s.results[name]; ok {
		return &execution.StepResult{StepName: name, Type: "tool", Output: result}, nil
	}
	err := fmt.Errorf("tool %q not found", name)
	return &execution.StepResult{StepName: name, Type: "tool", Error: err}, err
}
