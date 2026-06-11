package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/assistant"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

// mockConvStore implements agent.ConversationStore for testing.
type mockConvStore struct {
	summary string
}

func (m *mockConvStore) AppendMessage(ctx context.Context, msg coreagent.SessionMessage) error { return nil }
func (m *mockConvStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, limit int) ([]aitypes.Message, error) {
	return nil, nil
}
func (m *mockConvStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return m.summary, nil
}
func (m *mockConvStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return nil
}
func (m *mockConvStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error { return nil }

func TestBuildPlannerContext_SetsAssistantIdentity(t *testing.T) {
	a := &HarnessAgent{
		runtime:            &assistant.AssistantRuntime{AssistantID: "test-assistant"},
				maxHistoryMessages: 50,
	}

	req := coreagent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "hello",
	}
	skillDescs := []aitypes.SkillDescriptor{
		{ID: "skill-1", Name: "Test", Description: "A test skill"},
	}

	pCtx, err := a.AssembleContext(context.Background(), req, skillDescs)
	if err != nil {
		t.Fatalf("buildPlannerContext failed: %v", err)
	}
	if pCtx.AssistantID != "test-assistant" {
		t.Errorf("AssistantID = %q, want %q", pCtx.AssistantID, "test-assistant")
	}
	if pCtx.UserRequest != "hello" {
		t.Errorf("UserRequest = %q, want %q", pCtx.UserRequest, "hello")
	}
	if len(pCtx.Skills) != 1 || pCtx.Skills[0].ID != "skill-1" {
		t.Errorf("Skills = %v, want [{ID:skill-1}]", pCtx.Skills)
	}
}

func TestBuildPlannerContext_WithConversationStore(t *testing.T) {
	a := &HarnessAgent{
		runtime:            &assistant.AssistantRuntime{AssistantID: "test"},
		maxHistoryMessages: 50,
	}

	req := coreagent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "continue",
	}

	pCtx, err := a.AssembleContext(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("buildPlannerContext failed: %v", err)
	}
	if pCtx.AssistantID != "test" {
		t.Errorf("AssistantID = %q, want %q", pCtx.AssistantID, "test")
	}
}

func TestBuildPlannerContext_NoMemoryManager_NoPanic(t *testing.T) {
	a := &HarnessAgent{
		runtime:            &assistant.AssistantRuntime{AssistantID: "test"},
				maxHistoryMessages: 10,
	}

	req := coreagent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "test",
	}

	pCtx, err := a.AssembleContext(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("buildPlannerContext failed: %v", err)
	}
	if pCtx == nil {
		t.Fatal("pCtx should not be nil")
	}
}

func TestResolveTasks_NoWorkflowResolver_SingleTask(t *testing.T) {
	a := &HarnessAgent{}
	pCtx := &planner.PlannerContext{UserRequest: "do something"}
	req := coreagent.MessageRequest{Message: "do something"}

	tasks := a.ResolveTasks(context.Background(), req, pCtx)
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].TaskDescription != "do something" {
		t.Errorf("TaskDescription = %q, want %q", tasks[0].TaskDescription, "do something")
	}
	if tasks[0].PlannerContext != pCtx {
		t.Error("PlannerContext should be the same pCtx")
	}
}

// --- Pipeline Phase Tests ---

func TestPhaseGatherSkillDescriptors_WithCapRegistry(t *testing.T) {
	a := &HarnessAgent{
		capRegistry: &mockCapRegistry{descs: []aitypes.SkillDescriptor{
			{ID: "cap-1", Name: "Cap1", Description: "Test capability"},
			{ID: "cap-2", Name: "Cap2", Description: "Another capability"},
		}},
	}
	skillDescs, err := a.GatherSkills(context.Background())
	if err != nil {
		t.Fatalf("gatherSkillDescriptors failed: %v", err)
	}
	if len(skillDescs) != 2 {
		t.Errorf("skillDescs len = %d, want 2", len(skillDescs))
	}
}

func TestPhaseGatherSkillDescriptors_WithSkillDisabled(t *testing.T) {
	a := &HarnessAgent{
		capRegistry: &mockCapRegistry{descs: []aitypes.SkillDescriptor{
			{ID: "enabled", Name: "Enabled", Description: "Active"},
			{ID: "disabled", Name: "Disabled", Description: "Inactive"},
		}},
		skillDisabled: func(id string) bool { return id == "disabled" },
	}
	skillDescs, err := a.GatherSkills(context.Background())
	if err != nil {
		t.Fatalf("gatherSkillDescriptors failed: %v", err)
	}
	if len(skillDescs) != 1 || skillDescs[0].ID != "enabled" {
		t.Errorf("skillDescs = %v, want only [enabled]", skillDescs)
	}
}

func TestPhaseGatherSkillDescriptors_FallbackSkillRegistry(t *testing.T) {
	a := &HarnessAgent{
		skillRegistry: &mockSkillRegistry{skills: map[string]mockSkillEntry{
			"skill-a": {id: "skill-a", name: "A", desc: "Skill A"},
		}},
	}
	skillDescs, err := a.GatherSkills(context.Background())
	if err != nil {
		t.Fatalf("gatherSkillDescriptors failed: %v", err)
	}
	if len(skillDescs) != 1 || skillDescs[0].ID != "skill-a" {
		t.Errorf("skillDescs = %v, want [{ID:skill-a}]", skillDescs)
	}
}

func TestPhasePrepareExecutionContext(t *testing.T) {
	a := &HarnessAgent{
		runtime:            &assistant.AssistantRuntime{AssistantID: "asst-1"},
		grantedPermissions: []string{"file.read", "http.fetch"},
	}
	req := coreagent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
	}
	ec := a.PrepareExecutionContext(context.Background(), "exec-123", req)
	if ec.RequestID != "exec-123" {
		t.Errorf("RequestID = %q, want %q", ec.RequestID, "exec-123")
	}
	if ec.AssistantID != "asst-1" {
		t.Errorf("AssistantID = %q, want %q", ec.AssistantID, "asst-1")
	}
	if ec.LoopMode != "harness" {
		t.Errorf("LoopMode = %q, want %q", ec.LoopMode, "harness")
	}
	if len(ec.GrantedPermissions) != 2 {
		t.Errorf("GrantedPermissions len = %d, want 2", len(ec.GrantedPermissions))
	}
}

func TestPhasePrepareExecutionContext_WithProgressCallback(t *testing.T) {
	called := false
	a := &HarnessAgent{
		runtime: &assistant.AssistantRuntime{AssistantID: "asst-1"},
	}
	req := coreagent.MessageRequest{
		ProgressCallback: func(eventType, content string, turn int, tool string) {
			called = true
		},
	}
	ec := a.PrepareExecutionContext(context.Background(), "exec-456", req)
	if ec.ProgressFn == nil {
		t.Error("ProgressFn should be set")
	}
	ec.ProgressFn("test", "msg", 0, "")
	if !called {
		t.Error("ProgressFn should invoke the callback")
	}
}

func TestPhaseFinalize_StoresTraceAndMessages(t *testing.T) {
	store := &mockReasoningStore{traces: map[string]any{}}
	a := &HarnessAgent{
		reasoningStore:    store,
			}
	result := &harness.HarnessResult{
		StepResults: []execution.StepResult{
			{StepID: "s1", StepName: "test-step", Type: "tool", Output: "done"},
		},
	}
	resp := &coreagent.MessageResponse{
		SessionID:   "s1",
		Message:     "completed",
		ExecutionID: "exec-789",
	}
	req := coreagent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "hello",
	}
	a.Finalize(context.Background(), "exec-789", req.TenantID, result, req, resp)
	if _, ok := store.traces["exec-789"]; !ok {
		t.Error("finalize should store execution trace")
	}
}

func TestPhaseFinalize_NilResult_NoPanic(t *testing.T) {
	store := &mockReasoningStore{traces: map[string]any{}}
	a := &HarnessAgent{
		reasoningStore:    store,
			}
	resp := &coreagent.MessageResponse{
		SessionID:   "s1",
		Message:     "done",
		ExecutionID: "exec-000",
	}
	req := coreagent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "hi",
	}
	a.Finalize(context.Background(), "exec-000", req.TenantID, nil, req, resp)
	// Should not panic and should not store trace for nil result
	if _, ok := store.traces["exec-000"]; ok {
		t.Error("finalize should not store trace for nil result")
	}
}

func TestPhaseFinalize_NilReasoningStore_NoPanic(t *testing.T) {
	a := &HarnessAgent{
			}
	result := &harness.HarnessResult{
		StepResults: []execution.StepResult{
			{StepID: "s1", StepName: "test", Type: "tool", Output: "x"},
		},
	}
	resp := &coreagent.MessageResponse{SessionID: "s1", Message: "ok", ExecutionID: "exec-111"}
	req := coreagent.MessageRequest{TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "hi"}
	// Should not panic
	a.Finalize(context.Background(), "exec-111", req.TenantID, result, req, resp)
}

func TestPhaseExecuteTasks_SingleTask(t *testing.T) {
	plannerMock := &mockPlanner{
		plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "respond", Type: execution.StepTypeLLM},
			},
		},
	}
	h := harness.NewExecutionHarness(
		harness.DefaultHarnessConfig(),
		nil,  // no tool runner
		nil,  // no skill executor
		harness.HarnessDeps{
			LLMGenerator: func(ctx context.Context, systemPrompt, userMessage string, _ []aitypes.Message) (string, error) {
				return "response text", nil
			},
		},
	)

	a := &HarnessAgent{
		planner: plannerMock,
		harness: h,
		runtime: &assistant.AssistantRuntime{AssistantID: "test"},
	}

	ec := execution.NewExecutionContext(context.Background(), "exec-1", "asst-1", "sess-1", "t1", "u1")
	tasks := []harness.TaskInput{
		{TaskDescription: "test task", PlannerContext: &planner.PlannerContext{UserRequest: "test"}},
	}

	lastResult, message, skillUsed, err := a.ExecuteTasks(context.Background(), tasks, ec)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}
	if lastResult == nil {
		t.Fatal("lastResult should not be nil")
	}
	if message == "" {
		t.Error("message should not be empty")
	}
	if skillUsed != "respond" {
		t.Errorf("skillUsed = %q, want %q", skillUsed, "respond")
	}
}

func TestPhaseExecuteTasks_MultipleTasks(t *testing.T) {
	callCount := 0
	plannerMock := &mockPlanner{
		plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "respond", Type: execution.StepTypeLLM},
			},
		},
	}
	h := harness.NewExecutionHarness(
		harness.DefaultHarnessConfig(),
		nil, nil,
		harness.HarnessDeps{
			LLMGenerator: func(ctx context.Context, systemPrompt, userMessage string, _ []aitypes.Message) (string, error) {
				callCount++
				return fmt.Sprintf("response %d", callCount), nil
			},
		},
	)

	a := &HarnessAgent{
		planner: plannerMock,
		harness: h,
		runtime: &assistant.AssistantRuntime{AssistantID: "test"},
	}

	ec := execution.NewExecutionContext(context.Background(), "exec-2", "asst-1", "sess-2", "t1", "u1")
	tasks := []harness.TaskInput{
		{TaskDescription: "task 1", PlannerContext: &planner.PlannerContext{UserRequest: "task 1"}},
		{TaskDescription: "task 2", PlannerContext: &planner.PlannerContext{UserRequest: "task 2"}},
	}

	lastResult, message, _, err := a.ExecuteTasks(context.Background(), tasks, ec)
	if err != nil {
		t.Fatalf("executeTasks failed: %v", err)
	}
	if lastResult == nil {
		t.Fatal("lastResult should not be nil")
	}
	if !containsSubstring(message, "Task") {
		t.Errorf("message should contain 'Task', got: %s", message)
	}
}

// --- Mock types for pipeline tests ---

type mockCapRegistry struct {
	descs []aitypes.SkillDescriptor
}

func (m *mockCapRegistry) Register(_ context.Context, _ capability.Capability) error {
	return nil
}
func (m *mockCapRegistry) Unregister(_ context.Context, _ string) error { return nil }
func (m *mockCapRegistry) Get(_ string) (capability.Capability, error)  { return nil, nil }
func (m *mockCapRegistry) List() []aitypes.SkillDescriptor {
	return m.descs
}
func (m *mockCapRegistry) ListByKind(_ capability.CapabilityKind) []aitypes.SkillDescriptor {
	return m.descs
}

type mockSkillEntry struct {
	id, name, desc string
}

type mockSkillRegistry struct {
	skills map[string]mockSkillEntry
}

func (m *mockSkillRegistry) List() []string {
	ids := make([]string, 0, len(m.skills))
	for id := range m.skills {
		ids = append(ids, id)
	}
	return ids
}

func (m *mockSkillRegistry) Get(id string) (skills.Skill, error) {
	entry, ok := m.skills[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return &mockSkillForPipeline{entry}, nil
}

type mockSkillForPipeline struct {
	data mockSkillEntry
}

func (m *mockSkillForPipeline) ID() string          { return m.data.id }
func (m *mockSkillForPipeline) Name() string        { return m.data.name }
func (m *mockSkillForPipeline) Description() string { return m.data.desc }
func (m *mockSkillForPipeline) InputSchema() *aitypes.JSONSchema {
	return &aitypes.JSONSchema{Type: "object"}
}
func (m *mockSkillForPipeline) OutputSchema() *aitypes.JSONSchema { return nil }
func (m *mockSkillForPipeline) RequiredPermissions() []string     { return nil }
func (m *mockSkillForPipeline) Timeout() time.Duration            { return 0 }
func (m *mockSkillForPipeline) Validate() error                   { return nil }

type mockReasoningStore struct {
	traces map[string]any
}

func (m *mockReasoningStore) StoreTrail(executionID string, entries []audit.AuditEvent) {}
func (m *mockReasoningStore) StoreTrace(executionID string, trace any) {
	m.traces[executionID] = trace
}

type mockPlanner struct {
	plan *execution.ExecutionPlan
	err  error
}

func (m *mockPlanner) Plan(_ context.Context, _ *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Return a fresh copy each time so PlanAndRun can freeze it independently
	steps := make([]execution.ExecutionStep, len(m.plan.Steps))
	copy(steps, m.plan.Steps)
	return &execution.ExecutionPlan{Steps: steps}, nil
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
