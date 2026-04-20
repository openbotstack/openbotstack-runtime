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
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

// ProviderInfo describes a registered model provider.
type ProviderInfo struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
}

// ProviderLister lists registered model providers.
type ProviderLister interface {
	ListProviders() []ProviderInfo
}

// ProviderReloader allows the admin API to hot-reload providers at runtime.
type ProviderReloader interface {
	ReloadProvider(providerName, baseURL, apiKey, model string) error
}

// ProviderConfigEntry describes a provider's configuration for the admin API.
// API keys are never exposed — only whether one is set.
type ProviderConfigEntry struct {
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	APIKeySet bool   `json:"api_key_set"`
	Model     string `json:"model"`
	IsDefault bool   `json:"is_default"`
}

// SkillAdmin provides skill management operations for the admin API.
type SkillAdmin interface {
	ListSkills() ([]SkillAdminInfo, error)
	SetSkillEnabled(skillID string, enabled bool) error
}

// SkillAdminInfo describes a skill for admin management.
type SkillAdminInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
}

// AdminRouter handles admin CRUD endpoints.
type AdminRouter struct {
	mux              *http.ServeMux
	db               *sql.DB
	providerLister   ProviderLister
	providerReloader ProviderReloader
	skillAdmin       SkillAdmin
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

// SetProviderLister sets the provider lister for the /v1/admin/providers endpoint.
func (ar *AdminRouter) SetProviderLister(pl ProviderLister) {
	ar.providerLister = pl
}

// SetProviderReloader sets the provider reloader for the /v1/admin/providers/config endpoint.
func (ar *AdminRouter) SetProviderReloader(pr ProviderReloader) {
	ar.providerReloader = pr
}

// SetSkillAdmin sets the skill admin for the /v1/admin/skills endpoint.
func (ar *AdminRouter) SetSkillAdmin(sa SkillAdmin) {
	ar.skillAdmin = sa
}

// Handler returns the admin http.Handler wrapped with RequireAdmin.
func (ar *AdminRouter) Handler() http.Handler {
	return middleware.RequireAdmin(ar.mux)
}

func (ar *AdminRouter) registerRoutes() {
	ar.mux.HandleFunc("/v1/admin/tenants", ar.handleTenants)
	ar.mux.HandleFunc("/v1/admin/tenants/{tenantID}", ar.handleTenant)
	ar.mux.HandleFunc("/v1/admin/tenants/{tenantID}/users", ar.handleTenantUsers)
	ar.mux.HandleFunc("/v1/admin/users/{userID}", ar.handleUser)
	ar.mux.HandleFunc("/v1/admin/users/{userID}/keys", ar.handleUserKeys)
	ar.mux.HandleFunc("/v1/admin/keys/{keyID}", ar.handleRevokeKey)
	ar.mux.HandleFunc("/v1/admin/providers", ar.handleProviders)
	ar.mux.HandleFunc("/v1/admin/providers/config", ar.handleProviderConfig)
	ar.mux.HandleFunc("/v1/admin/providers/test", ar.handleProviderTest)
	ar.mux.HandleFunc("/v1/admin/skills", ar.handleAdminSkills)
	ar.mux.HandleFunc("/v1/admin/skills/", ar.handleSkillAction)
	ar.mux.HandleFunc("/v1/admin/sessions", ar.handleAdminSessions)
	ar.mux.HandleFunc("/v1/admin/sessions/", ar.handleAdminSessionAction)
	ar.mux.HandleFunc("/v1/admin/audit", ar.handleAudit)
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
	// Delete associated API keys first (avoid FK constraint violation)
	_, _ = ar.db.Exec(`DELETE FROM api_keys WHERE user_id = ?`, userID)

	result, err := ar.db.Exec(`DELETE FROM users WHERE id = ?`, userID)
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

	w.WriteHeader(http.StatusNoContent)
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
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleProviders returns the list of registered model providers.
func (ar *AdminRouter) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.providerLister == nil {
		writeJSON(w, http.StatusOK, []ProviderInfo{})
		return
	}

	providers := ar.providerLister.ListProviders()
	if providers == nil {
		providers = []ProviderInfo{}
	}
	writeJSON(w, http.StatusOK, providers)
}

// validProviders lists provider names accepted by the admin API.
var validProviders = map[string]bool{
	"openai":      true,
	"modelscope":  true,
	"siliconflow": true,
	"claude":      true,
}

// handleProviderConfig handles GET (read) and PUT (update) for provider configuration.
func (ar *AdminRouter) handleProviderConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ar.getProviderConfig(w, r)
	case http.MethodPut:
		ar.updateProviderConfig(w, r)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) getProviderConfig(w http.ResponseWriter, r *http.Request) {
	// Read all provider configs from SQLite
	rows, err := ar.db.Query("SELECT provider_name, base_url, api_key, model, is_default FROM provider_config")
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to read provider config")
		return
	}
	defer func() { _ = rows.Close() }()

	providers := make(map[string]interface{})
	defaultProvider := ""

	for rows.Next() {
		var name, baseURL, apiKey, model string
		var isDefault int
		if err := rows.Scan(&name, &baseURL, &apiKey, &model, &isDefault); err != nil {
			continue
		}
		providers[name] = ProviderConfigEntry{
			Name:      name,
			BaseURL:   baseURL,
			APIKeySet: apiKey != "",
			Model:     model,
			IsDefault: isDefault == 1,
		}
		if isDefault == 1 {
			defaultProvider = name
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"default":   defaultProvider,
		"providers": providers,
	})
}

func (ar *AdminRouter) updateProviderConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider   string `json:"provider"`
		BaseURL    string `json:"base_url"`
		APIKey     string `json:"api_key"`
		Model      string `json:"model"`
		IsDefault  string `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	// Validation
	if req.Provider == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "provider is required")
		return
	}
	if !validProviders[req.Provider] {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "unsupported provider (use: openai, modelscope, siliconflow, claude)")
		return
	}
	if req.Model == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "model is required")
		return
	}

	// Set default base URL if not provided
	if req.BaseURL == "" {
		switch req.Provider {
		case "openai":
			req.BaseURL = "https://api.openai.com/v1"
		case "modelscope":
			req.BaseURL = "https://api-inference.modelscope.cn/v1"
		case "siliconflow":
			req.BaseURL = "https://api.siliconflow.cn/v1"
		case "claude":
			req.BaseURL = "https://api.anthropic.com/v1"
		}
	}

	// Parse is_default
	isDefault := req.IsDefault == "true"

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Use transaction to atomically clear defaults + upsert
	tx, err := ar.db.Begin()
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	// If setting as default, clear other defaults first
	if isDefault {
		if _, err := tx.Exec("UPDATE provider_config SET is_default = 0 WHERE is_default = 1"); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update provider config")
			return
		}
	}

	// Upsert provider config
	// If api_key is empty, preserve the existing one
	var existingKey string
	_ = tx.QueryRow("SELECT api_key FROM provider_config WHERE provider_name = ?", req.Provider).Scan(&existingKey)

	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = existingKey
	}

	isDefaultInt := 0
	if isDefault {
		isDefaultInt = 1
	}

	_, err = tx.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_name) DO UPDATE SET base_url = ?, api_key = ?, model = ?, is_default = ?, updated_at = ?`,
		req.Provider, req.BaseURL, apiKey, req.Model, isDefaultInt, now,
		req.BaseURL, apiKey, req.Model, isDefaultInt, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to save provider config")
		return
	}

	if err := tx.Commit(); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to commit transaction")
		return
	}

	// Hot-reload provider if reloader is available and we have an API key
	if ar.providerReloader != nil && apiKey != "" {
		if err := ar.providerReloader.ReloadProvider(req.Provider, req.BaseURL, apiKey, req.Model); err != nil {
			slog.ErrorContext(r.Context(), "provider hot-reload failed",
				"provider", req.Provider, "error", err)
			// Still return success for the config save, but log the reload failure
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": req.Provider,
		"saved":    true,
	})
}

// handleProviderTest tests connectivity to a provider endpoint.
func (ar *AdminRouter) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	if req.Provider == "" || req.APIKey == "" || req.Model == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "provider, api_key, and model are required")
		return
	}

	// Use stored base_url if not provided
	if req.BaseURL == "" {
		_ = ar.db.QueryRow("SELECT base_url FROM provider_config WHERE provider_name = ?", req.Provider).Scan(&req.BaseURL)
		if req.BaseURL == "" {
			switch req.Provider {
			case "openai":
				req.BaseURL = "https://api.openai.com/v1"
			case "modelscope":
				req.BaseURL = "https://api-inference.modelscope.cn/v1"
			case "siliconflow":
				req.BaseURL = "https://api.siliconflow.cn/v1"
			case "claude":
				req.BaseURL = "https://api.anthropic.com/v1"
			default:
				writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "unknown provider")
				return
			}
		}
	}

	// Test by making a minimal models list request
	endpoint := strings.TrimRight(req.BaseURL, "/") + "/models"
	start := time.Now()

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": fmt.Sprintf("failed to create request: %v", err),
		})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"message":     fmt.Sprintf("connection failed: %v", err),
			"latency_ms":  latency.Milliseconds(),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"message":     "connection successful",
			"latency_ms":  latency.Milliseconds(),
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":     false,
			"message":     fmt.Sprintf("server returned status %d", resp.StatusCode),
			"latency_ms":  latency.Milliseconds(),
		})
	}
}

// handleAdminSkills returns all loaded skills with their enabled status.
func (ar *AdminRouter) handleAdminSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.skillAdmin == nil {
		writeJSON(w, http.StatusOK, []SkillAdminInfo{})
		return
	}

	skills, err := ar.skillAdmin.ListSkills()
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list skills")
		return
	}
	if skills == nil {
		skills = []SkillAdminInfo{}
	}
	writeJSON(w, http.StatusOK, skills)
}

// handleSkillAction handles enable/disable actions for /v1/admin/skills/{skillID}/{action}
// Path format: /v1/admin/skills/{skillID}/enable or /v1/admin/skills/{skillID}/disable
// skillID may contain slashes (e.g., "core/summarize").
func (ar *AdminRouter) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	// Parse: /v1/admin/skills/{skillID}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/skills/")
	// Split into parts — the last part is the action, everything before is the skillID
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash < 0 {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid path format")
		return
	}
	skillID := path[:lastSlash]
	action := path[lastSlash+1:]

	if ar.skillAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "skill admin not configured")
		return
	}

	var enabled bool
	switch action {
	case "enable":
		enabled = true
	case "disable":
		enabled = false
	default:
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "action must be 'enable' or 'disable'")
		return
	}

	if err := ar.skillAdmin.SetSkillEnabled(skillID, enabled); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update skill")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": skillID, "enabled": enabled})
}

// handleAdminSessions returns all sessions across all tenants (admin view).
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

	result, err := ar.db.Exec(`DELETE FROM session_entries WHERE session_id = ?`, sessionID)
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
