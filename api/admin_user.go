package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (ar *AdminRouter) handleTenantUsers(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenantID")

	switch r.Method {
	case http.MethodPost:
		ar.createUser(w, r, tenantID)
	case http.MethodGet:
		ar.listUsers(w, r, tenantID)
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

func (ar *AdminRouter) createUser(w http.ResponseWriter, r *http.Request, tenantID string) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
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
	if req.Role == "" {
		req.Role = "member"
	}
	if req.Role != "admin" && req.Role != "member" {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusBadRequest,
			"error", "role must be admin or member",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "role must be 'admin' or 'member'")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := ar.db.Exec(`INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		req.ID, tenantID, req.Name, req.Role, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to create user")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id": req.ID, "tenant_id": tenantID, "name": req.Name,
		"role": req.Role, "created_at": now,
	})
}

func (ar *AdminRouter) listUsers(w http.ResponseWriter, r *http.Request, tenantID string) {
	rows, err := ar.db.Query(`SELECT id, tenant_id, name, role, created_at FROM users WHERE tenant_id = ? ORDER BY created_at`, tenantID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list users")
		return
	}
	defer func() { _ = rows.Close() }()

	var result []map[string]string
	for rows.Next() {
		var id, tid, name, role, createdAt string
		if err := rows.Scan(&id, &tid, &name, &role, &createdAt); err != nil {
			continue
		}
		result = append(result, map[string]string{
			"id": id, "tenant_id": tid, "name": name,
			"role": role, "created_at": createdAt,
		})
	}
	if result == nil {
		result = []map[string]string{}
	}
	writeJSON(w, http.StatusOK, result)
}

// handleUser handles PUT (update) and DELETE for /v1/admin/users/{userID}
func (ar *AdminRouter) handleUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")

	switch r.Method {
	case http.MethodPut:
		ar.updateUser(w, r, userID)
	case http.MethodDelete:
		ar.deleteUser(w, r, userID)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) updateUser(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	// Build dynamic UPDATE — only set fields that are provided
	sets := []string{}
	args := []interface{}{}
	if req.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, req.Name)
	}
	if req.Role != "" {
		if req.Role != "admin" && req.Role != "member" {
			writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "role must be 'admin' or 'member'")
			return
		}
		sets = append(sets, "role = ?")
		args = append(args, req.Role)
	}
	if len(sets) == 0 {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "no fields to update")
		return
	}

	args = append(args, userID)
	query := "UPDATE users SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	result, err := ar.db.Exec(query, args...)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update user")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "user not found")
		return
	}

	// Return updated user
	var id, tenantID, name, role, createdAt string
	_ = ar.db.QueryRow(`SELECT id, tenant_id, name, role, created_at FROM users WHERE id = ?`, userID).
		Scan(&id, &tenantID, &name, &role, &createdAt)
	writeJSON(w, http.StatusOK, map[string]string{
		"id": id, "tenant_id": tenantID, "name": name, "role": role, "created_at": createdAt,
	})
}

func (ar *AdminRouter) deleteUser(w http.ResponseWriter, r *http.Request, userID string) {
	tx, err := ar.db.Begin()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to begin transaction")
		return
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM api_keys WHERE user_id = ?`, userID); err != nil {
		slog.ErrorContext(r.Context(), "failed to delete user API keys", "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete user API keys")
		return
	}

	result, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete user")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "user not found")
		return
	}

	if err := tx.Commit(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to commit transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleUserKeys handles /v1/admin/users/{userID}/keys
