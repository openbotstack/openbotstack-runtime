package api

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

func (ar *AdminRouter) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	query := `SELECT id, tenant_id, user_id, action, resource, outcome, duration_ms, timestamp
		FROM audit_logs WHERE 1=1`
	args := []interface{}{}

	if q := r.URL.Query().Get("tenant_id"); q != "" {
		query += " AND tenant_id = ?"
		args = append(args, q)
	}
	if q := r.URL.Query().Get("action"); q != "" {
		query += " AND action = ?"
		args = append(args, q)
	}
	if q := r.URL.Query().Get("user_id"); q != "" {
		query += " AND user_id = ?"
		args = append(args, q)
	}

	query += " ORDER BY timestamp DESC"

	limit := 100
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := fmt.Sscanf(q, "%d", &limit); err != nil || n != 1 || limit > 1000 {
			limit = 100
		}
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := ar.db.Query(query, args...)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query audit logs")
		return
	}
	defer func() { _ = rows.Close() }()

	type AuditEntry struct {
		ID         string `json:"id"`
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		Action     string `json:"action"`
		Resource   string `json:"resource"`
		Outcome    string `json:"outcome"`
		DurationMs int    `json:"duration_ms"`
		Timestamp  string `json:"timestamp"`
	}

	var result []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.Action, &e.Resource, &e.Outcome, &e.DurationMs, &e.Timestamp); err != nil {
			continue
		}
		result = append(result, e)
	}
	if result == nil {
		result = []AuditEntry{}
	}
	writeJSON(w, http.StatusOK, result)
}

// HandleMe returns the authenticated user's identity and role.
// This is a user-level endpoint — any authenticated user can call it.
// It must NOT be behind RequireAdmin middleware.
func HandleMe(w http.ResponseWriter, r *http.Request) {
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
