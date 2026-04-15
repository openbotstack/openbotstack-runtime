package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestWriteMiddlewareError_JSONFormat(t *testing.T) {
	rr := httptest.NewRecorder()
	writeMiddlewareError(rr, http.StatusUnauthorized, ErrUnauthorized, "missing key")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp errorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "UNAUTHORIZED" {
		t.Errorf("code = %q, want %q", resp.Error.Code, "UNAUTHORIZED")
	}
	if resp.Error.Message != "missing key" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "missing key")
	}
}

func TestMiddlewareErrorCodes(t *testing.T) {
	codes := []string{ErrUnauthorized, ErrForbidden, ErrRateLimited, ErrInternal}
	for _, code := range codes {
		for _, c := range code {
			if (c < 'A' || c > 'Z') && c != '_' && (c < '0' || c > '9') {
				t.Errorf("error code %q has non-UPPER_SNAKE_CASE char: %c", code, c)
			}
		}
	}
}

func TestWriteMiddlewareError_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		code       string
		message    string
	}{
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized, "no auth"},
		{"forbidden", http.StatusForbidden, ErrForbidden, "no access"},
		{"rate limited", http.StatusTooManyRequests, ErrRateLimited, "slow down"},
		{"internal", http.StatusInternalServerError, ErrInternal, "oops"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeMiddlewareError(rr, tt.statusCode, tt.code, tt.message)

			if rr.Code != tt.statusCode {
				t.Errorf("status = %d, want %d", rr.Code, tt.statusCode)
			}

			var resp errorResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", resp.Error.Code, tt.code)
			}
		})
	}
}
