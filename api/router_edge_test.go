package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api"
)

// ==================== Edge Case Tests ====================

func TestChatEndpointEmptyBody(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte{}))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty body, got %d", rr.Code)
	}
}

func TestChatEndpointNullBody(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte("null")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// null is valid JSON, so should be OK (but may have empty message)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for null body, got %d", rr.Code)
	}
}

func TestChatEndpointVeryLongMessage(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	// 100KB message
	longMessage := strings.Repeat("a", 100*1024)
	body := api.ChatRequest{Message: longMessage}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should handle without crashing (actual limit is app-dependent)
	if rr.Code == 0 {
		t.Error("Server crashed on large message")
	}
}

func TestChatEndpointSpecialCharacters(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	tests := []string{
		`{"message": "Hello 你好 مرحبا"}`,            // Unicode
		`{"message": "Hello\nWorld\tTab"}`,         // Escape sequences
		`{"message": "He said \"hello\""}`,         // Quotes
		`{"message": "<script>alert(1)</script>"}`, // XSS attempt
		`{"message": "' OR 1=1; --"}`,              // SQL injection attempt
	}

	for _, body := range tests {
		req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Failed to handle special chars in: %s, got status %d", body, rr.Code)
		}
	}
}

func TestChatEndpointMissingFields(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	// Only message, no tenant/user/session
	body := `{"message": "Hello"}`
	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should still work - other fields are optional
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestChatEndpointExtraFields(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	// Extra unknown field - should be ignored
	body := `{"message": "Hello", "unknown_field": "value", "skill_id": "hacked"}`
	req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should succeed - extra fields ignored
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Verify the response skill came from agent, not the request
	var resp api.ChatResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.SkillUsed == "hacked" {
		t.Error("Bug: skill_id from request was used - this is a security issue!")
	}
}

func TestHistoryEndpointInvalidSession(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	// Very long session ID
	req := httptest.NewRequest("GET", "/v1/sessions/"+strings.Repeat("x", 10000)+"/history", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should handle without crashing
	if rr.Code == 0 {
		t.Error("Server crashed on long session ID")
	}
}

func TestHistoryEndpointNoSession(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	req := httptest.NewRequest("GET", "/v1/sessions//history", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should return 404 or handle gracefully
	if rr.Code == 0 {
		t.Error("Server crashed on empty session ID")
	}
}

// ==================== Concurrency Tests ====================

func TestConcurrentRequests(t *testing.T) {
	handler := api.NewRouter(&mockAgent{})

	const numRequests = 50
	done := make(chan bool, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			body := api.ChatRequest{
				Message:   "Hello",
				SessionID: "session-" + string(rune(id)),
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/v1/chat", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Request %d failed with status %d", id, rr.Code)
			}
			done <- true
		}(i)
	}

	timeout := time.After(10 * time.Second)
	for i := 0; i < numRequests; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent requests")
		}
	}
}
