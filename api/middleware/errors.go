package middleware

import (
	"encoding/json"
	"net/http"
)

// Error code constants for middleware responses (UPPER_SNAKE_CASE).
// These mirror the codes in the api package but live here to avoid import cycles.
const (
	ErrUnauthorized = "UNAUTHORIZED"
	ErrForbidden    = "FORBIDDEN"
	ErrRateLimited  = "RATE_LIMITED"
	ErrInternal     = "INTERNAL_ERROR"
)

type middlewareErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeMiddlewareError writes a structured JSON error response.
func writeMiddlewareError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := middlewareErrorResponse{}
	resp.Error.Code = code
	resp.Error.Message = message
	_ = json.NewEncoder(w).Encode(resp)
}
