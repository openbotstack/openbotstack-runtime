// Package api provides the REST API for OpenBotStack runtime.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	

	"github.com/openbotstack/openbotstack-core/control/agent"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/harness"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
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
	SessionID   string `json:"session_id"`
	Message     string `json:"message"`
	SkillUsed   string `json:"skill_used,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
}

// HistoryResponse contains conversation history.
type HistoryResponse struct {
	SessionID string           `json:"session_id"`
	Messages  []aitypes.Message `json:"messages"`
}

// HistoryProvider gives access to conversation history.
type HistoryProvider interface {
	// GetSessionHistory retrieves messages for a session.
	GetSessionHistory(ctx context.Context, sessionID string) ([]aitypes.Message, error)
	// ListSessions returns all sessions for the current tenant.
	ListSessions(ctx context.Context) ([]SessionSummary, error)
	// DeleteSession removes all entries for a session.
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionSummary is a summary of a session for listing.
type SessionSummary struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	LastEntry string `json:"last_entry"`
	EntryCount int   `json:"entry_count"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// RouterConfig holds all dependencies for constructing a Router.
type RouterConfig struct {
	Agent agent.Agent

	// Optional dependencies
	AuthMiddleware   func(http.Handler) http.Handler
	Skills           SkillProvider
	SkillDisabled    func(id string) bool
	ExecStore        ExecutionStore
	History          HistoryProvider
	HealthCheckers   []HealthChecker
	BuildInfo        BuildInfo
	ReasoningStore   reasoning.Store
	TelemetryHandler *TelemetryHandler
	LineageBuilder   *LineageBuilder
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
	mux             *http.ServeMux
	v1Mux           *http.ServeMux
	v1Handler       http.Handler
	agent           agent.Agent
	skills          SkillProvider
	skillDisabled   func(id string) bool
	execStore       ExecutionStore
	history         HistoryProvider
	healthCheckers  []HealthChecker
	buildInfo       BuildInfo
	reasoningStore   reasoning.Store
	telemetryHandler *TelemetryHandler
	lineageBuilder   *LineageBuilder
}

// NewRouter creates a new API router from a RouterConfig.
func NewRouter(cfg RouterConfig) *Router {
	r := &Router{
		mux:             http.NewServeMux(),
		v1Mux:           http.NewServeMux(),
		agent:           cfg.Agent,
		skills:          cfg.Skills,
		skillDisabled:   cfg.SkillDisabled,
		execStore:       cfg.ExecStore,
		history:         cfg.History,
		healthCheckers:  cfg.HealthCheckers,
		buildInfo:       cfg.BuildInfo,
		reasoningStore:   cfg.ReasoningStore,
		telemetryHandler: cfg.TelemetryHandler,
		lineageBuilder:   cfg.LineageBuilder,
	}
	r.v1Handler = r.v1Mux
	if cfg.AuthMiddleware != nil {
		r.v1Handler = cfg.AuthMiddleware(r.v1Mux)
	}
	r.registerRoutes()
	return r
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

func (r *Router) registerRoutes() {
	r.mux.HandleFunc("/health", r.handleHealthz)
	r.mux.HandleFunc("/healthz", r.handleHealthz)
	r.mux.HandleFunc("/readyz", r.handleReadyz)
	r.mux.HandleFunc("/version", r.handleVersion)

	// Register v1 routes on v1Mux
	r.v1Mux.HandleFunc("/v1/chat", r.handleChat)
	r.v1Mux.HandleFunc("/v1/chat/stream", r.handleChatStream)
	r.v1Mux.HandleFunc("/v1/chat/completions", r.handleChatCompletions)
	r.v1Mux.HandleFunc("/v1/skills", r.handleSkills)
	r.v1Mux.HandleFunc("/v1/executions", r.handleExecutions)
	r.v1Mux.HandleFunc("/v1/sessions", r.handleListSessions)
	r.v1Mux.HandleFunc("/v1/sessions/", r.handleSessions)
	r.v1Mux.HandleFunc("/v1/execution/", r.handleExecutionAction)
	r.v1Mux.HandleFunc("/v1/me", HandleMe)

	// Route /v1/ traffic to the potentially wrapped v1Handler
	r.mux.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.v1Handler.ServeHTTP(w, req)
	}))

	r.mux.HandleFunc("/", r.handleNotFound)
}

// agentRequest is the shared preamble for all three chat endpoints
// (/v1/chat, /v1/chat/stream, /v1/chat/completions): it guards on agent
// availability and assembles an agent.MessageRequest from the supplied
// identity + message, with the authenticated context identity overriding
// tenant/user. Each endpoint still does its own method guard (must precede
// body decode so a GET yields 405, not 400) and its own request decoding.
// Returns ok=false after writing a 503.
func (r *Router) agentRequest(w http.ResponseWriter, req *http.Request, tenantID, userID, sessionID, message string) (agent.MessageRequest, bool) {
	if r.agent == nil {
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusServiceUnavailable,
			"error", "agent not configured",
		)
		writeAPIError(w, http.StatusServiceUnavailable, ErrAgentNotConfigured, "agent not configured")
		return agent.MessageRequest{}, false
	}
	ar := agent.MessageRequest{
		TenantID:  tenantID,
		UserID:    userID,
		SessionID: sessionID,
		Message:   message,
	}
	if user, ok := middleware.UserFromContext(req.Context()); ok {
		ar.TenantID = user.TenantID
		ar.UserID = user.ID
	}
	return ar, true
}

func (r *Router) handleChat(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusMethodNotAllowed,
			"error", "method not allowed",
		)
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var chatReq ChatRequest
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20) // 1MB limit
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusBadRequest,
			"error", "invalid request body",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}

	// Validate message content
	if strings.TrimSpace(chatReq.Message) == "" {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusBadRequest,
			"error", "empty message",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "message is required")
		return
	}

	// Delegate entirely to Agent - NO skill selection logic here
	agentReq, ok := r.agentRequest(w, req, chatReq.TenantID, chatReq.UserID, chatReq.SessionID, chatReq.Message)
	if !ok {
		return
	}

	agentResp, err := r.agent.HandleMessage(req.Context(), agentReq)
	if err != nil {
		// Agent handles errors gracefully, but in case of critical failure
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "internal error")
		return
	}

	resp := ChatResponse{
		SessionID:   agentResp.SessionID,
		Message:     agentResp.Message,
		SkillUsed:   agentResp.SkillUsed,
		ExecutionID: agentResp.ExecutionID,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (r *Router) handleChatStream(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusMethodNotAllowed,
			"error", "method not allowed",
		)
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var chatReq ChatRequest
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20) // 1MB limit
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}

	// Validate message content
	if strings.TrimSpace(chatReq.Message) == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "message is required")
		return
	}

	agentReq, ok := r.agentRequest(w, req, chatReq.TenantID, chatReq.UserID, chatReq.SessionID, chatReq.Message)
	if !ok {
		return
	}

	// Create a single SSE handler for the entire stream lifecycle.
	// Headers + `:ok\n\n` are sent immediately to unblock the client.
	sseHandler := NewSSEHandler(w)
	var tokensStreamed bool

	agentReq.ProgressCallback = func(eventType, content string, turn int, tool string) {
		if eventType == "token" {
			tokensStreamed = true
		}
		data, _ := json.Marshal(harness.ProgressEvent{Type: eventType, Content: content, Turn: turn, Tool: tool})
		_ = sseHandler.WriteEvent(SSEEvent{Event: "progress", Data: string(data)})
	}

	agentResp, err := r.agent.HandleMessage(req.Context(), agentReq)
	if err != nil {
		slog.ErrorContext(req.Context(), "streaming handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		errData, _ := json.Marshal(map[string]string{"error": "internal error", "code": ErrInternal})
		_ = sseHandler.WriteEvent(SSEEvent{Event: "error", Data: string(errData)})
		return
	}

	// Mark message as already streamed if tokens were emitted during execution
	if agentResp != nil && tokensStreamed {
		agentResp.Message = ""
	}

	// Reuse the same handler for final events (no second WriteHeader)
	handler := sseHandler

	// Emit the message as a token event so clients always see content via the token path.
	// For LLM-based skills, tokens were already streamed during execution.
	// For Wasm/native skills, this is the single token that carries the full output.
	if agentResp.Message != "" {
		tokenData, _ := json.Marshal(harness.ProgressEvent{Type: "token", Content: agentResp.Message})
		_ = handler.WriteEvent(SSEEvent{Event: "progress", Data: string(tokenData)})
	}

	sessionJSON, _ := json.Marshal(map[string]string{"session_id": agentResp.SessionID})
	_ = handler.WriteEvent(SSEEvent{
		Event: "session",
		Data:  string(sessionJSON),
	})
	donePayload := map[string]string{"execution_id": agentResp.ExecutionID}
	if agentResp.SkillUsed != "" {
		donePayload["skill_used"] = agentResp.SkillUsed
	}
	doneJSON, _ := json.Marshal(donePayload)
	_ = handler.WriteEvent(SSEEvent{
		Event: "done",
		Data:  string(doneJSON),
	})
}

func (r *Router) handleSessions(w http.ResponseWriter, req *http.Request) {
	// Extract session ID from path: /v1/sessions/{sessionID}/history or /v1/sessions/{sessionID}
	path := strings.TrimPrefix(req.URL.Path, "/v1/sessions/")
	parts := strings.Split(path, "/")

	sessionID := parts[0]
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "missing session ID")
		return
	}

	// DELETE /v1/sessions/{sessionID}
	if req.Method == http.MethodDelete {
		r.deleteSessionByID(w, req, sessionID)
		return
	}

	// GET /v1/sessions/{sessionID}/history
	if len(parts) < 2 || parts[1] != "history" {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusNotFound,
			"error", "not found",
		)
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "not found")
		return
	}

	messages := []aitypes.Message{}
	if r.history != nil {
		var err error
		messages, err = r.history.GetSessionHistory(req.Context(), sessionID)
		if err != nil {
			slog.ErrorContext(req.Context(), "handler error",
				"method", req.Method,
				"path", req.URL.Path,
				"status", http.StatusInternalServerError,
				"error", err,
			)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to get session history")
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

func (r *Router) handleListSessions(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodGet {
		r.listSessions(w, req)
		return
	}
	writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
}

func (r *Router) listSessions(w http.ResponseWriter, req *http.Request) {
	if r.history == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]SessionSummary{})
		return
	}

	sessions, err := r.history.ListSessions(req.Context())
	if err != nil {
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list sessions")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessions)
}


func (r *Router) deleteSessionByID(w http.ResponseWriter, req *http.Request, sessionID string) {
	if r.history == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "history provider not configured")
		return
	}

	if err := r.history.DeleteSession(req.Context(), sessionID); err != nil {
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete session")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
func (r *Router) handleNotFound(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/" {
		return
	}
	http.NotFound(w, req)
}
