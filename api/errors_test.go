package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAPIError_JSONFormat(t *testing.T) {
	rr := httptest.NewRecorder()
	writeAPIError(rr, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var resp APIErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Code != "INVALID_REQUEST" {
		t.Errorf("error code = %q, want %q", resp.Error.Code, "INVALID_REQUEST")
	}
	if resp.Error.Message != "invalid request body" {
		t.Errorf("error message = %q, want %q", resp.Error.Message, "invalid request body")
	}
}

func TestWriteAPIError_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		code       string
		message    string
	}{
		{"bad request", http.StatusBadRequest, ErrInvalidRequest, "bad input"},
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized, "no auth"},
		{"forbidden", http.StatusForbidden, ErrForbidden, "no access"},
		{"not found", http.StatusNotFound, ErrNotFound, "missing"},
		{"method not allowed", http.StatusMethodNotAllowed, ErrMethodNotAllowed, "wrong method"},
		{"rate limited", http.StatusTooManyRequests, ErrRateLimited, "slow down"},
		{"internal error", http.StatusInternalServerError, ErrInternal, "oops"},
		{"service unavailable", http.StatusServiceUnavailable, ErrUnavailable, "down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeAPIError(rr, tt.statusCode, tt.code, tt.message)

			if rr.Code != tt.statusCode {
				t.Errorf("status = %d, want %d", rr.Code, tt.statusCode)
			}

			var resp APIErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			if resp.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", resp.Error.Code, tt.code)
			}
		})
	}
}

func TestErrorCodes_AreConsistent(t *testing.T) {
	codes := []string{
		ErrMethodNotAllowed, ErrInvalidRequest, ErrUnauthorized,
		ErrForbidden, ErrNotFound, ErrRateLimited,
		ErrInternal, ErrUnavailable, ErrAgentNotConfigured,
	}
	for _, code := range codes {
		for _, c := range code {
			if (c < 'A' || c > 'Z') && c != '_' && (c < '0' || c > '9') {
				t.Errorf("error code %q contains non-UPPER_SNAKE_CASE char: %c", code, c)
			}
		}
	}
}
