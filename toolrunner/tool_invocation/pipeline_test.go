package tool_invocation

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

func TestInvoke_HTTPTool(t *testing.T) {
	// Create test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	// Create pipeline with wildcard allowlist
	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)
	pipeline := NewToolInvocationPipeline(client, nil, 10*time.Second)

	result, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: ts.URL + "/test",
		Type: "http",
		Arguments: map[string]any{
			"url":    ts.URL + "/test",
			"method": "GET",
		},
	})

	if err != nil {
		t.Fatalf("Invoke failed: %v", err)
	}
	if result.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if string(result.Output) != `{"status":"ok"}` {
		t.Errorf("unexpected output: %s", result.Output)
	}
	if result.Duration == 0 {
		t.Error("expected non-zero duration")
	}
}

func TestInvoke_HTTPTool_Blocked(t *testing.T) {
	// Empty allowlist blocks everything
	allowlist := wasm.NewHTTPAllowlist([]string{})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)
	pipeline := NewToolInvocationPipeline(client, nil, 10*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: "https://evil.example.com/api",
		Type: "http",
		Arguments: map[string]any{
			"url":    "https://evil.example.com/api",
			"method": "GET",
		},
	})

	if err == nil {
		t.Fatal("expected error for blocked URL")
	}
}

func TestInvoke_HTTPTool_NilClient(t *testing.T) {
	pipeline := NewToolInvocationPipeline(nil, nil, 5*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: "https://example.com",
		Type: "http",
		Arguments: map[string]any{
			"url": "https://example.com",
		},
	})

	if err != ErrNilHTTPClient {
		t.Errorf("expected ErrNilHTTPClient, got %v", err)
	}
}

func TestInvoke_RegistryTool_NilClient(t *testing.T) {
	pipeline := NewToolInvocationPipeline(nil, nil, 5*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: "some-tool",
		Type: "registry",
		Arguments: map[string]any{
			"input": "test",
		},
	})

	if err != ErrNilRegistryClient {
		t.Errorf("expected ErrNilRegistryClient, got %v", err)
	}
}

func TestInvoke_UnsupportedType(t *testing.T) {
	pipeline := NewToolInvocationPipeline(nil, nil, 5*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: "test",
		Type: "unknown",
	})

	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !errors.Is(err, ErrUnsupportedToolType) {
		t.Errorf("expected ErrUnsupportedToolType, got %v", err)
	}
}

func TestInvoke_MissingName(t *testing.T) {
	pipeline := NewToolInvocationPipeline(nil, nil, 5*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: "",
		Type: "http",
	})

	if err != ErrMissingToolName {
		t.Errorf("expected ErrMissingToolName, got %v", err)
	}
}

func TestWireHTTPFetch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("response body"))
	}))
	defer ts.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)
	pipeline := NewToolInvocationPipeline(client, nil, 10*time.Second)

	hf := &wasm.HostFunctions{}
	WireHTTPFetch(hf, pipeline)

	if hf.HTTPFetch == nil {
		t.Fatal("HTTPFetch was not wired")
	}

	body, statusCode, err := hf.HTTPFetch(context.Background(), ts.URL+"/test", "GET", nil)
	if err != nil {
		t.Fatalf("HTTPFetch failed: %v", err)
	}
	if statusCode != 200 {
		t.Errorf("expected status 200, got %d", statusCode)
	}
	if string(body) != "response body" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestInvoke_HTTPTool_WithSSRF(t *testing.T) {
	// Verify pipeline works with SSRF-protected client
	// httptest server binds to 127.0.0.1, so SSRF protection should block it
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not reach"))
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)
	pipeline := NewToolInvocationPipeline(client, nil, 10*time.Second)

	_, err := pipeline.Invoke(context.Background(), ToolInvocation{
		Name: server.URL + "/test",
		Type: "http",
		Arguments: map[string]any{
			"url":    server.URL + "/test",
			"method": "GET",
		},
	})

	if err == nil {
		t.Fatal("expected SSRF protection to block localhost request")
	}
}
