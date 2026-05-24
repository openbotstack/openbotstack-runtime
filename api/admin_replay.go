package api

import (
	"log/slog"
	"net/http"

	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
)

// handleAuditReplay reconstructs an execution trace from audit events.
// GET /v1/admin/audit/replay?execution_id=xxx
func (ar *AdminRouter) handleAuditReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.auditQuerier == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "audit not configured")
		return
	}

	executionID := r.URL.Query().Get("execution_id")
	if executionID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "execution_id query parameter is required")
		return
	}

	builder := rtAudit.NewReplayBuilder(ar.auditQuerier)
	replay, err := builder.Build(r.Context(), executionID)
	if err != nil {
		switch err {
		case rtAudit.ErrExecutionNotFound:
			writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
		default:
			slog.ErrorContext(r.Context(), "audit replay build failed",
				"execution_id", executionID, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to build replay")
		}
		return
	}

	writeJSON(w, http.StatusOK, replay)
}
