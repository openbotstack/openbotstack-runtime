package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// ReasoningResponse is the API response for the reasoning endpoint.
type ReasoningResponse struct {
	ExecutionID string               `json:"execution_id"`
	Tree        *reasoning.ReasoningEvent `json:"tree"`
	Text        string               `json:"text"`
	Debug       *ReasoningDebug      `json:"debug,omitempty"`
}

// ReasoningDebug contains extra information when ?debug=true is requested.
type ReasoningDebug struct {
	AuditTrail []AuditEntryJSON `json:"audit_trail"`
}

// AuditEntryJSON is the JSON representation of an audit entry for the debug response.
type AuditEntryJSON struct {
	TraceID   string `json:"trace_id"`
	StepID    string `json:"step_id"`
	StepName  string `json:"step_name"`
	StepType  string `json:"step_type"`
	Timestamp string `json:"timestamp"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	DurationMs int   `json:"duration_ms"`
}

// handleExecutionAction dispatches /v1/execution/{id}/* sub-paths.
func (r *Router) handleExecutionAction(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	switch {
	case strings.HasSuffix(path, "/reasoning"):
		r.handleExecutionReasoning(w, req)
	case strings.HasSuffix(path, "/lineage"):
		handleLineage(r.lineageBuilder, w, req)
	default:
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "not found")
	}
}

// handleExecutionReasoning handles GET /v1/execution/{id}/reasoning
func (r *Router) handleExecutionReasoning(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method, "path", req.URL.Path,
			"status", http.StatusMethodNotAllowed, "error", "method not allowed")
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	// Extract execution ID: /v1/execution/{id}/reasoning
	path := req.URL.Path
	executionID := extractExecutionID(path)
	if executionID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "missing execution ID")
		return
	}

	if r.reasoningStore == nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "reasoning not available")
		return
	}

	trail, err := r.reasoningStore.GetAuditTrail(req.Context(), executionID)
	if err != nil {
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method, "path", req.URL.Path,
			"status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to retrieve reasoning")
		return
	}

	if trail == nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
		return
	}

	// Verify tenant ownership: check if the trail belongs to the requesting user's tenant.
	if user, ok := middleware.UserFromContext(req.Context()); ok && user.TenantID != "" {
		if len(trail) > 0 && trail[0].TenantID != "" && trail[0].TenantID != user.TenantID {
			slog.WarnContext(req.Context(), "reasoning access denied: tenant mismatch",
				"method", req.Method, "path", req.URL.Path,
				"user_tenant", user.TenantID, "trail_tenant", trail[0].TenantID)
			writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
			return
		}
	}

	tree := reasoning.BuildReasoningTree(trail)
	text := reasoning.RenderReasoningText(tree)

	resp := ReasoningResponse{
		ExecutionID: executionID,
		Tree:        tree,
		Text:        text,
	}

	// PART 5: Debug mode
	if req.URL.Query().Get("debug") == "true" {
		debugEntries := make([]AuditEntryJSON, len(trail))
		for i, e := range trail {
			debugEntries[i] = AuditEntryJSON{
				TraceID:    e.TraceID,
				StepID:     e.StepID,
				StepName:   e.StepName,
				StepType:   e.StepType,
				Timestamp:  e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
				Status:     e.Status,
				Error:      e.Error,
				DurationMs: int(e.Duration.Milliseconds()),
			}
		}
		resp.Debug = &ReasoningDebug{
			AuditTrail: debugEntries,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// extractExecutionID parses the execution ID from /v1/execution/{id}/reasoning
func extractExecutionID(path string) string {
	// Expected: /v1/execution/{id}/reasoning
	prefix := "/v1/execution/"
	if len(path) <= len(prefix) {
		return ""
	}
	trimmed := path[len(prefix):]
	// Find the next /
	if idx := indexOfSlash(trimmed); idx > 0 {
		return trimmed[:idx]
	}
	return ""
}

func indexOfSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
