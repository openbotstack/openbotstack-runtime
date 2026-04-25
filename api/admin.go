package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

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

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
