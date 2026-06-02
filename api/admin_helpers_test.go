package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireMethod_Match(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/admin/test", nil)

	if !requireMethod(rec, req, http.MethodGet) {
		t.Fatal("requireMethod returned false for matching method")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, want 200 (no response written yet)", rec.Code)
	}
}

func TestRequireMethod_Mismatch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/test", nil)

	if requireMethod(rec, req, http.MethodGet) {
		t.Fatal("requireMethod returned true for mismatched method")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestRequireMethod_MismatchWritesJSONError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/admin/test", nil)

	requireMethod(rec, req, http.MethodGet)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := rec.Body.String()
	if !contains(body, "METHOD_NOT_ALLOWED") {
		t.Errorf("body = %q, want substring METHOD_NOT_ALLOWED", body)
	}
}

func TestRequireAnyMethod_SingleMatch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/test", nil)

	if !requireAnyMethod(rec, req, http.MethodGet, http.MethodPost) {
		t.Fatal("requireAnyMethod returned false for matching method")
	}
}

func TestRequireAnyMethod_NoMatch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/v1/admin/test", nil)

	if requireAnyMethod(rec, req, http.MethodGet, http.MethodPost) {
		t.Fatal("requireAnyMethod returned true for non-matching method")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
