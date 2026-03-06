package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/skill"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/audit"
)

// ==================== Mock SkillProvider ====================

type testSkill struct {
	id          string
	name        string
	description string
}

func (s *testSkill) ID() string                      { return s.id }
func (s *testSkill) Name() string                    { return s.name }
func (s *testSkill) Description() string             { return s.description }
func (s *testSkill) InputSchema() *skill.JSONSchema  { return nil }
func (s *testSkill) OutputSchema() *skill.JSONSchema { return nil }
func (s *testSkill) RequiredPermissions() []string   { return nil }
func (s *testSkill) Timeout() time.Duration          { return 30 * time.Second }
func (s *testSkill) Validate() error                 { return nil }

type mockSkillProvider struct {
	skills map[string]*testSkill
}

func (p *mockSkillProvider) List() []string {
	ids := make([]string, 0, len(p.skills))
	for id := range p.skills {
		ids = append(ids, id)
	}
	return ids
}

func (p *mockSkillProvider) Get(id string) (skill.Skill, error) {
	s, ok := p.skills[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

// ==================== Skills Endpoint Tests ====================

func TestSkillsEndpointReturnsSkills(t *testing.T) {
	provider := &mockSkillProvider{
		skills: map[string]*testSkill{
			"core/math.add": {
				id:          "core/math.add",
				name:        "Math Add",
				description: "Adds two numbers",
			},
			"core/text.wordcount": {
				id:          "core/text.wordcount",
				name:        "Word Count",
				description: "Counts words",
			},
		},
	}

	handler := api.NewRouter(&mockAgent{})
	handler.SetSkillProvider(provider)

	req := httptest.NewRequest("GET", "/v1/skills", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var skills []api.SkillResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &skills); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(skills) != 2 {
		t.Errorf("Expected 2 skills, got %d", len(skills))
	}

	// Verify fields are populated
	for _, s := range skills {
		if s.ID == "" {
			t.Error("skill ID is empty")
		}
		if s.Name == "" {
			t.Error("skill name is empty")
		}
		if !s.Enabled {
			t.Errorf("skill %s should be enabled", s.ID)
		}
	}
}

func TestSkillsEndpointEmpty(t *testing.T) {
	provider := &mockSkillProvider{skills: map[string]*testSkill{}}
	handler := api.NewRouter(&mockAgent{})
	handler.SetSkillProvider(provider)

	req := httptest.NewRequest("GET", "/v1/skills", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var skills []api.SkillResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &skills)
	if len(skills) != 0 {
		t.Errorf("Expected empty list, got %d", len(skills))
	}
}

func TestSkillsEndpointNoProvider(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	// No SetSkillProvider call

	req := httptest.NewRequest("GET", "/v1/skills", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var skills []api.SkillResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &skills)
	if len(skills) != 0 {
		t.Errorf("Expected empty list, got %d", len(skills))
	}
}

func TestSkillsEndpointMethodNotAllowed(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("POST", "/v1/skills", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}

// ==================== Executions Endpoint Tests ====================

func TestExecutionsEndpoint(t *testing.T) {
	logger := audit.NewPGAuditLogger()

	// Add some execution events
	_ = logger.Log(context.Background(), audit.Event{
		ID:       "exec-1",
		Action:   "skill.execute",
		Resource: "core/math.add",
		Outcome:  "success",
		Duration: 42 * time.Millisecond,
		Metadata: map[string]string{"session_id": "sess-1"},
	})
	_ = logger.Log(context.Background(), audit.Event{
		ID:       "exec-2",
		Action:   "skill.execute",
		Resource: "core/text.wordcount",
		Outcome:  "failure",
		Duration: 100 * time.Millisecond,
		Metadata: map[string]string{"session_id": "sess-2", "error": "timeout"},
	})

	execStore := api.NewAuditExecutionStore(logger)
	handler := api.NewRouter(&mockAgent{})
	handler.SetExecutionStore(execStore)

	req := httptest.NewRequest("GET", "/v1/executions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var execs []api.ExecutionRecord
	if err := json.Unmarshal(rr.Body.Bytes(), &execs); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(execs) != 2 {
		t.Fatalf("Expected 2 executions, got %d", len(execs))
	}

	// Verify first execution
	found := false
	for _, e := range execs {
		if e.ExecutionID == "exec-1" {
			found = true
			if e.SkillID != "core/math.add" {
				t.Errorf("Expected skill core/math.add, got %s", e.SkillID)
			}
			if e.DurationMs != 42 {
				t.Errorf("Expected 42ms, got %d", e.DurationMs)
			}
			if e.Status != "success" {
				t.Errorf("Expected success, got %s", e.Status)
			}
			if e.SessionID != "sess-1" {
				t.Errorf("Expected sess-1, got %s", e.SessionID)
			}
		}
	}
	if !found {
		t.Error("exec-1 not found in results")
	}

	// Verify error execution
	for _, e := range execs {
		if e.ExecutionID == "exec-2" {
			if e.Error != "timeout" {
				t.Errorf("Expected error 'timeout', got '%s'", e.Error)
			}
		}
	}
}

func TestExecutionsEndpointEmpty(t *testing.T) {
	logger := audit.NewPGAuditLogger()
	execStore := api.NewAuditExecutionStore(logger)
	handler := api.NewRouter(&mockAgent{})
	handler.SetExecutionStore(execStore)

	req := httptest.NewRequest("GET", "/v1/executions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}

	var execs []api.ExecutionRecord
	_ = json.Unmarshal(rr.Body.Bytes(), &execs)
	if len(execs) != 0 {
		t.Errorf("Expected empty, got %d", len(execs))
	}
}

func TestExecutionsEndpointNoStore(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("GET", "/v1/executions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
}

func TestExecutionsEndpointMethodNotAllowed(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("POST", "/v1/executions", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405, got %d", rr.Code)
	}
}
