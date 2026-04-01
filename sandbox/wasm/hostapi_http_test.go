package wasm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// ==================== HTTP Allowlist Tests ====================

func TestHTTPAllowlistEmpty(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist(nil)

	if allowlist.IsAllowed("https://example.com") {
		t.Error("Empty allowlist should deny all URLs")
	}
}

func TestHTTPAllowlistExactMatch(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{
		"https://api.example.com",
	})

	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://api.example.com", true},
		{"https://api.example.com/path", true},
		{"https://evil.com", false},
		{"https://api.example.com.evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := allowlist.IsAllowed(tt.url); got != tt.allowed {
				t.Errorf("IsAllowed(%s) = %v, want %v", tt.url, got, tt.allowed)
			}
		})
	}
}

func TestHTTPAllowlistWildcard(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{
		"*.example.com",
	})

	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://api.example.com", true},
		{"https://www.example.com", true},
		{"https://example.com", false}, // no subdomain
		{"https://evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := allowlist.IsAllowed(tt.url); got != tt.allowed {
				t.Errorf("IsAllowed(%s) = %v, want %v", tt.url, got, tt.allowed)
			}
		})
	}
}

// ==================== SandboxedHTTPClient Tests ====================

func TestSandboxedHTTPClientAllowed(t *testing.T) {
	// Start test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "ok"}`))
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{server.URL})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	body, statusCode, err := client.Fetch(context.Background(), server.URL, "GET", nil)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", statusCode)
	}

	if string(body) != `{"message": "ok"}` {
		t.Errorf("Unexpected body: %s", string(body))
	}
}

func TestSandboxedHTTPClientDenied(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{"https://allowed.com"})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), "https://evil.com", "GET", nil)
	if err == nil {
		t.Error("Expected error for denied URL")
	}

	if err != wasm.ErrURLNotAllowed {
		t.Errorf("Expected ErrURLNotAllowed, got %v", err)
	}
}

func TestSandboxedHTTPClientTimeout(t *testing.T) {
	// Start slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait for request context to be done (will timeout or be cancelled by Close)
		<-r.Context().Done()
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{server.URL})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()

	_, _, err := client.Fetch(ctx, server.URL, "GET", nil)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestSandboxedHTTPClientPOST(t *testing.T) {
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created": true}`))
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{server.URL})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	body, statusCode, err := client.Fetch(context.Background(), server.URL, "POST", []byte(`{"data": "test"}`))
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if statusCode != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", statusCode)
	}

	if string(receivedBody) != `{"data": "test"}` {
		t.Errorf("Server received wrong body: %s", string(receivedBody))
	}

	if string(body) != `{"created": true}` {
		t.Errorf("Unexpected response: %s", string(body))
	}
}

func TestSandboxedHTTPClientInvalidURL(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), "not-a-valid-url", "GET", nil)
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestSandboxedHTTPClientEmptyMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Default to GET
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{server.URL})
	client := wasm.NewSandboxedHTTPClient(allowlist, nil)

	_, statusCode, err := client.Fetch(context.Background(), server.URL, "", nil)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if statusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", statusCode)
	}
}
