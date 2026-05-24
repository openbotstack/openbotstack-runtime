package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPISpec_YAMLInput(t *testing.T) {
	yaml := []byte("openapi: '3.0.3'\ninfo:\n  title: Test\n  version: '1.0'\npaths: {}\n")
	handler := NewOpenAPISpec(yaml)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("body should not be empty")
	}
	// Should contain JSON-like content, not raw YAML
	if body[0] != '{' {
		t.Errorf("body should start with '{', got %q", body[0])
	}
}

func TestOpenAPISpec_JSONInput(t *testing.T) {
	json := []byte(`{"openapi":"3.0.3","info":{"title":"Test","version":"1.0"},"paths":{}}`)
	handler := NewOpenAPISpec(json)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestOpenAPISpec_MethodRejected(t *testing.T) {
	handler := NewOpenAPISpec([]byte("{}"))
	req := httptest.NewRequest(http.MethodPost, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestOpenAPISpec_InvalidInput(t *testing.T) {
	handler := NewOpenAPISpec([]byte("not: valid: yaml: [[{"))
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOpenAPISpec_CacheControl(t *testing.T) {
	handler := NewOpenAPISpec([]byte(`{}`))
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", cc, "public, max-age=300")
	}
}
