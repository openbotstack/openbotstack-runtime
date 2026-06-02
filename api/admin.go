package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/openbotstack/openbotstack-core/audit"
	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	"github.com/openbotstack/openbotstack-runtime/mcp"
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
	ID        string `json:"id"`
	Provider  string `json:"provider"`
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
	ReloadSkills(ctx context.Context) error
	ReloadSkill(ctx context.Context, skillID string) error
}

// SkillAdminInfo describes a skill for admin management.
type SkillAdminInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Enabled     bool   `json:"enabled"`
}

// MCPAdmin provides MCP server management operations for the admin API.
type MCPAdmin interface {
	AddServer(ctx context.Context, cfg mcpcore.ServerConfig) error
	RemoveServer(ctx context.Context, serverID string) error
	UpdateServer(ctx context.Context, serverID string, cfg mcpcore.ServerConfig) error
	ListServers() []mcpcore.ServerStatus
	GetServerTools(ctx context.Context, serverID string) ([]mcpcore.ClientTool, error)
	ReconnectServer(ctx context.Context, serverID string) error
	HealthCheck(ctx context.Context) []mcp.ServerHealth
}

// CapabilityLister lists all registered capabilities.
type CapabilityLister interface {
	List() []CapabilityDescriptor
}


// CapabilityDescriptor mirrors core/capability.CapabilityDescriptor for JSON transport.
type CapabilityDescriptor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	SourceID    string `json:"source_id"`
}

// AdminRouterConfig holds all dependencies for constructing an AdminRouter.
type AdminRouterConfig struct {
	DB *sql.DB

	// Optional dependencies
	ProviderLister       ProviderLister
	ProviderReloader     ProviderReloader
	SkillAdmin           SkillAdmin
	MCPAdmin             MCPAdmin
	TelemetryHandler     *TelemetryHandler
	CapabilityLister     CapabilityLister
	AuditQuerier         AuditQuerier
	ModelRegistry        ModelRegistryAdmin
	RetentionManager     RetentionManager
	ComplianceGenerator  *rtAudit.ComplianceReportGenerator
	ApprovalGateway      execution.ApprovalGateway
	EventMappers         map[string]audit.AuditEventMapper
}

// ModelRegistryAdmin provides model governance info for the admin API.
type ModelRegistryAdmin interface {
	ListModels() []ModelInfo
	GetModelUsage(executionID string) (*ModelUsageInfo, bool)
}

// ModelInfo describes a registered model for the admin API.
type ModelInfo struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
	RegisteredAt string   `json:"registered_at"`
}

// ModelUsageInfo describes model usage for a specific execution.
type ModelUsageInfo struct {
	ExecutionID string `json:"execution_id"`
	ModelID     string `json:"model_id"`
	UsedAt      string `json:"used_at"`
}

// RetentionManager provides audit retention policy management for the admin API.
type RetentionManager interface {
	RetentionConfig() RetentionConfigSnapshot
	SetTenantOverride(tenantID string, days int)
	RemoveTenantOverride(tenantID string)
	PurgeExpired() (int64, error)
}

// RetentionConfigSnapshot is the JSON-serializable retention config.
type RetentionConfigSnapshot struct {
	Enabled         bool           `json:"enabled"`
	DefaultDays     int            `json:"default_days"`
	TenantOverrides map[string]int `json:"tenant_overrides"`
}

// AdminRouter handles admin CRUD endpoints.
type AdminRouter struct {
	mux                 *http.ServeMux
	db                  *sql.DB
	providerLister      ProviderLister
	providerReloader    ProviderReloader
	skillAdmin          SkillAdmin
	mcpAdmin            MCPAdmin
	telemetryHandler    *TelemetryHandler
	capabilityLister    CapabilityLister
	auditQuerier        AuditQuerier
	modelRegistry       ModelRegistryAdmin
	retentionManager    RetentionManager
	complianceGenerator *rtAudit.ComplianceReportGenerator
	approvalGateway     execution.ApprovalGateway
	eventMappers        map[string]audit.AuditEventMapper
}

// NewAdminRouter creates an admin router from an AdminRouterConfig.
func NewAdminRouter(cfg AdminRouterConfig) *AdminRouter {
	ar := &AdminRouter{
		mux:                 http.NewServeMux(),
		db:                  cfg.DB,
		providerLister:      cfg.ProviderLister,
		providerReloader:    cfg.ProviderReloader,
		skillAdmin:          cfg.SkillAdmin,
		mcpAdmin:            cfg.MCPAdmin,
		telemetryHandler:    cfg.TelemetryHandler,
		capabilityLister:    cfg.CapabilityLister,
		auditQuerier:        cfg.AuditQuerier,
		modelRegistry:       cfg.ModelRegistry,
		retentionManager:    cfg.RetentionManager,
		complianceGenerator: cfg.ComplianceGenerator,
		approvalGateway:     cfg.ApprovalGateway,
		eventMappers:        cfg.EventMappers,
	}
	ar.registerRoutes()
	return ar
}

// SetProviderReloader allows setting the provider reloader after construction (for tests).
func (ar *AdminRouter) SetProviderReloader(pr ProviderReloader) { ar.providerReloader = pr }

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
	ar.mux.HandleFunc("/v1/admin/telemetry/health", ar.handleTelemetryHealth)
	ar.mux.HandleFunc("/v1/admin/telemetry/spans", ar.handleTelemetrySpans)
	ar.mux.HandleFunc("/v1/admin/telemetry/events", ar.handleTelemetryEvents)
	ar.mux.HandleFunc("/v1/admin/telemetry/metrics", ar.handleTelemetryMetrics)
	ar.mux.HandleFunc("/v1/admin/telemetry/failures", ar.handleTelemetryFailures)
	ar.mux.HandleFunc("/v1/admin/sessions", ar.handleAdminSessions)
	ar.mux.HandleFunc("/v1/admin/sessions/", ar.handleAdminSessionAction)
	ar.mux.HandleFunc("/v1/admin/mcp/servers", ar.handleMCPServers)
	ar.mux.HandleFunc("/v1/admin/mcp/servers/", ar.handleMCPServerAction)
	ar.mux.HandleFunc("/v1/admin/mcp/health", ar.handleMCPHealth)
	ar.mux.HandleFunc("/v1/admin/capabilities", ar.handleCapabilities)
	ar.mux.HandleFunc("/v1/admin/audit", ar.handleAudit)
	ar.mux.HandleFunc("/v1/admin/audit/export", ar.handleAuditExport)
	ar.mux.HandleFunc("/v1/admin/audit/compliance", ar.handleAuditCompliance)
	ar.mux.HandleFunc("/v1/admin/audit/replay", ar.handleAuditReplay)
	ar.mux.HandleFunc("/v1/admin/audit/retention", ar.handleAuditRetention)
	ar.mux.HandleFunc("/v1/admin/models", ar.handleModels)
	ar.mux.HandleFunc("/v1/admin/approval", ar.handleApprovalList)
	ar.mux.HandleFunc("/v1/admin/approval/", ar.handleApprovalAction)
}

func (ar *AdminRouter) handleTelemetryHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if ar.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_configured"})
		return
	}
	ar.telemetryHandler.handleHealth(w, r)
}

func (ar *AdminRouter) handleTelemetrySpans(w http.ResponseWriter, r *http.Request) {
	ar.handleTelemetryGet(w, r, []interface{}{}, ar.telemetryHandler.handleSpans)
}

func (ar *AdminRouter) handleTelemetryEvents(w http.ResponseWriter, r *http.Request) {
	ar.handleTelemetryGet(w, r, []interface{}{}, ar.telemetryHandler.handleEvents)
}

func (ar *AdminRouter) handleTelemetryMetrics(w http.ResponseWriter, r *http.Request) {
	ar.handleTelemetryGet(w, r, map[string]interface{}{}, ar.telemetryHandler.handleMetrics)
}

func (ar *AdminRouter) handleTelemetryFailures(w http.ResponseWriter, r *http.Request) {
	ar.handleTelemetryGet(w, r, map[string]int{}, ar.telemetryHandler.handleFailures)
}

// handleTelemetryGet is the shared boilerplate for GET-only telemetry endpoints
// that forward to a telemetry handler method when configured, or return an empty
// response when telemetry is not configured.
func (ar *AdminRouter) handleTelemetryGet(
	w http.ResponseWriter,
	r *http.Request,
	emptyResponse interface{},
	forward func(http.ResponseWriter, *http.Request),
) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if ar.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, emptyResponse)
		return
	}
	forward(w, r)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
