package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// AuditQuerier queries audit events through the structured audit layer.
type AuditQuerier interface {
	Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error)
	Count(ctx context.Context, filter execution_logs.QueryFilter) (int, error)
}

func parseAuditFilter(r *http.Request, defaultLimit int) execution_logs.QueryFilter {
	filter := execution_logs.QueryFilter{
		TenantID: r.URL.Query().Get("tenant_id"),
		UserID:   r.URL.Query().Get("user_id"),
		Action:   r.URL.Query().Get("action"),
		Source:   audit.Source(r.URL.Query().Get("source")),
	}

	if q := r.URL.Query().Get("from"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			filter.From = t
		}
	}
	if q := r.URL.Query().Get("to"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			filter.To = t
		}
	}

	filter.Limit = defaultLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 1000 {
			filter.Limit = n
		}
	}

	return filter
}

func (ar *AdminRouter) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.auditQuerier == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "audit not configured")
		return
	}

	filter := parseAuditFilter(r, 100)

	if filter.TenantID == "" {
		slog.WarnContext(r.Context(), "admin audit query without tenant_id filter",
			"method", r.Method, "path", r.URL.Path)
	}

	events, err := ar.auditQuerier.Query(r.Context(), filter)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin audit query failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query audit logs")
		return
	}

	envelopes := make([]audit.AuditEnvelope, len(events))
	for i, e := range events {
		envelopes[i] = e.ToEnvelope()
	}
	if envelopes == nil {
		envelopes = []audit.AuditEnvelope{}
	}

	// Support ?format=<format_id> for industry-specific mapping (e.g. FHIR).
	if format := r.URL.Query().Get("format"); format != "" {
		if ar.eventMappers == nil {
			writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, fmt.Sprintf("unknown format: %q", format))
			return
		}
		mapper, ok := ar.eventMappers[format]
		if !ok {
			writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, fmt.Sprintf("unknown format: %q", format))
			return
		}
		mapped, err := mapper.MapBatch(envelopes)
		if err != nil {
			slog.ErrorContext(r.Context(), "audit format mapping failed", "format", format, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "format mapping failed")
			return
		}
		writeJSON(w, http.StatusOK, mapped)
		return
	}

	writeJSON(w, http.StatusOK, envelopes)
}

// handleAuditExport streams audit events as JSON Lines.
func (ar *AdminRouter) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.auditQuerier == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "audit not configured")
		return
	}

	filter := parseAuditFilter(r, 10000)

	events, err := ar.auditQuerier.Query(r.Context(), filter)
	if err != nil {
		slog.ErrorContext(r.Context(), "audit export query failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query audit logs")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", "attachment; filename=audit_export.ndjson")

	for _, e := range events {
		env := e.ToEnvelope()
		line, err := json.Marshal(env)
		if err != nil {
			slog.WarnContext(r.Context(), "audit export: skipping event with marshal error",
				"event_id", env.EventID, "error", err)
			continue
		}
		fmt.Fprintf(w, "%s\n", line)
	}
}

// HandleMe returns the authenticated user's identity and role.
// Only GET is allowed.
func HandleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	user, ok := middleware.UserFromContext(r.Context())
	if !ok || user == nil {
		writeAPIError(w, http.StatusUnauthorized, ErrUnauthorized, "not authenticated")
		return
	}
	role := middleware.RoleFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"user_id":   user.ID,
		"tenant_id": user.TenantID,
		"name":      user.Name,
		"role":      role,
	})
}

// handleAuditCompliance generates a compliance report from audit events.
// Registered at GET /v1/admin/audit/compliance.
func (ar *AdminRouter) handleAuditCompliance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.complianceGenerator == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "compliance report not configured")
		return
	}

	// Parse query parameters
	scope := audit.ComplianceScope{
		TenantID: r.URL.Query().Get("tenant_id"),
		UserID:   r.URL.Query().Get("user_id"),
	}

	var period audit.TimeRange
	if q := r.URL.Query().Get("from"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			period.From = t
		}
	}
	if q := r.URL.Query().Get("to"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			period.To = t
		}
	}

	// Default period: last 30 days if not specified
	if period.From.IsZero() {
		period.From = time.Now().AddDate(0, 0, -30)
	}
	if period.To.IsZero() {
		period.To = time.Now()
	}

	report, err := ar.complianceGenerator.Generate(r.Context(), rtAudit.ComplianceReportRequest{
		Scope:  scope,
		Period: period,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "compliance report generation failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to generate compliance report")
		return
	}

	writeJSON(w, http.StatusOK, report)
}
