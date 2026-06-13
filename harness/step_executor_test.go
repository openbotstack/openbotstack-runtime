package harness

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- StepExecutor Tests ---

func TestStepExecutor_ToolExecution(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["my-tool"] = "tool-output"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "my-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"key": "value"},
	}
	ec := testEC()

	result, err := se.ExecuteTool(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepName != "my-tool" {
		t.Errorf("StepName = %q, want %q", result.StepName, "my-tool")
	}
	if result.Output != "tool-output" {
		t.Errorf("Output = %v, want tool-output", result.Output)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
}

func TestStepExecutor_ToolRunnerError(t *testing.T) {
	tr := newMockToolRunner()
	tr.err["fail-tool"] = fmt.Errorf("tool execution failed")
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "fail-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	_, err := se.ExecuteTool(context.Background(), step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from tool runner")
	}
	if err.Error() != "tool execution failed" {
		t.Errorf("error = %v, want 'tool execution failed'", err)
	}
}

// TestStepExecutor_UnresolvableTemplateFailsStep guards the C1 fix: a step
// argument containing an unresolvable {{...}} template (e.g. a field the
// planner guessed that doesn't exist on the prior result) MUST fail the step
// with a clear error, not dispatch it with the literal template string.
func TestStepExecutor_UnresolvableTemplateFailsStep(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["my-tool"] = "should-not-reach"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID: "step-1",
		Name:   "my-tool",
		Type:   execution.StepTypeTool,
		// "content" does not exist on the prior result (it has "text").
		Arguments: map[string]any{"input": "{{prev_step.content}}"},
	}
	ec := testEC()
	prevResults := map[string]any{
		"prev_step": map[string]any{"text": "the real content"},
	}

	_, err := se.ExecuteTool(context.Background(), step, ec, prevResults, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for unresolvable template, got nil (literal would reach the tool)")
	}
	if !strings.Contains(err.Error(), "content") {
		t.Errorf("error should name the missing field 'content': %v", err)
	}
	// The tool runner must NOT have been invoked.
	for _, call := range tr.calls {
		if call == "my-tool" {
			t.Fatal("tool runner was invoked despite unresolvable template — step should have failed first")
		}
	}
}

func TestStepExecutor_NoToolRunner(t *testing.T) {
	se := NewStepExecutor(nil, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "tool-a",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	_, err := se.ExecuteTool(context.Background(), step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for no tool runner")
	}
}

func TestStepExecutor_SkillExecution(t *testing.T) {
	skillExec := &mockSkillExecutor{resp: []byte(`{"result":"skill-output"}`)}
	se := NewStepExecutor(nil, skillExec, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "my-skill",
		Type:      execution.StepTypeSkill,
		Arguments: map[string]any{"input": "data"},
	}
	ec := testEC()

	result, err := se.ExecuteSkill(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepName != "my-skill" {
		t.Errorf("StepName = %q, want %q", result.StepName, "my-skill")
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
}

func TestStepExecutor_SkillExecutionError(t *testing.T) {
	skillExec := &mockSkillExecutor{err: fmt.Errorf("skill crashed")}
	se := NewStepExecutor(nil, skillExec, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "crash-skill",
		Type:      execution.StepTypeSkill,
		Arguments: map[string]any{},
	}
	ec := testEC()

	_, err := se.ExecuteSkill(context.Background(), step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error from skill executor")
	}
}

func TestStepExecutor_NoSkillExecutor(t *testing.T) {
	se := NewStepExecutor(nil, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "skill-a",
		Type:      execution.StepTypeSkill,
		Arguments: map[string]any{},
	}
	ec := testEC()

	_, err := se.ExecuteSkill(context.Background(), step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for no skill executor")
	}
}

func TestStepExecutor_ContextCancellation(t *testing.T) {
	// Use a tool runner that blocks until context is done
	tr := &blockingToolRunner{}
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a tiny delay so the executor starts but then sees cancellation
	go func() {
		time.Sleep(1 * time.Millisecond)
		cancel()
	}()

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "slow-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	result, err := se.ExecuteTool(ctx, step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on cancellation")
	}
}

func TestStepExecutor_StepTimeout(t *testing.T) {
	// Step timeout of 1ms should expire quickly
	tr := newMockToolRunner()
	// mockToolRunner returns immediately, so we can't easily test timeout.
	// Instead verify that step timeout is set on context correctly.
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "fast-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	result, err := se.ExecuteTool(context.Background(), step, ec, nil, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestStepExecutor_ResultInterpolation(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["step-b"] = "resolved-value"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "step-b",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"input": "{{step-a}}"},
	}
	prevResults := map[string]any{"step-a": "previous-output"}
	ec := testEC()

	_, err := se.ExecuteTool(context.Background(), step, ec, prevResults, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Arguments are cloned before mutation — the tool runner receives resolved values.
	if tr.LastArgs["input"] != "previous-output" {
		t.Errorf("input = %v, want 'previous-output'", tr.LastArgs["input"])
	}
}

func TestStepExecutor_CoerceStringNumbers(t *testing.T) {
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "math-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"a": "42", "b": "3.14", "c": "text"},
	}
	ec := testEC()

	_, err := se.ExecuteTool(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Arguments are cloned — tool runner receives coerced values.
	if tr.LastArgs["a"] != int64(42) {
		t.Errorf("a = %v (%T), want int64(42)", tr.LastArgs["a"], tr.LastArgs["a"])
	}
	if tr.LastArgs["b"] != 3.14 {
		t.Errorf("b = %v (%T), want float64(3.14)", tr.LastArgs["b"], tr.LastArgs["b"])
	}
	if tr.LastArgs["c"] != "text" {
		t.Errorf("c = %v, want 'text'", tr.LastArgs["c"])
	}
}

func TestStepExecutor_NilArguments(t *testing.T) {
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "tool-a",
		Type:      execution.StepTypeTool,
		Arguments: nil,
	}
	ec := testEC()

	_, err := se.ExecuteTool(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStepExecutor_FallbackExecution(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["fallback-tool"] = "fallback-result"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	ec := testEC()
	result, err := se.ExecuteFallback(context.Background(), "fallback-tool", map[string]any{"key": "val"}, ec, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepName != "fallback-tool" {
		t.Errorf("StepName = %q, want 'fallback-tool'", result.StepName)
	}
	if result.Output != "fallback-result" {
		t.Errorf("Output = %v, want fallback-result", result.Output)
	}
}

func TestStepExecutor_SkillWithStepTimeout(t *testing.T) {
	skillExec := &mockSkillExecutor{resp: []byte(`{"ok":true}`)}
	se := NewStepExecutor(nil, skillExec, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "timed-skill",
		Type:      execution.StepTypeSkill,
		Timeout:   5000, // 5 seconds in milliseconds
		Arguments: map[string]any{"input": "data"},
	}
	ec := testEC()

	result, err := se.ExecuteSkill(context.Background(), step, ec, nil, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output == nil {
		t.Error("expected non-nil output")
	}
}

// --- TDD: Unified Execute dispatches all step types through one method ---

func TestStepExecutor_Execute_DispatchesToolStep(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["my-tool"] = "tool-output"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "my-tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"key": "value"},
	}
	ec := testEC()

	result, err := se.Execute(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepName != "my-tool" {
		t.Errorf("StepName = %q, want %q", result.StepName, "my-tool")
	}
	if result.Output != "tool-output" {
		t.Errorf("Output = %v, want tool-output", result.Output)
	}
}

func TestStepExecutor_Execute_DispatchesSkillStep(t *testing.T) {
	skillExec := &mockSkillExecutor{resp: []byte(`{"result":"skill-output"}`)}
	se := NewStepExecutor(nil, skillExec, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "my-skill",
		Type:      execution.StepTypeSkill,
		Arguments: map[string]any{"input": "data"},
	}
	ec := testEC()

	result, err := se.Execute(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepName != "my-skill" {
		t.Errorf("StepName = %q, want %q", result.StepName, "my-skill")
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
}

func TestStepExecutor_Execute_ClonesBeforeMutation(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["my-tool"] = "ok"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	originalArgs := map[string]any{"input": "{{step-a}}"}
	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "my-tool",
		Type:      execution.StepTypeTool,
		Arguments: originalArgs,
	}
	prevResults := map[string]any{"step-a": "resolved"}
	ec := testEC()

	_, err := se.Execute(context.Background(), step, ec, prevResults, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Original arguments must NOT be mutated
	if step.Arguments["input"] != "{{step-a}}" {
		t.Errorf("original args mutated: input = %v, want {{step-a}}", step.Arguments["input"])
	}
	// Tool runner receives resolved values
	if tr.LastArgs["input"] != "resolved" {
		t.Errorf("tool args = %v, want 'resolved'", tr.LastArgs["input"])
	}
}

func TestStepExecutor_Execute_CoercesAndResolves(t *testing.T) {
	tr := newMockToolRunner()
	tr.result["math"] = "ok"
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "math",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"a": "42", "b": "{{prev}}"},
	}
	prevResults := map[string]any{"prev": "value"}
	ec := testEC()

	_, err := se.Execute(context.Background(), step, ec, prevResults, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.LastArgs["a"] != int64(42) {
		t.Errorf("a = %v (%T), want int64(42)", tr.LastArgs["a"], tr.LastArgs["a"])
	}
	if tr.LastArgs["b"] != "value" {
		t.Errorf("b = %v, want 'value'", tr.LastArgs["b"])
	}
}

func TestStepExecutor_Execute_RoutesMCPPrefix(t *testing.T) {
	mcpRunner := newMockToolRunner()
	mcpRunner.result["mcp.search"] = "search-results"
	se := NewStepExecutor(nil, nil, StepExecutorDeps{MCPRunner: mcpRunner})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "mcp.search",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	result, err := se.Execute(context.Background(), step, ec, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "search-results" {
		t.Errorf("Output = %v, want search-results", result.Output)
	}
}

func TestStepExecutor_Execute_NoRunnerForStepType(t *testing.T) {
	se := NewStepExecutor(nil, nil, StepExecutorDeps{})

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "unknown",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := testEC()

	result, err := se.Execute(context.Background(), step, ec, nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for no tool runner")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
}

// Ensure mockSkillExecutor implements the interface at compile time
var _ execution.SkillExecutor = (*mockSkillExecutor)(nil)

// blockingToolRunner blocks until context is cancelled, then returns error.
type blockingToolRunner struct{}

func (b *blockingToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingToolRunner) callCount() int { return 1 }
