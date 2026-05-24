package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

func (ar *AdminRouter) handleAdminSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	query := `SELECT session_id, tenant_id, COUNT(*) as entry_count,
		MIN(created_at) as created_at, MAX(created_at) as updated_at
		FROM session_entries GROUP BY session_id, tenant_id ORDER BY updated_at DESC`

	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, _ := fmt.Sscanf(q, "%d", &limit); n != 1 || limit > 1000 {
			limit = 200
		}
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := ar.db.Query(query)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list sessions")
		return
	}
	defer func() { _ = rows.Close() }()

	type AdminSession struct {
		SessionID  string `json:"session_id"`
		TenantID   string `json:"tenant_id"`
		EntryCount int    `json:"entry_count"`
		CreatedAt  string `json:"created_at"`
		UpdatedAt  string `json:"updated_at"`
		LastEntry  string `json:"last_entry"`
	}

	var result []AdminSession
	for rows.Next() {
		var s AdminSession
		if err := rows.Scan(&s.SessionID, &s.TenantID, &s.EntryCount, &s.CreatedAt, &s.UpdatedAt); err != nil {
			continue
		}
		// Get last entry content
		_ = ar.db.QueryRow(
			"SELECT content FROM session_entries WHERE session_id = ? AND tenant_id = ? ORDER BY created_at DESC LIMIT 1",
			s.SessionID, s.TenantID,
		).Scan(&s.LastEntry)
		result = append(result, s)
	}
	if result == nil {
		result = []AdminSession{}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAdminSessionAction handles DELETE /v1/admin/sessions/{sessionID} (admin, no tenant filter)
func (ar *AdminRouter) handleAdminSessionAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/sessions/")
	sessionID := strings.TrimSuffix(path, "/")
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "missing session ID")
		return
	}

	// Optional tenant_id filter for safe cross-tenant admin operations.
	// When provided, only deletes entries matching both session_id and tenant_id.
	tenantID := r.URL.Query().Get("tenant_id")
	var result sql.Result
	var err error
	if tenantID != "" {
		result, err = ar.db.Exec(`DELETE FROM session_entries WHERE session_id = ? AND tenant_id = ?`, sessionID, tenantID)
	} else {
		slog.WarnContext(r.Context(), "admin session delete without tenant filter",
			"method", r.Method, "path", r.URL.Path, "session_id", sessionID)
		result, err = ar.db.Exec(`DELETE FROM session_entries WHERE session_id = ?`, sessionID)
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete session")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "session not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAudit returns audit log entries with optional filters.
