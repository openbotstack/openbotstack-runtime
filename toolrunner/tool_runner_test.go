package toolrunner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

func TestRegistryClient_Invoke(t *testing.T) {
	// Mock tool response
	mockOutput := []byte(`{"result":"success"}`)
	
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/invoke" {
			t.Errorf("Expected path /invoke, got %s", r.URL.Path)
		}
		
		var reqBody struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
			Meta      map[string]string `json:"meta"`
		}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("Failed to decode request body: %v", err)
		}
		
		if reqBody.Tool != "test-tool" {
			t.Errorf("Expected tool test-tool, got %s", reqBody.Tool)
		}
		if reqBody.Meta["tenant_id"] != "tenant-1" {
			t.Errorf("Expected tenant_id tenant-1, got %s", reqBody.Meta["tenant_id"])
		}

		w.WriteHeader(http.StatusOK)
		resp := struct {
			Output json.RawMessage `json:"output"`
			Error  string          `json:"error"`
		}{
			Output: mockOutput,
		}
			_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	tc := NewToolContext(context.Background(), ec)

	output, err := client.Invoke(tc, "test-tool", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}

	if string(output) != string(mockOutput) {
		t.Errorf("Expected output %s, got %s", string(mockOutput), string(output))
	}
}

func TestRegistryClient_Invoke_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Output json.RawMessage `json:"output"`
			Error  string          `json:"error"`
		}{
			Error: "tool execution failed",
		}
			_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	tc := NewToolContext(context.Background(), ec)

	_, err := client.Invoke(tc, "test-tool", nil)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if err.Error() != "tool error: tool execution failed" {
		t.Errorf("Expected specific error message, got %v", err)
	}
}

func TestRegistryToolRunner_Execute(t *testing.T) {
	mockOutput := []byte(`{"ok":true}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := struct {
			Output json.RawMessage `json:"output"`
			Error  string          `json:"error"`
		}{
			Output: mockOutput,
		}
			_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewRegistryClient(server.URL)
	runner := NewRegistryToolRunner(client)
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")

	// Start a timer for duration check
	start := time.Now()
	result, err := runner.Execute(context.Background(), "test-tool", nil, ec)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.StepName != "test-tool" {
		t.Errorf("Expected step name test-tool, got %s", result.StepName)
	}
	
	outputBytes, ok := result.Output.([]byte)
	if !ok {
		t.Fatalf("Expected []byte output, got %T", result.Output)
	}
	
	if string(outputBytes) != string(mockOutput) {
		t.Errorf("Expected output %s, got %s", string(mockOutput), string(outputBytes))
	}
	if result.Duration > duration || result.Duration == 0 {
		t.Errorf("Invalid duration: %v", result.Duration)
	}
}
