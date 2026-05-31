package agent

import (
	"context"
	"testing"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/assistant"
	csSkills "github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/planner"
)

// mockConvStore implements agent.ConversationStore for testing.
type mockConvStore struct {
	summary string
}

func (m *mockConvStore) AppendMessage(ctx context.Context, msg agent.SessionMessage) error { return nil }
func (m *mockConvStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, limit int) ([]agent.Message, error) {
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
		conversationStore:  &mockConvStore{},
		maxHistoryMessages: 50,
	}

	req := agent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "hello",
	}
	skillDescs := []csSkills.SkillDescriptor{
		{ID: "skill-1", Name: "Test", Description: "A test skill"},
	}

	pCtx, err := a.buildPlannerContext(context.Background(), req, skillDescs, nil)
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
		conversationStore:  &mockConvStore{summary: "Previous summary"},
		maxHistoryMessages: 50,
	}

	req := agent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "continue",
	}

	pCtx, err := a.buildPlannerContext(context.Background(), req, nil, nil)
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
		conversationStore:  &mockConvStore{},
		maxHistoryMessages: 10,
	}

	req := agent.MessageRequest{
		TenantID: "t1", UserID: "u1", SessionID: "s1", Message: "test",
	}

	pCtx, err := a.buildPlannerContext(context.Background(), req, nil, nil)
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
	req := agent.MessageRequest{Message: "do something"}

	tasks := a.resolveTasks(req, pCtx)
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
