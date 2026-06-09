package toolrunner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-core/execution"
)

// TestRegistryClientImplementsToolRunner verifies that RegistryClient
// directly satisfies the ToolRunner interface without a wrapper.
func TestRegistryClientImplementsToolRunner(t *testing.T) {
	// Compile-time check: RegistryClient must implement ToolRunner.
	var _ ToolRunner = (*RegistryClient)(nil)
}

func TestRegistryClient_Execute(t *testing.T) {
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
	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")

	// Call Execute directly on RegistryClient (no RegistryToolRunner wrapper).
	result, err := client.Execute(context.Background(), "test-tool", map[string]any{"key": "value"}, ec)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.StepName != "test-tool" {
		t.Errorf("StepName = %q, want %q", result.StepName, "test-tool")
	}
	if result.Type != "tool" {
		t.Errorf("Type = %q, want %q", result.Type, "tool")
	}
	if result.Duration == 0 {
		t.Error("Duration should be non-zero")
	}

	outputBytes, ok := result.Output.([]byte)
	if !ok {
		t.Fatalf("Output type = %T, want []byte", result.Output)
	}
	if string(outputBytes) != string(mockOutput) {
		t.Errorf("Output = %s, want %s", string(outputBytes), string(mockOutput))
	}
}

func TestRegistryClient_Execute_Error(t *testing.T) {
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

	result, err := client.Execute(context.Background(), "test-tool", nil, ec)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if result == nil {
		t.Fatal("Result should not be nil even on error")
	}
	if result.Error == nil {
		t.Error("Result.Error should be set")
	}
}
