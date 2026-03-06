// Package api provides the REST API for OpenBotStack runtime.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/openbotstack/openbotstack-runtime/agent"
)

// ChatRequest is the input for chat endpoint.
type ChatRequest struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ChatResponse is the output from chat endpoint.
type ChatResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	SkillUsed string `json:"skill_used,omitempty"`
}

// HistoryResponse contains conversation history.
type HistoryResponse struct {
	SessionID string    `json:"session_id"`
	Messages  []Message `json:"messages"`
}

// Message represents a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Router handles HTTP routing.
//
// The Router is responsible ONLY for:
//   - HTTP request/response handling
//   - Request validation
//   - Session management
//   - Delegating to Agent
//
// The Router does NOT:
//   - Select skills (that's the Agent's job)
//   - Execute skills (that's the Executor's job)
//   - Make any decisions about which skill to use
type Router struct {
	mux       *http.ServeMux
	agent     agent.Agent
	skills    SkillProvider
	execStore ExecutionStore
}

// NewRouter creates a new API router with an Agent.
func NewRouter(a agent.Agent) *Router {
	r := &Router{
		mux:   http.NewServeMux(),
		agent: a,
	}
	r.registerRoutes()
	return r
}

// SetSkillProvider sets the skill registry for the /v1/skills endpoint.
func (r *Router) SetSkillProvider(sp SkillProvider) {
	r.skills = sp
}

// SetExecutionStore sets the execution store for the /v1/executions endpoint.
func (r *Router) SetExecutionStore(es ExecutionStore) {
	r.execStore = es
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) registerRoutes() {
	r.mux.HandleFunc("/health", r.handleHealth)
	r.mux.HandleFunc("/v1/chat", r.handleChat)
	r.mux.HandleFunc("/v1/skills", r.handleSkills)
	r.mux.HandleFunc("/v1/executions", r.handleExecutions)
	r.mux.HandleFunc("/v1/sessions/", r.handleSessions)
	r.mux.HandleFunc("/", r.handleNotFound)
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (r *Router) handleChat(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chatReq ChatRequest
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Delegate entirely to Agent - NO skill selection logic here
	if r.agent == nil {
		http.Error(w, "agent not configured", http.StatusServiceUnavailable)
		return
	}

	agentReq := agent.MessageRequest{
		TenantID:  chatReq.TenantID,
		UserID:    chatReq.UserID,
		SessionID: chatReq.SessionID,
		Message:   chatReq.Message,
	}

	agentResp, err := r.agent.HandleMessage(req.Context(), agentReq)
	if err != nil {
		// Agent handles errors gracefully, but in case of critical failure
		http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := ChatResponse{
		SessionID: agentResp.SessionID,
		Message:   agentResp.Message,
		SkillUsed: agentResp.SkillUsed,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (r *Router) handleSessions(w http.ResponseWriter, req *http.Request) {
	// Extract session ID from path: /v1/sessions/{sessionID}/history
	path := strings.TrimPrefix(req.URL.Path, "/v1/sessions/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "history" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	sessionID := parts[0]

	// TODO: Retrieve actual history
	resp := HistoryResponse{
		SessionID: sessionID,
		Messages:  []Message{},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (r *Router) handleNotFound(w http.ResponseWriter, req *http.Request) {
	// Only trigger for truly unmatched paths
	if req.URL.Path == "/" || req.URL.Path == "/health" ||
		strings.HasPrefix(req.URL.Path, "/v1/") {
		return
	}
	http.NotFound(w, req)
}
