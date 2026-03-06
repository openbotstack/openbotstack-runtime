package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/openbotstack/openbotstack-core/skill"
)

// SkillProvider provides access to loaded skills for the API.
type SkillProvider interface {
	List() []string
	Get(id string) (skill.Skill, error)
}

// SkillResponse is the JSON representation of a skill.
type SkillResponse struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	Type         string      `json:"type"`
	InputSchema  interface{} `json:"input_schema,omitempty"`
	OutputSchema interface{} `json:"output_schema,omitempty"`
	Version      string      `json:"version"`
	Enabled      bool        `json:"enabled"`
}

// skillTypeFromID infers skill type from metadata or ID convention.
func skillTypeFromID(s skill.Skill) string {
	// Check if the skill has Wasm bytes (indicates wasm type)
	if ws, ok := s.(interface{ WasmBytes() []byte }); ok && len(ws.WasmBytes()) > 0 {
		return "wasm"
	}
	// Check if the skill has LLM dependency
	if _, ok := s.(interface{ UsesLLM() bool }); ok {
		return "llm"
	}
	return "code"
}

func (r *Router) handleSkills(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.skills == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]SkillResponse{})
		return
	}

	ids := r.skills.List()
	skills := make([]SkillResponse, 0, len(ids))

	for _, id := range ids {
		s, err := r.skills.Get(id)
		if err != nil {
			continue
		}

		resp := SkillResponse{
			ID:          s.ID(),
			Name:        s.Name(),
			Description: s.Description(),
			Type:        skillTypeFromID(s),
			Version:     "1.0.0",
			Enabled:     true,
		}

		if schema := s.InputSchema(); schema != nil {
			resp.InputSchema = schema
		}
		if schema := s.OutputSchema(); schema != nil {
			resp.OutputSchema = schema
		}

		skills = append(skills, resp)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(skills)
}

// ExecutionStore provides access to execution history.
type ExecutionStore interface {
	QueryExecutions(ctx context.Context, limit int) ([]ExecutionRecord, error)
}

// ExecutionRecord is a single execution entry.
type ExecutionRecord struct {
	ExecutionID string `json:"execution_id"`
	SessionID   string `json:"session_id"`
	SkillID     string `json:"skill_id"`
	DurationMs  int64  `json:"duration_ms"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

func (r *Router) handleExecutions(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.execStore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ExecutionRecord{})
		return
	}

	records, err := r.execStore.QueryExecutions(req.Context(), 50)
	if err != nil {
		http.Error(w, "failed to query executions", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(records)
}
