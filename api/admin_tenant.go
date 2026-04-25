package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// handleTenants handles POST (create) and GET (list) for /v1/admin/tenants
func (ar *AdminRouter) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		ar.createTenant(w, r)
	case http.MethodGet:
		ar.listTenants(w, r)
	default:
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusMethodNotAllowed,
			"error", "method not allowed",
		)
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) createTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusBadRequest,
			"error", "invalid request",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}
	if req.ID == "" || req.Name == "" {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusBadRequest,
			"error", "id and name required",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "id and name required")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := ar.db.Exec(`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
		req.ID, req.Name, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeAPIError(w, http.StatusConflict, "CONFLICT", "tenant already exists")
			return
		}
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to create tenant")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id": req.ID, "name": req.Name, "created_at": now,
	})
}

func (ar *AdminRouter) listTenants(w http.ResponseWriter, r *http.Request) {
	rows, err := ar.db.Query("SELECT id, name, created_at FROM tenants ORDER BY created_at")
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list tenants")
		return
	}
	defer func() { _ = rows.Close() }()

	var result []map[string]string
	for rows.Next() {
		var id, name, createdAt string
		if err := rows.Scan(&id, &name, &createdAt); err != nil {
			continue
		}
		result = append(result, map[string]string{
			"id": id, "name": name, "created_at": createdAt,
		})
	}
	if result == nil {
		result = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleTenant handles PUT (update) and DELETE for /v1/admin/tenants/{tenantID}
func (ar *AdminRouter) handleTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenantID")

	switch r.Method {
	case http.MethodPut:
		ar.updateTenant(w, r, tenantID)
	case http.MethodDelete:
		ar.deleteTenant(w, r, tenantID)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) updateTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}
	if req.Name == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "name required")
		return
	}

	result, err := ar.db.Exec(`UPDATE tenants SET name = ? WHERE id = ?`, req.Name, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update tenant")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "tenant not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"id": tenantID, "name": req.Name})
}

func (ar *AdminRouter) deleteTenant(w http.ResponseWriter, r *http.Request, tenantID string) {
	// Cascade delete: api_keys → users → session_entries → tenants
	tx, err := ar.db.Begin()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	// Delete API keys for users in this tenant
	if _, err := tx.Exec(`DELETE FROM api_keys WHERE user_id IN (SELECT id FROM users WHERE tenant_id = ?)`, tenantID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete tenant API keys")
		return
	}
	// Delete users in this tenant
	if _, err := tx.Exec(`DELETE FROM users WHERE tenant_id = ?`, tenantID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete tenant users")
		return
	}
	// Delete session entries for this tenant
	if _, err := tx.Exec(`DELETE FROM session_entries WHERE tenant_id = ?`, tenantID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete tenant sessions")
		return
	}
	// Delete the tenant itself
	result, err := tx.Exec(`DELETE FROM tenants WHERE id = ?`, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete tenant")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "tenant not found")
		return
	}

	if err := tx.Commit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to commit transaction")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTenantUsers handles /v1/admin/tenants/{tenantID}/users
