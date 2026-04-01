// Package api provides the REST API for OpenBotStack runtime.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/openbotstack/openbotstack-core/control/agent"
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

// HistoryProvider gives access to conversation history.
type HistoryProvider interface {
	// GetSessionHistory retrieves messages for a session.
	GetSessionHistory(ctx context.Context, sessionID string) ([]Message, error)
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
	mux        *http.ServeMux
	v1Mux      *http.ServeMux
	v1Handler  http.Handler
	agent      agent.Agent
	skills     SkillProvider
	execStore  ExecutionStore
	history    HistoryProvider
	metrics    *Metrics
}

// NewRouter creates a new API router with an Agent.
func NewRouter(a agent.Agent) *Router {
	r := &Router{
		mux:     http.NewServeMux(),
		v1Mux:   http.NewServeMux(),
		agent:   a,
		metrics: NewMetrics(),
	}
	r.v1Handler = r.v1Mux
	r.registerRoutes()
	return r
}

// SetAuthMiddleware sets an authentication middleware to be used for all /v1/ endpoints.
// This should be called before requests begin processing.
func (r *Router) SetAuthMiddleware(mw func(http.Handler) http.Handler) {
	if mw != nil {
		r.v1Handler = mw(r.v1Mux)
	}
}

// SetSkillProvider sets the skill registry for the /v1/skills endpoint.
func (r *Router) SetSkillProvider(sp SkillProvider) {
	r.skills = sp
}

// SetExecutionStore sets the execution store for the /v1/executions endpoint.
func (r *Router) SetExecutionStore(es ExecutionStore) {
	r.execStore = es
}

// SetHistoryProvider sets the history provider for the /v1/sessions/{id}/history endpoint.
func (r *Router) SetHistoryProvider(hp HistoryProvider) {
	r.history = hp
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) registerRoutes() {
	r.mux.HandleFunc("/health", r.handleHealth)
	r.mux.HandleFunc("/healthz", r.handleHealth)
	r.mux.HandleFunc("/readyz", r.handleReady)
	r.mux.HandleFunc("/metrics", r.metrics.Handler())
	
	// Register v1 routes on v1Mux
	r.v1Mux.HandleFunc("/v1/chat", r.handleChat)
	r.v1Mux.HandleFunc("/v1/skills", r.handleSkills)
	r.v1Mux.HandleFunc("/v1/executions", r.handleExecutions)
	r.v1Mux.HandleFunc("/v1/sessions/", r.handleSessions)

	// Route /v1/ traffic to the potentially wrapped v1Handler
	r.mux.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.v1Handler.ServeHTTP(w, req)
	}))

	r.mux.HandleFunc("/", r.handleNotFound)
}

func (r *Router) handleHealth(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (r *Router) handleReady(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// For now, always return ready. Later we can add checks for database/redis availability.
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
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

	messages := []Message{}
	if r.history != nil {
		var err error
		messages, err = r.history.GetSessionHistory(req.Context(), sessionID)
		if err != nil {
			http.Error(w, "failed to get session history", http.StatusInternalServerError)
			return
		}
	}

	resp := HistoryResponse{
		SessionID: sessionID,
		Messages:  messages,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (r *Router) handleNotFound(w http.ResponseWriter, req *http.Request) {
	// Only trigger for truly unmatched paths
	if req.URL.Path == "/" || req.URL.Path == "/health" || req.URL.Path == "/healthz" || req.URL.Path == "/readyz" ||
		strings.HasPrefix(req.URL.Path, "/v1/") {
		return
	}
	http.NotFound(w, req)
}
