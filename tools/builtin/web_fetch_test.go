package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebFetchTool_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	tool := &WebFetchTool{Timeout: 5 * time.Second, MaxBytes: 1024 * 1024, allowPrivateIPs: true}
	result, err := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["status_code"] != int64(200) {
		t.Errorf("status_code = %v, want 200", result["status_code"])
	}
}

func TestWebFetchTool_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	tool := &WebFetchTool{Timeout: 50 * time.Millisecond, allowPrivateIPs: true}
	_, err := tool.Execute(ctx, map[string]any{"url": server.URL})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWebFetchTool_MaxResponseSize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer server.Close()
	tool := &WebFetchTool{MaxBytes: 1024, Timeout: 5 * time.Second, allowPrivateIPs: true}
	_, err := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if err == nil {
		t.Fatal("expected error for response too large")
	}
}

func TestWebFetchTool_InvalidURL(t *testing.T) {
	tool := &WebFetchTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "ftp://bad"})
	if err == nil {
		t.Fatal("expected error for non-http URL")
	}
}

func TestWebFetchTool_SSRFProtection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("should not reach"))
	}))
	defer server.Close()
	// allowPrivateIPs defaults to false — SSRF protection active.
	tool := &WebFetchTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{"url": server.URL})
	if err == nil {
		t.Fatal("expected SSRF error for localhost address")
	}
	if !strings.Contains(err.Error(), "private network addresses is blocked") {
		t.Errorf("error = %q, want SSRF block message", err.Error())
	}
}

func TestWebFetchTool_SSRFProtection_ExplicitPrivateIP(t *testing.T) {
	tool := &WebFetchTool{Timeout: 5 * time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://127.0.0.1:9999/"})
	if err == nil {
		t.Fatal("expected SSRF error for 127.0.0.1")
	}
	if !strings.Contains(err.Error(), "private network addresses is blocked") {
		t.Errorf("error = %q, want SSRF block message", err.Error())
	}
}

func TestWebFetchTool_MethodWhitelist(t *testing.T) {
	tool := &WebFetchTool{Timeout: 5 * time.Second, allowPrivateIPs: true}
	for _, method := range []string{"DELETE", "PUT", "PATCH", "OPTIONS", "TRACE"} {
		_, err := tool.Execute(context.Background(), map[string]any{"url": "http://example.com", "method": method})
		if err == nil {
			t.Errorf("method %q should be rejected", method)
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("method %q error = %q, want 'not allowed'", method, err.Error())
		}
	}
}

func TestWebFetchTool_MethodWhitelist_Allowed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer server.Close()
	tool := &WebFetchTool{Timeout: 5 * time.Second, allowPrivateIPs: true}
	for _, method := range []string{"GET", "POST", "HEAD"} {
		result, err := tool.Execute(context.Background(), map[string]any{"url": server.URL, "method": method})
		if err != nil {
			t.Errorf("method %q should be allowed, got error: %v", method, err)
		}
		if result["status_code"] != int64(200) {
			t.Errorf("method %q status_code = %v, want 200", method, result["status_code"])
		}
	}
}
