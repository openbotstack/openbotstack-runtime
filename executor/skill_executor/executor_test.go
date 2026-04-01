package skill_executor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	control_skills "github.com/openbotstack/openbotstack-core/control/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
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
