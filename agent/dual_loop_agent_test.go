package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	csSkills "github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-runtime/loop"
)

// --- Mocks ---

// mockSkillRegistry implements agent.SkillRegistry
type mockSkillRegistry struct {
	ids []string
}

func (m *mockSkillRegistry) List() []string { return m.ids }
func (m *mockSkillRegistry) Get(id string) (skills.Skill, error) {
	return &stubSkill{id: id}, nil
}

// stubSkill implements skills.Skill minimally
type stubSkill struct{ id string }

func (s *stubSkill) ID() string                    { return s.id }
func (s *stubSkill) Name() string                  { return s.id }
func (s *stubSkill) Description() string           { return "test skill" }
func (s *stubSkill) InputSchema() *csSkills.JSONSchema  { return nil }
func (s *stubSkill) OutputSchema() *csSkills.JSONSchema { return nil }
func (s *stubSkill) RequiredPermissions() []string  { return nil }
func (s *stubSkill) Timeout() time.Duration         { return 10 * time.Second }
func (s *stubSkill) Validate() error                { return nil }

// mockConversationStore implements agent.ConversationStore
type mockConversationStore struct {
	messages []agent.SessionMessage
}

func (m *mockConversationStore) AppendMessage(ctx context.Context, msg agent.SessionMessage) error {
	m.messages = append(m.messages, msg)
	return nil
}
func (m *mockConversationStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]agent.Message, error) {
	return nil, nil
}
func (m *mockConversationStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return "", nil
}
func (m *mockConversationStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return nil
}
func (m *mockConversationStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	return nil
}

// mockLoopInnerLoop implements loop.InnerLoop
type mockLoopInnerLoop struct {
	result *loop.TaskResult
	err    error
}

func (m *mockLoopInnerLoop) Run(ctx context.Context, task loop.TaskInput, ec *execution.ExecutionContext) (*loop.TaskResult, error) {
	return m.result, m.err
}

// mockLoopOuterLoop implements loop.OuterLoop
type mockLoopOuterLoop struct {
	result *loop.WorkflowResult
	err    error
}

func (m *mockLoopOuterLoop) Run(ctx context.Context, tasks []loop.TaskInput, ec *execution.ExecutionContext) (*loop.WorkflowResult, error) {
	return m.result, m.err
}

// mockPlannerExec implements planner.ExecutionPlanner
type mockPlannerExec struct {
	plan *execution.ExecutionPlan
	err  error
}

func (m *mockPlannerExec) Plan(ctx context.Context, pCtx *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	return m.plan, m.err
}

// --- Tests ---

func TestDualLoopAgent_SingleTaskSuccess(t *testing.T) {
	innerResult := &loop.TaskResult{
		StopReason: loop.StopReasonPlannerStopped,
		TurnResults: []loop.TurnResult{
			{ActionsExecuted: []string{"core/summarize"}, Observations: []string{"Summary: Hello world"}},
		},
	}
	outerResult := &loop.WorkflowResult{
		TaskResults:   []*loop.TaskResult{innerResult},
		StopCondition: loop.StopCondition{Reason: loop.StopReasonGoalAchieved},
	}

	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/summarize"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{result: innerResult},
		&mockLoopOuterLoop{result: outerResult},
	)
	dla.SetConversationStore(&mockConversationStore{})

	resp, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "Summarize this text",
	})
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if resp.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", resp.SessionID, "s1")
	}
	if resp.Message == "" {
		t.Error("Message should not be empty")
	}
	if resp.SkillUsed != "core/summarize" {
		t.Errorf("SkillUsed = %q, want %q", resp.SkillUsed, "core/summarize")
	}
}

func TestDualLoopAgent_OuterLoopError(t *testing.T) {
	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/test"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{},
		&mockLoopOuterLoop{err: fmt.Errorf("outer loop exploded")},
	)
	dla.SetConversationStore(&mockConversationStore{})

	_, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "test",
	})
	if err == nil {
		t.Fatal("expected error from outer loop")
	}
}

func TestDualLoopAgent_NoSkillsAvailable(t *testing.T) {
	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{},
		&mockLoopOuterLoop{},
	)

	_, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "test",
	})
	if err == nil {
		t.Fatal("expected error when no skills available")
	}
}

func TestDualLoopAgent_MessagesStored(t *testing.T) {
	innerResult := &loop.TaskResult{
		StopReason: loop.StopReasonPlannerStopped,
		TurnResults: []loop.TurnResult{
			{Observations: []string{"result data"}},
		},
	}
	outerResult := &loop.WorkflowResult{
		TaskResults:   []*loop.TaskResult{innerResult},
		StopCondition: loop.StopCondition{Reason: loop.StopReasonGoalAchieved},
	}

	store := &mockConversationStore{}
	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/test"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{result: innerResult},
		&mockLoopOuterLoop{result: outerResult},
	)
	dla.SetConversationStore(store)

	_, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "hello",
	})
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if len(store.messages) != 2 {
		t.Fatalf("expected 2 stored messages, got %d", len(store.messages))
	}
	if store.messages[0].Role != "user" {
		t.Errorf("first message role = %q, want %q", store.messages[0].Role, "user")
	}
	if store.messages[1].Role != "assistant" {
		t.Errorf("second message role = %q, want %q", store.messages[1].Role, "assistant")
	}
}

func TestDualLoopAgent_Setters(t *testing.T) {
	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{},
		&mockLoopOuterLoop{},
	)

	dla.SetConversationStore(&mockConversationStore{})
	dla.SetMaxHistoryMessages(25)
	dla.SetContextAssembler(nil)
	dla.SetWorkflowResolver(nil)

	if dla.maxHistoryMessages != 25 {
		t.Errorf("maxHistoryMessages = %d, want 25", dla.maxHistoryMessages)
	}
}

func TestDualLoopAgent_ImplementsAgentInterface(t *testing.T) {
	var _ agent.Agent = NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{},
		&assistant.AssistantRuntime{},
		&mockLoopInnerLoop{},
		&mockLoopOuterLoop{},
	)
}

// Compile-time interface checks
var _ agent.SkillRegistry = (*mockSkillRegistry)(nil)
var _ agent.ConversationStore = (*mockConversationStore)(nil)

// --- MemoryManager Mock ---

type mockMemoryManager struct {
	entries []abstraction.MemoryEntry
	err     error
	called  bool
	query   string
}

func (m *mockMemoryManager) StoreShortTerm(ctx context.Context, entry abstraction.MemoryEntry) error {
	return nil
}
func (m *mockMemoryManager) StoreLongTerm(ctx context.Context, entry abstraction.MemoryEntry) error {
	return nil
}
func (m *mockMemoryManager) RetrieveSimilar(ctx context.Context, query string, limit int) ([]abstraction.MemoryEntry, error) {
	m.called = true
	m.query = query
	if m.err != nil {
		return nil, m.err
	}
	return m.entries, nil
}
func (m *mockMemoryManager) RetrieveByTag(ctx context.Context, tags []string, limit int) ([]abstraction.MemoryEntry, error) {
	return nil, nil
}
func (m *mockMemoryManager) Forget(ctx context.Context, id string) error {
	return nil
}
func (m *mockMemoryManager) Summarize(ctx context.Context, entries []abstraction.MemoryEntry) (abstraction.MemoryEntry, error) {
	return abstraction.MemoryEntry{}, nil
}

// --- MemoryContext Tests ---

func TestDualLoopAgent_MemoryContextPopulated(t *testing.T) {
	memMgr := &mockMemoryManager{
		entries: []abstraction.MemoryEntry{
			{ID: "mem1", Content: "User previously asked about Qwen models"},
			{ID: "mem2", Content: "User prefers concise summaries"},
		},
	}

	// Use a mock outer loop that captures the PlannerContext
	var capturedPctx *planner.PlannerContext
	innerResult := &loop.TaskResult{
		StopReason: loop.StopReasonPlannerStopped,
		TurnResults: []loop.TurnResult{
			{Observations: []string{"done"}},
		},
	}
	outerResult := &loop.WorkflowResult{
		TaskResults:   []*loop.TaskResult{innerResult},
		StopCondition: loop.StopCondition{Reason: loop.StopReasonGoalAchieved},
	}

	// Use a mock inner loop that captures the PlannerContext from the task
	mockInner := &capturingInnerLoop{result: innerResult, capturedCtx: &capturedPctx}
	mockOuter := &capturingOuterLoop{result: outerResult, capturedCtx: &capturedPctx}

	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/summarize"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		mockInner,
		mockOuter,
	)
	dla.SetConversationStore(&mockConversationStore{})
	dla.SetMemoryManager(memMgr)

	_, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "Summarize this text",
	})
	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	// Verify MemoryManager was called
	if !memMgr.called {
		t.Fatal("MemoryManager.RetrieveSimilar was not called")
	}
	if memMgr.query != "Summarize this text" {
		t.Errorf("query = %q, want %q", memMgr.query, "Summarize this text")
	}

	// Verify PlannerContext has memory populated
	if capturedPctx == nil {
		t.Fatal("PlannerContext was nil")
	}
	if len(capturedPctx.MemoryContext) != 2 {
		t.Fatalf("MemoryContext has %d entries, want 2", len(capturedPctx.MemoryContext))
	}
	if string(capturedPctx.MemoryContext[0].Content) != "User previously asked about Qwen models" {
		t.Errorf("MemoryContext[0].Content = %q, want %q", capturedPctx.MemoryContext[0].Content, "User previously asked about Qwen models")
	}
}

func TestDualLoopAgent_MemoryContextErrorFallback(t *testing.T) {
	memMgr := &mockMemoryManager{
		err: fmt.Errorf("vector store unavailable"),
	}

	innerResult := &loop.TaskResult{
		StopReason: loop.StopReasonPlannerStopped,
		TurnResults: []loop.TurnResult{{Observations: []string{"done"}}},
	}
	outerResult := &loop.WorkflowResult{
		TaskResults:   []*loop.TaskResult{innerResult},
		StopCondition: loop.StopCondition{Reason: loop.StopReasonGoalAchieved},
	}

	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/summarize"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{result: innerResult},
		&mockLoopOuterLoop{result: outerResult},
	)
	dla.SetConversationStore(&mockConversationStore{})
	dla.SetMemoryManager(memMgr)

	resp, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "test",
	})
	if err != nil {
		t.Fatalf("HandleMessage should not fail on memory error, got: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response even with memory error")
	}
}

func TestDualLoopAgent_MemoryContextNilManager(t *testing.T) {
	innerResult := &loop.TaskResult{
		StopReason: loop.StopReasonPlannerStopped,
		TurnResults: []loop.TurnResult{{Observations: []string{"done"}}},
	}
	outerResult := &loop.WorkflowResult{
		TaskResults:   []*loop.TaskResult{innerResult},
		StopCondition: loop.StopCondition{Reason: loop.StopReasonGoalAchieved},
	}

	dla := NewDualLoopAgent(
		&mockPlannerExec{},
		&mockSkillRegistry{ids: []string{"core/summarize"}},
		&assistant.AssistantRuntime{AssistantID: "test"},
		&mockLoopInnerLoop{result: innerResult},
		&mockLoopOuterLoop{result: outerResult},
	)
	dla.SetConversationStore(&mockConversationStore{})
	// No SetMemoryManager call — memoryManager stays nil

	resp, err := dla.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "t1",
		UserID:    "u1",
		SessionID: "s1",
		Message:   "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
}

// --- Capturing mocks for PlannerContext inspection ---

type capturingInnerLoop struct {
	result      *loop.TaskResult
	err         error
	capturedCtx **planner.PlannerContext
}

func (m *capturingInnerLoop) Run(ctx context.Context, task loop.TaskInput, ec *execution.ExecutionContext) (*loop.TaskResult, error) {
	if m.capturedCtx != nil && task.PlannerContext != nil {
		*m.capturedCtx = task.PlannerContext
	}
	return m.result, m.err
}

type capturingOuterLoop struct {
	result      *loop.WorkflowResult
	err         error
	capturedCtx **planner.PlannerContext
}

func (m *capturingOuterLoop) Run(ctx context.Context, tasks []loop.TaskInput, ec *execution.ExecutionContext) (*loop.WorkflowResult, error) {
	if m.capturedCtx != nil && len(tasks) > 0 && tasks[0].PlannerContext != nil {
		*m.capturedCtx = tasks[0].PlannerContext
	}
	return m.result, m.err
}
