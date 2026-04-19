package skill_executor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	control_skills "github.com/openbotstack/openbotstack-core/control/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// mockSkill implements skills.Skill for testing.
type mockSkill struct {
	id        string
	valid     bool
	timeout   time.Duration
	wasmBytes []byte
}

func (m *mockSkill) ID() string                      { return m.id }
func (m *mockSkill) Name() string                    { return "mock-" + m.id }
func (m *mockSkill) Description() string             { return "Test skill " + m.id }
func (m *mockSkill) Timeout() time.Duration          { return m.timeout }
func (m *mockSkill) InputSchema() *control_skills.JSONSchema  { return nil }
func (m *mockSkill) OutputSchema() *control_skills.JSONSchema { return nil }
func (m *mockSkill) RequiredPermissions() []string   { return nil }
func (m *mockSkill) Validate() error {
	if !m.valid {
		return errors.New("invalid skill")
	}
	return nil
}
func (m *mockSkill) WasmBytes() []byte { return m.wasmBytes }

func newMockSkill(id string, valid bool) *mockSkill {
	return &mockSkill{id: id, valid: valid, timeout: 30 * time.Second}
}

// schemaMockSkill extends mockSkill with an input schema.
type schemaMockSkill struct {
	mockSkill
	inputSchema *control_skills.JSONSchema
}

func (s *schemaMockSkill) InputSchema() *control_skills.JSONSchema { return s.inputSchema }

// Minimal valid Wasm module with execute export
var testWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section
	0x03, 0x02, 0x01, 0x00, // function section
	0x07, 0x0b, 0x01, 0x07, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x65, 0x00, 0x00, // export "execute"
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b, // code section
}

// ==================== Load Tests ====================

func TestLoadSkillSuccess(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("skill-1", true)

	err := e.LoadSkill(ctx, s)
	if err != nil {
		t.Fatalf("LoadSkill failed: %v", err)
	}

	if e.SkillCount() != 1 {
		t.Errorf("Expected 1 skill, got %d", e.SkillCount())
	}
}

func TestLoadSkillNil(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	err := e.LoadSkill(ctx, nil)
	if err != executor.ErrNilSkill {
		t.Errorf("Expected ErrNilSkill, got %v", err)
	}
}

func TestLoadSkillEmptyID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("", true)

	err := e.LoadSkill(ctx, s)
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

func TestLoadSkillInvalid(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("skill-1", false) // invalid

	err := e.LoadSkill(ctx, s)
	if err == nil {
		t.Error("Expected error for invalid skill")
	}
}

func TestLoadSkillDuplicate(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("skill-1", true)

	_ = e.LoadSkill(ctx, s)
	err := e.LoadSkill(ctx, s)
	if err != executor.ErrSkillAlreadyLoaded {
		t.Errorf("Expected ErrSkillAlreadyLoaded, got %v", err)
	}
}

// ==================== Unload Tests ====================

func TestUnloadSkillSuccess(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("skill-1", true)

	_ = e.LoadSkill(ctx, s)
	err := e.UnloadSkill(ctx, "skill-1")
	if err != nil {
		t.Fatalf("UnloadSkill failed: %v", err)
	}

	if e.SkillCount() != 0 {
		t.Errorf("Expected 0 skills, got %d", e.SkillCount())
	}
}

func TestUnloadSkillNotFound(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	err := e.UnloadSkill(ctx, "nonexistent")
	if err != executor.ErrSkillNotFound {
		t.Errorf("Expected ErrSkillNotFound, got %v", err)
	}
}

func TestUnloadSkillEmptyID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	err := e.UnloadSkill(ctx, "")
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

// ==================== Get/List Tests ====================

func TestGetSkillSuccess(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("skill-1", true)

	_ = e.LoadSkill(ctx, s)
	got, err := e.GetSkill(ctx, "skill-1")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.ID() != "skill-1" {
		t.Errorf("Expected skill-1, got %s", got.ID())
	}
}

func TestGetSkillNotFound(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_, err := e.GetSkill(ctx, "nonexistent")
	if err != executor.ErrSkillNotFound {
		t.Errorf("Expected ErrSkillNotFound, got %v", err)
	}
}

func TestGetSkillEmptyID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_, err := e.GetSkill(ctx, "")
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

func TestListSkillsEmpty(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	ids := e.ListSkills(ctx)
	if len(ids) != 0 {
		t.Errorf("Expected empty list, got %v", ids)
	}
}

func TestListSkillsMultiple(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_ = e.LoadSkill(ctx, newMockSkill("a", true))
	_ = e.LoadSkill(ctx, newMockSkill("b", true))
	_ = e.LoadSkill(ctx, newMockSkill("c", true))

	ids := e.ListSkills(ctx)
	if len(ids) != 3 {
		t.Errorf("Expected 3 skills, got %d", len(ids))
	}
}

// ==================== CanExecute Tests ====================

func TestCanExecuteTrue(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("skill-1", true))

	can, err := e.CanExecute(ctx, "skill-1")
	if err != nil {
		t.Fatalf("CanExecute failed: %v", err)
	}
	if !can {
		t.Error("Expected CanExecute to return true")
	}
}

func TestCanExecuteFalse(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	can, err := e.CanExecute(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("CanExecute failed: %v", err)
	}
	if can {
		t.Error("Expected CanExecute to return false")
	}
}

func TestCanExecuteEmptyID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	_, err := e.CanExecute(ctx, "")
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

// ==================== Execute Tests ====================

func TestExecuteSuccess(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("skill-1", true))

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "skill-1",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

func TestExecuteSkillNotLoaded(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "nonexistent",
	})
	if err != execution.ErrSkillNotLoaded {
		t.Errorf("Expected ErrSkillNotLoaded, got %v", err)
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

func TestExecuteEmptySkillID(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "",
	})
	if err != executor.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

// ==================== Real Wasm Execution Tests ====================

func TestExecuteWithRealWasm(t *testing.T) {
	rt, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer rt.Close() //nolint:errcheck // test cleanup

	e := executor.NewDefaultExecutorWithRuntime(rt, nil)
	ctx := context.Background()

	s := &mockSkill{id: "wasm-skill", valid: true, timeout: 30 * time.Second, wasmBytes: testWasm}
	_ = e.LoadSkill(ctx, s)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "wasm-skill",
		Input:   []byte(`{"name": "test"}`),
	})
	if err != nil {
		t.Fatalf("Execute with real Wasm failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v (error: %s)", result.Status, result.Error)
	}
}

func TestExecuteWithLoadSkillWithWasm(t *testing.T) {
	rt, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer rt.Close() //nolint:errcheck // test cleanup

	e := executor.NewDefaultExecutorWithRuntime(rt, nil)
	ctx := context.Background()

	s := newMockSkill("wasm-skill-2", true)
	err = e.LoadSkillWithWasm(ctx, s, testWasm)
	if err != nil {
		t.Fatalf("LoadSkillWithWasm failed: %v", err)
	}

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "wasm-skill-2",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

// ==================== Lifecycle Tests ====================

func TestFullLifecycle(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("lifecycle-test", true)

	// Load
	if err := e.LoadSkill(ctx, s); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if e.SkillCount() != 1 {
		t.Errorf("After load: expected 1, got %d", e.SkillCount())
	}

	// Execute
	result, err := e.Execute(ctx, execution.ExecutionRequest{SkillID: "lifecycle-test"})
	if err != nil || result.Status != execution.StatusSuccess {
		t.Fatalf("Execute failed: %v", err)
	}

	// Unload
	if err := e.UnloadSkill(ctx, "lifecycle-test"); err != nil {
		t.Fatalf("Unload failed: %v", err)
	}
	if e.SkillCount() != 0 {
		t.Errorf("After unload: expected 0, got %d", e.SkillCount())
	}

	// Execute after unload should fail
	_, err = e.Execute(ctx, execution.ExecutionRequest{SkillID: "lifecycle-test"})
	if err != execution.ErrSkillNotLoaded {
		t.Errorf("Expected ErrSkillNotLoaded after unload, got %v", err)
	}
}

func TestReloadSkill(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	s := newMockSkill("reload-test", true)

	// Load, unload, reload
	_ = e.LoadSkill(ctx, s)
	_ = e.UnloadSkill(ctx, "reload-test")
	err := e.LoadSkill(ctx, s)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	if e.SkillCount() != 1 {
		t.Errorf("After reload: expected 1, got %d", e.SkillCount())
	}
}

// ==================== Audit Tests ====================

func TestExecuteWithAuditSuccess(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("audit-skill", true))

	// Wire in-memory audit logger
	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "audit-skill",
		Input:     []byte("test"),
		TenantID:  "tenant-1",
		UserID:    "user-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify audit events: should have "started" + "success"
	events, err := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events (started + success), got %d", len(events))
	}

	// Check events by outcome (order depends on implementation)
	// Find the "started" and "success" events
	var startedEvt, successEvt *execution_logs.Event
	for i := range events {
		switch events[i].Outcome {
		case "started":
			startedEvt = &events[i]
		case "success":
			successEvt = &events[i]
		}
	}

	if startedEvt == nil {
		t.Fatal("Missing 'started' audit event")
	}
	if successEvt == nil {
		t.Fatal("Missing 'success' audit event")
	}

	// Verify started event
	if startedEvt.Action != "skills.execute" {
		t.Errorf("Started event Action: got %q, want %q", startedEvt.Action, "skills.execute")
	}
	if startedEvt.Resource != "skill/audit-skill" {
		t.Errorf("Started event Resource: got %q, want %q", startedEvt.Resource, "skill/audit-skill")
	}
	if startedEvt.TenantID != "tenant-1" {
		t.Errorf("Started event TenantID: got %q, want %q", startedEvt.TenantID, "tenant-1")
	}
	if startedEvt.UserID != "user-1" {
		t.Errorf("Started event UserID: got %q, want %q", startedEvt.UserID, "user-1")
	}

	// Verify success event
	if successEvt.Duration == 0 {
		t.Error("Success event Duration should be > 0")
	}
}

func TestExecuteWithAuditFailure(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "nonexistent",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		RequestID: "req-2",
	})
	if err == nil {
		t.Fatal("Expected error for nonexistent skill")
	}

	// Verify: started + failure
	events, err := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-2"})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 audit events (started + failure), got %d", len(events))
	}

	// Find the failure event
	var failureEvt *execution_logs.Event
	for i := range events {
		if events[i].Outcome == "failure" {
			failureEvt = &events[i]
		}
	}

	if failureEvt == nil {
		t.Fatal("Missing 'failure' audit event")
	}
	if failureEvt.Metadata["error"] != "skill not loaded" {
		t.Errorf("Metadata error: got %q, want %q", failureEvt.Metadata["error"], "skill not loaded")
	}
}

func TestExecuteWithNilAuditLogger(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("no-audit", true))

	// No SetAuditLogger called -- auditLogger is nil

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "no-audit",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

func TestSetAuditLoggerNilSafe(t *testing.T) {
	e := executor.NewDefaultExecutor()
	// Should not panic
	e.SetAuditLogger(nil)
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("nil-audit", true))

	result, err := e.Execute(ctx, execution.ExecutionRequest{SkillID: "nil-audit"})
	if err != nil {
		t.Fatalf("Execute with nil audit logger failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

// mockTextGenerator implements executor.TextGenerator for testing.
type mockTextGenerator struct {
	response string
	err      error
	called   bool
	prompt   string
}

func (m *mockTextGenerator) GenerateText(ctx context.Context, prompt string) (string, error) {
	m.called = true
	m.prompt = prompt
	return m.response, m.err
}

func TestDeclarativeSkill_WithTextGenerator(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("core/summarize", true)
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "This is a summary of the text."}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "core/summarize",
		Input:   []byte(`{"text": "Long text to summarize"}`),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
	if string(result.Output) != "This is a summary of the text." {
		t.Errorf("Expected LLM output, got %q", string(result.Output))
	}
	if !mockLLM.called {
		t.Error("TextGenerator was not called")
	}
	if mockLLM.prompt == "" {
		t.Error("Prompt was empty")
	}
	// Verify the prompt includes skill description and user input
	if !contains(mockLLM.prompt, "Test skill core/summarize") {
		t.Errorf("Prompt should include skill name, got: %s", mockLLM.prompt)
	}
	if !contains(mockLLM.prompt, "Long text to summarize") {
		t.Errorf("Prompt should include user input, got: %s", mockLLM.prompt)
	}
}

func TestDeclarativeSkill_NoTextGenerator_Passthrough(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("core/summarize", true)
	_ = e.LoadSkill(ctx, skill)
	// No SetTextGenerator called

	input := []byte(`{"text": "hello"}`)
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "core/summarize",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
	// Without TextGenerator, should passthrough the raw input
	if string(result.Output) != string(input) {
		t.Errorf("Expected passthrough input, got %q", string(result.Output))
	}
}

func TestDeclarativeSkill_LLMError(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := newMockSkill("core/summarize", true)
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{err: errors.New("LLM unavailable")}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "core/summarize",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("Expected error from LLM failure")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Expected StatusFailed, got %v", result.Status)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestWasmSkill_FallbackToLLM(t *testing.T) {
	// Create executor with a Wasm runtime that will fail on execution
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer wasmRT.Close()

	e := executor.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	// Load a skill WITH Wasm bytes (invalid Wasm that will fail)
	skill := &mockSkill{
		id:        "core/test-wasm",
		valid:     true,
		timeout:   5 * time.Second,
		wasmBytes: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, // invalid Wasm header
	}
	_ = e.LoadSkill(ctx, skill)

	// Set up LLM fallback
	mockLLM := &mockTextGenerator{response: "LLM fallback result"}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "core/test-wasm",
		Input:   []byte("test input"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess via LLM fallback, got %v", result.Status)
	}
	if string(result.Output) != "LLM fallback result" {
		t.Errorf("Expected LLM fallback output, got %q", string(result.Output))
	}
	if !mockLLM.called {
		t.Error("LLM fallback should have been called after Wasm failure")
	}
}

func TestWasmSkill_NoFallbackWithoutTextGenerator(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("Failed to create wasm runtime: %v", err)
	}
	defer wasmRT.Close()

	e := executor.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:        "core/test-wasm-no-fallback",
		valid:     true,
		timeout:   5 * time.Second,
		wasmBytes: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, // invalid Wasm
	}
	_ = e.LoadSkill(ctx, skill)
	// No TextGenerator set

	_, err = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "core/test-wasm-no-fallback",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Error("Expected error when Wasm fails and no TextGenerator")
	}
}

// ==================== Schema Validation Tests ====================

func TestExecute_SchemaValidationReject(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "schema-skill", valid: true, timeout: 30 * time.Second},
		inputSchema: &control_skills.JSONSchema{
			Type:     "object",
			Required: []string{"text"},
			Properties: map[string]*control_skills.JSONSchema{
				"text": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	// Missing required field "text"
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "schema-skill",
		Input:   []byte(`{"other": "value"}`),
	})
	if err == nil {
		t.Fatal("Expected error for schema validation failure")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Expected StatusRejected, got %v", result.Status)
	}
}

func TestExecute_SchemaValidationPass(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "schema-skill-pass", valid: true, timeout: 30 * time.Second},
		inputSchema: &control_skills.JSONSchema{
			Type:     "object",
			Required: []string{"text"},
			Properties: map[string]*control_skills.JSONSchema{
				"text": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	// Valid input with required field
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "schema-skill-pass",
		Input:   []byte(`{"text": "hello"}`),
	})
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}

func TestExecute_NilSchemaSkipsValidation(t *testing.T) {
	e := executor.NewDefaultExecutor()
	ctx := context.Background()

	// Default mockSkill returns nil InputSchema
	_ = e.LoadSkill(ctx, newMockSkill("nil-schema-skill", true))

	// Any input should pass since schema is nil
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "nil-schema-skill",
		Input:   []byte(`{"arbitrary": "data"}`),
	})
	if err != nil {
		t.Fatalf("Expected success with nil schema, got: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Expected StatusSuccess, got %v", result.Status)
	}
}
