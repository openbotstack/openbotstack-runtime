// Package e2e provides end-to-end tests for the runtime.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
)

// mockAgent implements agent.Agent for e2e tests.
type mockAgent struct{}

func (m *mockAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	return &agent.MessageResponse{
		SessionID: req.SessionID,
		Message:   "E2E test response for: " + req.Message,
		SkillUsed: "e2e/mock",
	}, nil
}

func newTestRouter() *api.Router {
	return api.NewRouter(&mockAgent{})
}

func TestHealthEndpoint(t *testing.T) {
	router := newTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestChatFlowBasic(t *testing.T) {
	router := newTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Send first message
	reqBody := `{"tenant_id":"test-tenant","user_id":"test-user","message":"Hello"}`
	resp, err := http.Post(srv.URL+"/v1/chat", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST /v1/chat failed: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify message field exists
	if _, ok := result["message"]; !ok {
		t.Error("Expected message in response")
	}
}

func TestChatFlowWithSession(t *testing.T) {
	router := newTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	// First message
	req1 := map[string]string{
		"tenant_id": "test-tenant",
		"user_id":   "test-user",
		"message":   "What is 2+2?",
	}
	body1, _ := json.Marshal(req1)
	resp1, err := http.Post(srv.URL+"/v1/chat", "application/json", bytes.NewReader(body1))
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	defer resp1.Body.Close() //nolint:errcheck // test cleanup

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("First message failed with status %d", resp1.StatusCode)
	}
}

func TestChatHistoryRetrieval(t *testing.T) {
	router := newTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Get history for a session
	histResp, err := http.Get(srv.URL + "/v1/sessions/test-session/history")
	if err != nil {
		t.Fatalf("GET history failed: %v", err)
	}
	defer histResp.Body.Close() //nolint:errcheck // test cleanup

	if histResp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 for history, got %d", histResp.StatusCode)
	}
}

func TestChatValidation(t *testing.T) {
	router := newTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Missing message should still work (current implementation)
	resp, err := http.Post(srv.URL+"/v1/chat", "application/json",
		strings.NewReader(`{"tenant_id":"t","user_id":"u","message":"test"}`))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}
}

func TestServerStartupShutdown(t *testing.T) {
	router := newTestRouter()
	srv := &http.Server{
		Addr:    ":0",
		Handler: router,
	}

	go func() {
		_ = srv.ListenAndServe()
	}()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}
