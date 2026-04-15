package api

import (
	"encoding/json"
	"net/http"
)

// Error code constants (UPPER_SNAKE_CASE).
const (
	ErrMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	ErrInvalidRequest     = "INVALID_REQUEST"
	ErrUnauthorized       = "UNAUTHORIZED"
	ErrForbidden          = "FORBIDDEN"
	ErrNotFound           = "NOT_FOUND"
	ErrRateLimited        = "RATE_LIMITED"
	ErrInternal           = "INTERNAL_ERROR"
	ErrUnavailable        = "SERVICE_UNAVAILABLE"
	ErrAgentNotConfigured = "AGENT_NOT_CONFIGURED"
)

// APIErrorResponse is the standard error envelope.
type APIErrorResponse struct {
	Error APIErrorDetail `json:"error"`
}

// APIErrorDetail contains the error code and human-readable message.
type APIErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeAPIError writes a structured JSON error response.
func writeAPIError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIErrorResponse{
		Error: APIErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}
