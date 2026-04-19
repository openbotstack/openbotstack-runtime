package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

// mockAgent implements agent.Agent for testing.
type mockAgent struct {
	response *agent.MessageResponse
	err      error
}

func (m *mockAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &agent.MessageResponse{
		SessionID: req.SessionID,
		Message:   "Mock response for: " + req.Message,
		SkillUsed: "mock/skill",
	}, nil
}

func TestHealthEndpoint(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp map[string]string
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", resp["status"])
	}
}

func TestChatEndpoint(t *testing.T) {
	mockResp := &agent.MessageResponse{
		SessionID: "session-1",
		Message:   "Hello! I'm here to help.",
		SkillUsed: "core/greeting",
	}
	handler := api.NewRouter(&mockAgent{response: mockResp})

	body := api.ChatRequest{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   "Hello",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	var resp api.ChatResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Message != "Hello! I'm here to help." {
		t.Errorf("Expected greeting message, got '%s'", resp.Message)
	}
	if resp.SkillUsed != "core/greeting" {
		t.Errorf("Expected skill core/greeting, got '%s'", resp.SkillUsed)
	}
}

func TestChatEndpointBadRequest(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}

func TestChatEndpointAgentError(t *testing.T) {
	handler := api.NewRouter(&mockAgent{err: errors.New("agent unavailable")})

	body := api.ChatRequest{Message: "Hello"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rr.Code)
	}
}

func TestChatEndpointNilAgent(t *testing.T) {
	handler := api.NewRouter(nil)

	body := api.ChatRequest{Message: "Hello"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", rr.Code)
	}
}

func TestHistoryEndpoint(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("GET", "/v1/sessions/session-1/history", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestNotFoundEndpoint(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rr.Code)
	}
}

func TestChatEndpointMethodNotAllowed(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("GET", "/v1/chat", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rr.Code)
	}
}

// captureAgent captures the MessageRequest for inspection.
type captureAgent struct {
	captured *agent.MessageRequest
}

func (c *captureAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	c.captured = &req
	return &agent.MessageResponse{
		SessionID: req.SessionID,
		Message:   "ok",
	}, nil
}

func TestChatEndpointUsesAuthenticatedIdentity(t *testing.T) {
	ca := &captureAgent{}
	router := api.NewRouter(ca)

	// Set auth middleware that injects a user into context
	router.SetAuthMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := &auth.User{ID: "auth-user", TenantID: "auth-tenant"}
			ctx := middleware.WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	// Request body has different tenant/user than the authenticated one
	body := api.ChatRequest{
		TenantID:  "body-tenant",
		UserID:    "body-user",
		SessionID: "session-1",
		Message:   "Hello",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	// Authenticated identity should override request body
	if ca.captured.TenantID != "auth-tenant" {
		t.Errorf("TenantID = %q, want %q (from auth context)", ca.captured.TenantID, "auth-tenant")
	}
	if ca.captured.UserID != "auth-user" {
		t.Errorf("UserID = %q, want %q (from auth context)", ca.captured.UserID, "auth-user")
	}
}

// mockHistoryProvider implements api.HistoryProvider for testing.
type mockHistoryProvider struct {
	sessions    []api.SessionSummary
	history     []api.Message
	deleteErr   error
	deletedID   string
}

func (m *mockHistoryProvider) GetSessionHistory(ctx context.Context, sessionID string) ([]api.Message, error) {
	return m.history, nil
}

func (m *mockHistoryProvider) ListSessions(ctx context.Context) ([]api.SessionSummary, error) {
	return m.sessions, nil
}

func (m *mockHistoryProvider) DeleteSession(ctx context.Context, sessionID string) error {
	m.deletedID = sessionID
	return m.deleteErr
}

func TestListSessionsEndpoint(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	handler.SetHistoryProvider(&mockHistoryProvider{
		sessions: []api.SessionSummary{
			{SessionID: "s1", TenantID: "t1", LastEntry: "hello", EntryCount: 3, CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-02T00:00:00Z"},
			{SessionID: "s2", TenantID: "t1", LastEntry: "world", EntryCount: 1, CreatedAt: "2025-01-03T00:00:00Z", UpdatedAt: "2025-01-03T00:00:00Z"},
		},
	})

	req := httptest.NewRequest("GET", "/v1/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var sessions []api.SessionSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &sessions)
	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].SessionID != "s1" {
		t.Errorf("Expected session s1, got %s", sessions[0].SessionID)
	}
	if sessions[0].EntryCount != 3 {
		t.Errorf("Expected entry count 3, got %d", sessions[0].EntryCount)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	// No history provider set

	req := httptest.NewRequest("GET", "/v1/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var sessions []api.SessionSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &sessions)
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions without provider, got %d", len(sessions))
	}
}

func TestDeleteSessionEndpoint(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	mockHistory := &mockHistoryProvider{}
	handler.SetHistoryProvider(mockHistory)

	req := httptest.NewRequest("DELETE", "/v1/sessions/session-123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", rr.Code)
	}
	if mockHistory.deletedID != "session-123" {
		t.Errorf("Expected deleted session-123, got %s", mockHistory.deletedID)
	}
}

func TestDeleteSessionMissingID(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	handler.SetHistoryProvider(&mockHistoryProvider{})

	req := httptest.NewRequest("DELETE", "/v1/sessions/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing session ID, got %d", rr.Code)
	}
}

func TestDeleteSessionNoProvider(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	// No history provider set

	req := httptest.NewRequest("DELETE", "/v1/sessions/session-123", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 without provider, got %d", rr.Code)
	}
}

func TestSessionHistoryEndpointWithProvider(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})
	handler.SetHistoryProvider(&mockHistoryProvider{
		history: []api.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
		},
	})

	req := httptest.NewRequest("GET", "/v1/sessions/sess-1/history", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var resp api.HistoryResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.SessionID != "sess-1" {
		t.Errorf("Expected session ID sess-1, got %s", resp.SessionID)
	}
	if len(resp.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(resp.Messages))
	}
}

func TestChatEndpointNoAuthUsesBodyIdentity(t *testing.T) {
	ca := &captureAgent{}
	router := api.NewRouter(ca)
	// No auth middleware set

	body := api.ChatRequest{
		TenantID:  "body-tenant",
		UserID:    "body-user",
		SessionID: "session-1",
		Message:   "Hello",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	// Without auth, body values should be used as-is
	if ca.captured.TenantID != "body-tenant" {
		t.Errorf("TenantID = %q, want %q (from request body)", ca.captured.TenantID, "body-tenant")
	}
	if ca.captured.UserID != "body-user" {
		t.Errorf("UserID = %q, want %q (from request body)", ca.captured.UserID, "body-user")
	}
}
