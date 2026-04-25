package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

func (ar *AdminRouter) handleUserKeys(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userID")

	switch r.Method {
	case http.MethodPost:
		ar.createAPIKey(w, r, userID)
	case http.MethodGet:
		ar.listAPIKeys(w, r, userID)
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

func (ar *AdminRouter) createAPIKey(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
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
	if req.Name == "" {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusBadRequest,
			"error", "name required",
		)
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "name required")
		return
	}

	// Look up user's tenant_id
	var tenantID string
	err := ar.db.QueryRow("SELECT tenant_id FROM users WHERE id = ?", userID).Scan(&tenantID)
	if err == sql.ErrNoRows {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusNotFound,
			"error", "user not found",
		)
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "user not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "internal error")
		return
	}

	// Generate key: obs_ + 32 hex chars = 36 total
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to generate key")
		return
	}
	fullKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])
	prefix := fullKey[:12]
	now := time.Now().UTC().Format(time.RFC3339Nano)

	keyID := fmt.Sprintf("key-%s", hex.EncodeToString(keyBytes[:8]))
	_, err = ar.db.Exec(`INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		keyID, tenantID, userID, prefix, hashHex, req.Name, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to create API key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id": keyID, "key": fullKey, "key_prefix": prefix,
		"name": req.Name, "created_at": now,
	})
}

func (ar *AdminRouter) listAPIKeys(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := ar.db.Query(`SELECT id, key_prefix, name, created_at, revoked FROM api_keys WHERE user_id = ?`, userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list keys")
		return
	}
	defer func() { _ = rows.Close() }()

	var result []map[string]interface{}
	for rows.Next() {
		var id, prefix, name, createdAt string
		var revoked int
		if err := rows.Scan(&id, &prefix, &name, &createdAt, &revoked); err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"id": id, "key_prefix": prefix, "name": name,
			"created_at": createdAt, "revoked": revoked == 1,
		})
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, result)
}

func (ar *AdminRouter) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusMethodNotAllowed,
			"error", "method not allowed",
		)
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	keyID := r.PathValue("keyID")

	result, err := ar.db.Exec(`UPDATE api_keys SET revoked = 1 WHERE id = ?`, keyID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to revoke key")
		return
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		slog.WarnContext(r.Context(), "request validation error",
			"method", r.Method,
			"path", r.URL.Path,
			"status", http.StatusNotFound,
			"error", "key not found",
		)
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "key not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeJSON writes a JSON response.
