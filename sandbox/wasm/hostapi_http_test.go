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

// ==================== SSRF Protection Tests ====================

func TestSSRFProtection_BlockedURLs(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		blocked bool
	}{
		{"loopback IPv4", "http://127.0.0.1:8080/", true},
		{"loopback localhost", "http://localhost:8080/", true},
		{"private 10.x", "http://10.0.0.1:8080/", true},
		{"private 172.16.x", "http://172.16.0.1:8080/", true},
		{"private 192.168.x", "http://192.168.1.1:8080/", true},
		{"link-local", "http://169.254.0.1:8080/", true},
		{"metadata endpoint", "http://169.254.169.254/latest/meta-data/", true},
		{"unspecified 0.0.0.0", "http://0.0.0.0:8080/", true},
		{"loopback IPv6", "http://[::1]:8080/", true},
		{"public IP 8.8.8.8", "http://8.8.8.8:80/", false},
		{"public IP 1.1.1.1", "http://1.1.1.1:80/", false},
	}

	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := client.Fetch(context.Background(), tt.url, "GET", nil)
			if tt.blocked {
				if err == nil {
					t.Errorf("expected SSRF to block %s", tt.url)
				}
			} else {
				// Public IPs may fail for network reasons, but should NOT be blocked by SSRF
				if err != nil && err == wasm.ErrURLNotAllowed {
					t.Errorf("public IP %s should not be SSRF-blocked", tt.url)
				}
			}
		})
	}
}

func TestSandboxedHTTPClientWithSSRF_BlocksLocalhost(t *testing.T) {
	// Start a server on localhost
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not reach"))
	}))
	defer server.Close()

	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), server.URL, "GET", nil)
	if err == nil {
		t.Error("expected SSRF protection to block localhost")
	}
	if err != wasm.ErrURLNotAllowed {
		t.Errorf("expected ErrURLNotAllowed, got %v", err)
	}
}

func TestSandboxedHTTPClientWithSSRF_BlocksPrivateIP(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), "http://127.0.0.1:8080/test", "GET", nil)
	if err == nil {
		t.Error("expected SSRF protection to block 127.0.0.1")
	}
}

func TestSandboxedHTTPClientWithSSRF_BlocksMetadataEndpoint(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), "http://169.254.169.254/latest/meta-data/", "GET", nil)
	if err == nil {
		t.Error("expected SSRF protection to block cloud metadata endpoint")
	}
}

func TestSandboxedHTTPClientWithSSRF_BlocksUnspecifiedIP(t *testing.T) {
	allowlist := wasm.NewHTTPAllowlist([]string{"*"})
	client := wasm.NewSandboxedHTTPClientWithSSRF(allowlist, nil)

	_, _, err := client.Fetch(context.Background(), "http://0.0.0.0:8080/test", "GET", nil)
	if err == nil {
		t.Error("expected SSRF protection to block 0.0.0.0")
	}
}
