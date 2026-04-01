package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
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
