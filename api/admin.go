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

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

// AdminRouter handles admin CRUD endpoints.
type AdminRouter struct {
	mux *http.ServeMux
	db  *sql.DB
}

// NewAdminRouter creates an admin router backed by db.
func NewAdminRouter(db *sql.DB) *AdminRouter {
	ar := &AdminRouter{
		mux: http.NewServeMux(),
		db:  db,
	}
	ar.registerRoutes()
	return ar
}

// Handler returns the admin http.Handler wrapped with RequireAdmin.
func (ar *AdminRouter) Handler() http.Handler {
	return middleware.RequireAdmin(ar.mux)
}

func (ar *AdminRouter) registerRoutes() {
	ar.mux.HandleFunc("/v1/admin/tenants", ar.handleTenants)
	ar.mux.HandleFunc("/v1/admin/tenants/{tenantID}/users", ar.handleTenantUsers)
	ar.mux.HandleFunc("/v1/admin/users/{userID}/keys", ar.handleUserKeys)
	ar.mux.HandleFunc("/v1/admin/keys/{keyID}", ar.handleRevokeKey)
}

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
	defer rows.Close()

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

// handleTenantUsers handles /v1/admin/tenants/{tenantID}/users
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
	rows, err := ar.db.Query(`SELECT id, tenant_id, name, role, created_at FROM users WHERE tenant_id = ?`, tenantID)
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
	defer rows.Close()

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

// handleUserKeys handles /v1/admin/users/{userID}/keys
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
	defer rows.Close()

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

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": keyID, "revoked": true})
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
