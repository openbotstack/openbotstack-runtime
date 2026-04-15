package integration

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
)

func TestStreaming_SSEFormat(t *testing.T) {
	if !isServerHealthy() {
		t.Skip("Server not running. Run TestFullSystem first.")
	}

	body := `{"message":"summarize this text","session_id":"stream-test"}`
	resp, err := http.Post(serverURL+"/v1/chat/stream", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check SSE content type
	ct := resp.Header.Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// Read SSE events from response body
	scanner := bufio.NewScanner(resp.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "data:") {
			events = append(events, line)
		}
	}

	// Verify at least session and done events exist
	hasSession := false
	hasDone := false
	for _, e := range events {
		if strings.HasPrefix(e, "event: session") {
			hasSession = true
		}
		if strings.HasPrefix(e, "event: done") {
			hasDone = true
		}
	}

	if !hasSession {
		t.Error("expected 'event: session' in SSE stream")
	}
	if !hasDone {
		t.Error("expected 'event: done' in SSE stream")
	}
}

func TestStreaming_ErrorReturnsJSON(t *testing.T) {
	if !isServerHealthy() {
		t.Skip("Server not running. Run TestFullSystem first.")
	}

	// GET on streaming endpoint should return JSON error, not SSE
	resp, err := http.Get(serverURL + "/v1/chat/stream")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
