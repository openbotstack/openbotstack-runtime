package skill_executor_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// ============================================================================
// Timeout Handling Tests
// ============================================================================

func TestExecute_DeclarativeSkill_RespectsRequestTimeout(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("timeout-skill", true)
	_ = e.LoadSkill(ctx, skill)

	// TextGenerator that takes 2 seconds but we set a 50ms timeout
	slowLLM := &slowTextGenerator{delay: 2 * time.Second, response: "done"}
	e.SetTextGenerator(slowLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "timeout-skill",
		Input:   []byte("test"),
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Expected timeout error")
	}
	if result == nil {
		t.Fatal("Expected non-nil result even on timeout")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

func TestExecute_DeclarativeSkill_RespectsSkillDefaultTimeout(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	// Skill with a very short default timeout
	skill := &mockSkill{
		id:            "short-timeout",
		valid:         true,
		timeout:       50 * time.Millisecond,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	slowLLM := &slowTextGenerator{delay: 2 * time.Second, response: "done"}
	e.SetTextGenerator(slowLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "short-timeout",
		Input:   []byte("test"),
		// No Timeout in request; should use skill's default
	})
	if err == nil {
		t.Fatal("Expected timeout error")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

func TestExecute_DeclarativeSkill_RequestTimeoutOverridesSkillDefault(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	// Skill with a 5-second default timeout
	skill := &mockSkill{
		id:            "override-timeout",
		valid:         true,
		timeout:       5 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	slowLLM := &slowTextGenerator{delay: 2 * time.Second, response: "done"}
	e.SetTextGenerator(slowLLM)

	// Request overrides with a 50ms timeout
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "override-timeout",
		Input:   []byte("test"),
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Expected timeout error because request timeout overrides skill default")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

func TestExecute_DefaultTimeout_WhenBothZero(t *testing.T) {
	// When both skill timeout and request timeout are zero,
	// the executor defaults to 60s. We can't wait that long,
	// so we verify the path by using a fast LLM.
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "zero-timeout",
		valid:         true,
		timeout:       0, // no skill timeout
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	fastLLM := &mockTextGenerator{response: "fast"}
	e.SetTextGenerator(fastLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "zero-timeout",
		Input:   []byte("test"),
		// Timeout: 0 (no request timeout)
	})
	if err != nil {
		t.Fatalf("Expected success with default timeout, got: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

// ============================================================================
// StreamingTextGenerator Tests
// ============================================================================

func TestExecute_DeclarativeSkill_StreamingPath(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("stream-skill", true)
	_ = e.LoadSkill(ctx, skill)

	streamLLM := &mockStreamingTextGenerator{
		response: "streamed output",
		tokens:   []string{"stream", "ed output"},
	}
	e.SetTextGenerator(streamLLM)

	var capturedTokens []string
	tokenFn := func(token string) {
		capturedTokens = append(capturedTokens, token)
	}

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "stream-skill",
		Input:   []byte("test"),
		TokenFn: tokenFn,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
	if string(result.Output) != "streamed output" {
		t.Errorf("Output = %q, want %q", string(result.Output), "streamed output")
	}
	if !streamLLM.streamCalled {
		t.Error("Expected streaming path to be used")
	}
	if len(capturedTokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(capturedTokens))
	}
}

func TestExecute_DeclarativeSkill_StreamingFallback_NonStreamingGenerator(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("stream-fallback-skill", true)
	_ = e.LoadSkill(ctx, skill)

	// Non-streaming generator, but TokenFn is set
	nonStreamLLM := &mockTextGenerator{response: "non-stream output"}
	e.SetTextGenerator(nonStreamLLM)

	var capturedTokens []string
	tokenFn := func(token string) {
		capturedTokens = append(capturedTokens, token)
	}

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "stream-fallback-skill",
		Input:   []byte("test"),
		TokenFn: tokenFn,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
	// When generator doesn't support streaming, the whole output
	// is sent as a single token via TokenFn
	if len(capturedTokens) != 1 {
		t.Errorf("Expected 1 token from non-streaming fallback, got %d", len(capturedTokens))
	}
	if capturedTokens[0] != "non-stream output" {
		t.Errorf("Token = %q, want %q", capturedTokens[0], "non-stream output")
	}
}

// ============================================================================
// Non-Wasm Non-Declarative Skill Error Tests
// ============================================================================

func TestExecute_WasmModeSkill_NoBinary_Error(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	// Skill declares wasm mode but has no Wasm bytes and no runtime
	skill := &mockSkill{
		id:            "wasm-no-binary",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "wasm",
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "wasm-no-binary",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("Expected error for wasm-mode skill with no binary")
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
	if !strings.Contains(result.Error, "wasm execution but no binary available") {
		t.Errorf("Error should mention no binary available, got: %s", result.Error)
	}
}

func TestExecute_NativeModeSkill_NoBinary_Error(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "native-no-binary",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "native",
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "native-no-binary",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("Expected error for native-mode skill with no binary")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

// ============================================================================
// LoadSkillWithWasm Edge Cases
// ============================================================================

func TestLoadSkillWithWasm_NilSkill(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	err := e.LoadSkillWithWasm(ctx, nil, testWasm)
	if err != executor.ErrNilSkill {
		t.Errorf("Expected ErrNilSkill, got %v", err)
	}
}

func TestLoadSkillWithWasm_EmptyID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	s := newMockSkill("", true)
	err := e.LoadSkillWithWasm(ctx, s, testWasm)
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

func TestLoadSkillWithWasm_EmptyWasmBytes_Succeeds(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	s := newMockSkill("empty-wasm", true)
	err := e.LoadSkillWithWasm(ctx, s, nil)
	if err != nil {
		t.Fatalf("LoadSkillWithWasm with nil bytes should succeed: %v", err)
	}
	if e.SkillCount() != 1 {
		t.Errorf("Expected 1 skill, got %d", e.SkillCount())
	}
}

func TestLoadSkillWithWasm_Duplicate(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	s := newMockSkill("dup-wasm", true)
	_ = e.LoadSkillWithWasm(ctx, s, testWasm)
	err := e.LoadSkillWithWasm(ctx, s, testWasm)
	if err != executor.ErrSkillAlreadyLoaded {
		t.Errorf("Expected ErrSkillAlreadyLoaded, got %v", err)
	}
}

// ============================================================================
// ExecutePlan (multi-step) Tests
// ============================================================================

func TestExecutePlan_NilPlan(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	err := e.ExecutePlan(ctx, nil, ec)
	if err != executor.ErrNilExecutionPlan {
		t.Errorf("Expected ErrNilExecutionPlan, got %v", err)
	}
}

func TestExecutePlan_SingleStepSkill(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("plan-step-skill", true)
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "step-done"})

	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{
				Name:      "plan-step-skill",
				Type:      execution.StepTypeSkill,
				Arguments: map[string]any{"text": "input"},
			},
		},
	}
	_ = plan.Validate()

	err := e.ExecutePlan(ctx, plan, ec)
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}

	results := ec.Results()
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].StepName != "plan-step-skill" {
		t.Errorf("StepName = %q, want %q", results[0].StepName, "plan-step-skill")
	}
	if results[0].Error != nil {
		t.Errorf("Step error: %v", results[0].Error)
	}
}

func TestExecutePlan_MultiStep(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_ = e.LoadSkill(ctx, newMockSkill("step-a", true))
	_ = e.LoadSkill(ctx, newMockSkill("step-b", true))
	e.SetTextGenerator(&mockTextGenerator{response: "multi-step"})

	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "step-a", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
			{Name: "step-b", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	err := e.ExecutePlan(ctx, plan, ec)
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}

	results := ec.Results()
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}
}

func TestExecutePlan_StopsOnFirstError(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_ = e.LoadSkill(ctx, newMockSkill("fail-step", true))
	_ = e.LoadSkill(ctx, newMockSkill("success-step", true))
	// LLM will fail
	e.SetTextGenerator(&mockTextGenerator{err: errors.New("LLM down")})

	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "fail-step", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
			{Name: "success-step", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	err := e.ExecutePlan(ctx, plan, ec)
	if err == nil {
		t.Fatal("Expected error when first step fails")
	}
	if !strings.Contains(err.Error(), "fail-step") {
		t.Errorf("Error should mention step name, got: %v", err)
	}

	// Only first step should have been attempted
	results := ec.Results()
	if len(results) != 1 {
		t.Fatalf("Expected 1 result (stopped on first error), got %d", len(results))
	}
}

func TestExecutePlan_ContextCancellation(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx, cancel := context.WithCancel(context.Background())

	_ = e.LoadSkill(ctx, newMockSkill("cancel-step", true))
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "cancel-step", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	_ = plan.Validate()

	// Cancel context before execution
	cancel()

	err := e.ExecutePlan(ctx, plan, ec)
	if err == nil {
		t.Error("Expected error from cancelled context")
	}
}

// TestExecutePlan_UsesStepExecutor verifies that ExecutePlan dispatches through
// harness.StepExecutor (the canonical dispatch point) rather than the removed
// StepRunner. This test uses a tool step to confirm StepExecutor's prefix-based
// routing is active.
func TestExecutePlan_UsesStepExecutor_ToolStepWithRunner(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	// Set up a mock tool runner that records calls
	called := false
	mockRunner := &recordingToolRunner{fn: func(name string, args map[string]any) (*execution.StepResult, error) {
		called = true
		if name != "my_tool" {
			t.Errorf("tool name = %q, want %q", name, "my_tool")
		}
		return &execution.StepResult{StepName: name, Output: "tool-output"}, nil
	}}
	e.SetToolRunner(mockRunner)

	ec := execution.NewExecutionContext(ctx, "req", "", "", "t1", "u1")

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "my_tool", Type: execution.StepTypeTool, Arguments: map[string]any{"key": "value"}},
		},
	}
	_ = plan.Validate()

	err := e.ExecutePlan(ctx, plan, ec)
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}
	if !called {
		t.Error("tool runner should have been called via StepExecutor dispatch")
	}

	results := ec.Results()
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].StepName != "my_tool" {
		t.Errorf("StepName = %q, want %q", results[0].StepName, "my_tool")
	}
}

// recordingToolRunner is a tool runner that records calls for testing.
type recordingToolRunner struct {
	fn func(name string, args map[string]any) (*execution.StepResult, error)
}

func (m *recordingToolRunner) Execute(_ context.Context, name string, args map[string]any, _ *execution.ExecutionContext) (*execution.StepResult, error) {
	return m.fn(name, args)
}

// ============================================================================
// Wasm Fallback Tests
// ============================================================================

func TestExecute_WasmFallback_NoLLMPermission_NoFallback(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer wasmRT.Close()

	e := executor.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	// Skill with invalid Wasm bytes and NO llm:generate permission
	skill := &mockSkill{
		id:          "no-perm-skill",
		valid:       true,
		timeout:     5 * time.Second,
		wasmBytes:   []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, // invalid Wasm
		permissions: []string{},                                                 // no llm:generate
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "should not be called"}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "no-perm-skill",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("Expected error when Wasm fails and no llm:generate permission")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
	if mockLLM.called {
		t.Error("LLM should NOT have been called without llm:generate permission")
	}
}

func TestExecute_WasmFallback_LLMAfterFallback_AlsoFails(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer wasmRT.Close()

	e := executor.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:          "both-fail-skill",
		valid:       true,
		timeout:     5 * time.Second,
		wasmBytes:   []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
		permissions: []string{"llm:generate"},
	}
	_ = e.LoadSkill(ctx, skill)

	// LLM also fails
	failingLLM := &mockTextGenerator{err: errors.New("LLM also down")}
	e.SetTextGenerator(failingLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "both-fail-skill",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("Expected error when both Wasm and LLM fail")
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
	// The error should be the original Wasm error (not the LLM error)
	if !failingLLM.called {
		t.Error("Expected LLM fallback to be attempted after Wasm failure")
	}
}

// ============================================================================
// Input Validation Edge Cases
// ============================================================================

func TestExecute_InvalidJSONInput_WithSchema(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill:   mockSkill{id: "json-skill", valid: true, timeout: 30 * time.Second},
		inputSchema: &types.JSONSchema{Type: "object"},
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "json-skill",
		Input:   []byte("not-json-at-all{{{"),
	})
	if err == nil {
		t.Fatal("Expected error for invalid JSON input")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Expected StatusRejected, got %v", result.Status)
	}
	if !strings.Contains(result.Error, "input validation failed") {
		t.Errorf("Error should mention input validation, got: %s", result.Error)
	}
}

func TestExecute_EmptyInput_NilSchema(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("empty-input-skill", true)
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "empty-input-skill",
		Input:   nil, // nil input
	})
	if err != nil {
		t.Fatalf("Expected success with nil input and nil schema, got: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

func TestExecute_SchemaValidation_AuditRejectedEvent(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "reject-audit-skill", valid: true, timeout: 30 * time.Second},
		inputSchema: &types.JSONSchema{
			Type:     "object",
			Required: []string{"required_field"},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "reject-audit-skill",
		Input:     []byte(`{"wrong_field": "value"}`),
		TenantID:  "t1",
		UserID:    "u1",
		RequestID: "req-reject",
	})
	if err == nil {
		t.Fatal("Expected schema validation error")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Expected StatusRejected, got %v", result.Status)
	}

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-reject"})
	var hasRejected bool
	for _, evt := range events {
		if evt.Outcome == "rejected" {
			hasRejected = true
			if evt.Metadata["error"] == "" {
				t.Error("Rejected audit event should have error metadata")
			}
			break
		}
	}
	if !hasRejected {
		t.Error("Expected 'rejected' audit event for schema validation failure")
	}
}

// ============================================================================
// Declarative Skill With SKILL.md Template
// ============================================================================

func TestExecute_DeclarativeSkill_InputTemplateReplacement(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "template-skill",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "Process: {{.Input}} then respond.",
	}
	_ = e.LoadSkill(ctx, skill)

	capturedPrompt := &promptCapture{mockTextGenerator: &mockTextGenerator{response: "ok"}}
	e.SetTextGenerator(capturedPrompt)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "template-skill",
		Input:   []byte("my-input-data"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
	if !contains(capturedPrompt.lastPrompt, "my-input-data") {
		t.Errorf("Prompt should have {{.Input}} replaced, got: %s", capturedPrompt.lastPrompt)
	}
	if contains(capturedPrompt.lastPrompt, "{{.Input}}") {
		t.Errorf("Prompt should NOT contain raw {{.Input}} placeholder, got: %s", capturedPrompt.lastPrompt)
	}
}

// ============================================================================
// Get and List (non-context) Methods
// ============================================================================

func TestGet_NonContext_Success(t *testing.T) {
	e := executor.NewDefaultExecutor()
	s := newMockSkill("get-test", true)
	_ = e.LoadSkill(context.Background(), s)

	got, err := e.Get("get-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID() != "get-test" {
		t.Errorf("ID = %q, want %q", got.ID(), "get-test")
	}
}

func TestGet_NonContext_NotFound(t *testing.T) {
	e := executor.NewDefaultExecutor()

	_, err := e.Get("nonexistent")
	if err != executor.ErrSkillNotFound {
		t.Errorf("Expected ErrSkillNotFound, got %v", err)
	}
}

func TestList_NonContext_Empty(t *testing.T) {
	e := executor.NewDefaultExecutor()

	ids := e.List()
	if len(ids) != 0 {
		t.Errorf("Expected empty list, got %v", ids)
	}
}

func TestList_NonContext_Multiple(t *testing.T) {
	e := executor.NewDefaultExecutor()
	_ = e.LoadSkill(context.Background(), newMockSkill("a", true))
	_ = e.LoadSkill(context.Background(), newMockSkill("b", true))

	ids := e.List()
	if len(ids) != 2 {
		t.Errorf("Expected 2 skills, got %d", len(ids))
	}
}

// ============================================================================
// SetToolRunner
// ============================================================================

func TestSetToolRunner(t *testing.T) {
	e := executor.NewDefaultExecutor()

	// Should not panic with nil
	e.SetToolRunner(nil)

	// Should not panic with a mock
	e.SetToolRunner(&mockToolRunner{})
}

// ============================================================================
// Concurrent Safety Tests
// ============================================================================

func TestConcurrentLoadAndExecute(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	// Pre-load skills
	for i := 0; i < 10; i++ {
		_ = e.LoadSkill(ctx, newMockSkill(fmt.Sprintf("concurrent-%d", i), true))
	}

	var wg sync.WaitGroup
	var errors atomic.Int64

	// Concurrently execute skills
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			skillID := fmt.Sprintf("concurrent-%d", idx%10)
			result, err := e.Execute(ctx, execution.ExecutionRequest{
				SkillID: skillID,
				Input:   []byte("test"),
			})
			if err != nil || result.Status != execution.StatusSuccess {
				errors.Add(1)
			}
		}(i)
	}

	wg.Wait()
	if errors.Load() > 0 {
		t.Errorf("Expected 0 errors from concurrent execution, got %d", errors.Load())
	}
}

func TestConcurrentLoadUnload(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-lu-%d", idx%10)
			_ = e.LoadSkill(ctx, newMockSkill(id, true))
			_ = e.UnloadSkill(ctx, id)
		}(i)
	}
	wg.Wait()
	// No assertion on final count — just verifying no panics or deadlocks
}

// ============================================================================
// Wasm Timeout Audit Event
// ============================================================================

func TestExecute_WasmPath_EmitsAuditEvents(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer wasmRT.Close()

	e := executor.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:          "wasm-timeout-skill",
		valid:       true,
		timeout:     50 * time.Millisecond,
		wasmBytes:   testWasm,
		permissions: []string{},
	}
	_ = e.LoadSkill(ctx, skill)

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "wasm-timeout-skill",
		Input:   []byte("test"),
	})
	_ = result
	_ = err

	// Verify audit events were emitted
	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{})
	if len(events) < 2 {
		t.Fatalf("Expected at least 2 audit events (started + outcome), got %d", len(events))
	}

	// First event should be "started"
	if events[0].Outcome != "started" {
		t.Errorf("First event outcome = %q, want %q", events[0].Outcome, "started")
	}
}

// ============================================================================
// Audit Source Verification
// ============================================================================

func TestExecute_AuditEvent_Source(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("source-skill", true))
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "source-skill",
		Input:     []byte("test"),
		RequestID: "req-source",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-source"})
	for _, evt := range events {
		if evt.Source != audit.SourceExecutor {
			t.Errorf("Event Source = %q, want %q", evt.Source, audit.SourceExecutor)
		}
		if evt.Action != "skills.execute" {
			t.Errorf("Event Action = %q, want %q", evt.Action, "skills.execute")
		}
	}
}

// ============================================================================
// Error Wrapping Tests
// ============================================================================

func TestLoadSkill_InvalidSkill_WrapsError(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	s := &mockSkill{id: "bad-skill", valid: false}
	err := e.LoadSkill(ctx, s)

	if err == nil {
		t.Fatal("Expected error")
	}
	if !strings.Contains(err.Error(), "executor: invalid skill") {
		t.Errorf("Error should be wrapped with context, got: %v", err)
	}
}

func TestLoadSkillWithWasm_InvalidSkill_WrapsError(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	s := &mockSkill{id: "bad-wasm-skill", valid: false}
	err := e.LoadSkillWithWasm(ctx, s, testWasm)

	if err == nil {
		t.Fatal("Expected error")
	}
	if !strings.Contains(err.Error(), "executor: invalid skill") {
		t.Errorf("Error should be wrapped with context, got: %v", err)
	}
}

// ============================================================================
// Helper Types for Tests
// ============================================================================

// slowTextGenerator sleeps before returning, used for timeout tests.
type slowTextGenerator struct {
	delay    time.Duration
	response string
}

func (s *slowTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	select {
	case <-time.After(s.delay):
		return s.response, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// mockStreamingTextGenerator implements executor.StreamingTextGenerator.
type mockStreamingTextGenerator struct {
	response     string
	tokens       []string
	streamCalled bool
}

func (m *mockStreamingTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	return m.response, nil
}

func (m *mockStreamingTextGenerator) GenerateStreamText(ctx context.Context, prompt string, tokenFn func(string)) (string, error) {
	m.streamCalled = true
	for _, token := range m.tokens {
		tokenFn(token)
	}
	return m.response, nil
}

// promptCapture wraps a TextGenerator to capture the prompt.
type promptCapture struct {
	*mockTextGenerator
	lastPrompt string
}

func (p *promptCapture) GenerateText(ctx context.Context, prompt string) (string, error) {
	p.lastPrompt = prompt
	return p.mockTextGenerator.GenerateText(ctx, prompt)
}

// mockToolRunner implements toolrunner.ToolRunner for testing.
type mockToolRunner struct{}

func (m *mockToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	return &execution.StepResult{StepName: name, Output: "mock"}, nil
}
