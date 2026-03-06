package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/runtime"
	"github.com/openbotstack/openbotstack-core/skill"
	"github.com/openbotstack/openbotstack-runtime/agent"
)

// ==================== Mock Implementations ====================

type mockSkill struct {
	id          string
	name        string
	description string
}

func (m *mockSkill) ID() string                      { return m.id }
func (m *mockSkill) Name() string                    { return m.name }
func (m *mockSkill) Description() string             { return m.description }
func (m *mockSkill) Timeout() time.Duration          { return 30 * time.Second }
func (m *mockSkill) InputSchema() *skill.JSONSchema  { return nil }
func (m *mockSkill) OutputSchema() *skill.JSONSchema { return nil }
func (m *mockSkill) RequiredPermissions() []string   { return nil }
func (m *mockSkill) Validate() error                 { return nil }

type mockRegistry struct {
	skills map[string]skill.Skill
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{skills: make(map[string]skill.Skill)}
}

func (r *mockRegistry) Register(s skill.Skill) {
	r.skills[s.ID()] = s
}

func (r *mockRegistry) List() []string {
	ids := make([]string, 0, len(r.skills))
	for id := range r.skills {
		ids = append(ids, id)
	}
	return ids
}

func (r *mockRegistry) Get(id string) (skill.Skill, error) {
	s, ok := r.skills[id]
	if !ok {
		return nil, errors.New("skill not found")
	}
	return s, nil
}

type mockExecutor struct {
	lastPlan *agent.ExecutionPlan
	lastMeta agent.ExecutionMeta
	response *runtime.ExecutionResult
	err      error
}

func (e *mockExecutor) ExecuteFromPlan(ctx context.Context, plan *agent.ExecutionPlan, meta agent.ExecutionMeta) (*runtime.ExecutionResult, error) {
	e.lastPlan = plan
	e.lastMeta = meta
	if e.err != nil {
		return nil, e.err
	}
	if e.response != nil {
		return e.response, nil
	}
	return &runtime.ExecutionResult{
		Status: runtime.StatusSuccess,
		Output: []byte(`{"result": "ok"}`),
	}, nil
}

// ==================== Tests ====================

func TestDefaultAgentHandleMessageSuccess(t *testing.T) {
	registry := newMockRegistry()
	registry.Register(&mockSkill{id: "core/summarize", name: "Summarize", description: "Summarizes text"})
	registry.Register(&mockSkill{id: "core/sentiment", name: "Sentiment", description: "Analyzes sentiment"})

	planner := agent.NewMockPlanner("core/summarize")
	executor := &mockExecutor{
		response: &runtime.ExecutionResult{
			Status: runtime.StatusSuccess,
			Output: []byte("Summary: This is a test."),
		},
	}

	a := agent.NewDefaultAgent(planner, executor, registry)

	resp, err := a.HandleMessage(context.Background(), agent.MessageRequest{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   "Please summarize this text.",
	})

	if err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}

	if resp.SkillUsed != "core/summarize" {
		t.Errorf("Expected skill core/summarize, got %s", resp.SkillUsed)
	}

	if resp.Plan == nil {
		t.Error("Expected plan to be set")
	}

	if executor.lastMeta.TenantID != "tenant-1" {
		t.Errorf("Expected tenant-1, got %s", executor.lastMeta.TenantID)
	}
}

func TestDefaultAgentNoSkillsAvailable(t *testing.T) {
	registry := newMockRegistry() // empty
	planner := agent.NewMockPlanner("")
	executor := &mockExecutor{}

	a := agent.NewDefaultAgent(planner, executor, registry)

	_, err := a.HandleMessage(context.Background(), agent.MessageRequest{
		Message: "Hello",
	})

	if !errors.Is(err, agent.ErrNoSkillsAvailable) {
		t.Errorf("Expected ErrNoSkillsAvailable, got %v", err)
	}
}

func TestDefaultAgentPlannerError(t *testing.T) {
	registry := newMockRegistry()
	registry.Register(&mockSkill{id: "core/test", name: "Test", description: "Test skill"})

	planner := agent.NewMockPlanner("")
	planner.ForcedError = errors.New("LLM unavailable")
	executor := &mockExecutor{}

	a := agent.NewDefaultAgent(planner, executor, registry)

	_, err := a.HandleMessage(context.Background(), agent.MessageRequest{
		Message: "Hello",
	})

	if err == nil {
		t.Error("Expected error from planner")
	}
}

func TestDefaultAgentExecutorError(t *testing.T) {
	registry := newMockRegistry()
	registry.Register(&mockSkill{id: "core/test", name: "Test", description: "Test skill"})

	planner := agent.NewMockPlanner("core/test")
	executor := &mockExecutor{
		err: errors.New("execution failed"),
	}

	a := agent.NewDefaultAgent(planner, executor, registry)

	resp, err := a.HandleMessage(context.Background(), agent.MessageRequest{
		Message: "Hello",
	})

	if err == nil {
		t.Error("Expected error from executor")
	}

	// Response should still contain error info
	if resp == nil {
		t.Fatal("Expected response even with error")
	}
	if resp.SkillUsed != "core/test" {
		t.Errorf("Expected skill core/test, got %s", resp.SkillUsed)
	}
}

// ==================== ExecutionPlan Tests ====================

func TestExecutionPlanValidate(t *testing.T) {
	tests := []struct {
		name    string
		plan    *agent.ExecutionPlan
		wantErr error
	}{
		{
			name:    "nil plan",
			plan:    nil,
			wantErr: agent.ErrNilPlan,
		},
		{
			name:    "empty skill ID",
			plan:    &agent.ExecutionPlan{SkillID: ""},
			wantErr: agent.ErrEmptySkillID,
		},
		{
			name:    "valid plan",
			plan:    &agent.ExecutionPlan{SkillID: "core/test"},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.plan == nil {
				err = (*agent.ExecutionPlan)(nil).Validate()
			} else {
				err = tt.plan.Validate()
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestExecutionPlanArgumentsJSON(t *testing.T) {
	plan := &agent.ExecutionPlan{
		SkillID: "core/test",
		Arguments: map[string]any{
			"text": "hello",
			"num":  42,
		},
	}

	data, err := plan.ArgumentsJSON()
	if err != nil {
		t.Fatalf("ArgumentsJSON failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("Expected non-empty JSON")
	}
}

func TestExecutionPlanArgumentsJSONNil(t *testing.T) {
	plan := &agent.ExecutionPlan{
		SkillID:   "core/test",
		Arguments: nil,
	}

	data, err := plan.ArgumentsJSON()
	if err != nil {
		t.Fatalf("ArgumentsJSON failed: %v", err)
	}

	if string(data) != "{}" {
		t.Errorf("Expected {}, got %s", string(data))
	}
}
