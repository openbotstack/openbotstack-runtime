// Package main provides the OpenBotStack runtime entrypoint.
//
// Usage:
//
//	openbotstack [flags]
//
// Flags:
//
//	--config    Path to config file (default: ./config.yaml)
//	--addr      Listen address (default: :8080)
//	--mode      Run mode: all, api, worker (default: all)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-core/control/agent"
	coreExecution "github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/config"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	reasoningpkg "github.com/openbotstack/openbotstack-runtime/harness/reasoning"
	"github.com/openbotstack/openbotstack-runtime/internal/adapters"
	"github.com/openbotstack/openbotstack-runtime/internal/crypto"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/web/webui"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

var (
	configPath = flag.String("config", "./config.yaml", "Path to config file")
	listenAddr = flag.String("addr", ":8080", "Listen address")
	runMode    = flag.String("mode", "all", "Run mode: all, api, worker")

	// Build metadata injected via -ldflags.
	version   string
	commit    string
	branch    string
	buildTime string
)

func main() {
	flag.Parse()

	builder := NewServerBuilder()
	builder.
		InitInfrastructure().
		InitMemory().
		InitAI().
		InitExecution().
		InitCapabilities().
		InitTelemetry().
		InitAudit().
		InitAgent()
	defer builder.Cleanup()

	deps := builder.Build()
	skillAdmin := builder.SkillAdmin()
	server := NewServer(deps, skillAdmin, builder.Config())
	server.ListenAndServe()
}

// ServerDeps collects all dependencies needed to wire HTTP routes.
type ServerDeps struct {
	Agent               agent.Agent
	Exec                *executor.DefaultExecutor
	ModelRouter         *router.DefaultRouter
	ProviderFactory     *providers.ProviderFactory
	DB                  *persistence.DB
	MarkdownStore       *memory.MarkdownMemoryStore
	SessionStore        memory.SessionStateStore
	RateLimiter         *ratelimit.SQLiteRateLimiter
	AuditLogger         *audit.SQLiteAuditLogger
	ComplianceGenerator *rtAudit.ComplianceReportGenerator
	RetentionManager    api.RetentionManager
	ApprovalGateway     coreExecution.ApprovalGateway
	ReasoningStore      *reasoningpkg.InMemoryStore
	Telemetry           *api.TelemetryHandler
	MCPAdmin            api.MCPAdmin
	CapRegistry         capability.CapabilityRegistry
}

// Server bundles the HTTP mux with serving configuration.
type Server struct {
	mux *http.ServeMux
	cfg *config.Config
}

// NewServer wires all HTTP routes, middleware, and API handlers.
func NewServer(deps ServerDeps, skillAdmin *adapters.SkillAdminAdapter, cfg *config.Config) *Server {
	mux := http.NewServeMux()

	var capLister api.CapabilityLister
	if deps.CapRegistry != nil {
		capLister = adapters.NewCapabilityLister(deps.CapRegistry)
	}

	// Composite auth: API Key first, then JWT fallback
	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{
		DB:     deps.DB.DB,
		Strict: os.Getenv("OBS_AUTH_STRICT") == "true",
	})
	var authMW func(http.Handler) http.Handler = apiKeyMW

	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		if len(jwtSecret) < 32 {
			slog.Error("JWT_SECRET must be at least 32 characters")
			os.Exit(1)
		}
		jwtMW := middleware.JWTMiddleware(middleware.JWTMiddlewareConfig{
			SecretKey: []byte(jwtSecret),
			Strict:    os.Getenv("JWT_STRICT") == "true",
		})
		authMW = func(next http.Handler) http.Handler {
			return apiKeyMW(jwtMW(next))
		}
		slog.Info("composite auth enabled (API Key + JWT)")
	} else {
		slog.Info("API Key authentication enabled")
	}

	apiRouter := api.NewRouter(api.RouterConfig{
		Agent:            deps.Agent,
		AuthMiddleware:   authMW,
		Skills:           deps.Exec,
		SkillDisabled:    skillAdmin.IsDisabled,
		ExecStore:        api.NewAuditExecutionStore(deps.AuditLogger),
		History:          adapters.NewHistoryProvider(deps.MarkdownStore, deps.SessionStore),
		ReasoningStore:   deps.ReasoningStore,
		TelemetryHandler: deps.Telemetry,
		BuildInfo: api.BuildInfo{
			Version:   version,
			Commit:    commit,
			Branch:    branch,
			BuildTime: buildTime,
		},
	})

	mux.Handle("/health", apiRouter)
	mux.Handle("/healthz", apiRouter)
	mux.Handle("/readyz", apiRouter)
	mux.Handle("/version", apiRouter)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		api.MetricsHandler().ServeHTTP(w, r)
	})
	rateLimitMW := middleware.RateLimitMiddleware(deps.RateLimiter)
	mux.Handle("/v1/", rateLimitMW(apiRouter))

	// OpenAPI spec (optional — served from file if present)
	if specData, err := os.ReadFile("openapi.json"); err == nil {
		mux.Handle("/v1/openapi.json", api.NewOpenAPISpec(specData))
	} else if specData, err := os.ReadFile("../openbotstack-docs/api/openapi.yaml"); err == nil {
		mux.Handle("/v1/openapi.json", api.NewOpenAPISpec(specData))
	}

	adminRouter := api.NewAdminRouter(api.AdminRouterConfig{
		DB:               deps.DB.DB,
		ProviderLister:   &adapters.ModelRouterLister{Router: deps.ModelRouter},
		ProviderReloader: &adapters.ProviderReloader{Router: deps.ModelRouter, Factory: deps.ProviderFactory},
		SkillAdmin:       skillAdmin,
		MCPAdmin:         deps.MCPAdmin,
		TelemetryHandler: deps.Telemetry,
		CapabilityLister: capLister,
		AuditQuerier:         deps.AuditLogger,
		ComplianceGenerator: deps.ComplianceGenerator,
		RetentionManager:    deps.RetentionManager,
		ApprovalGateway:     deps.ApprovalGateway,
	})
	mux.Handle("/v1/admin/", authMW(adminRouter.Handler()))

	// UI routes (embedded dual SPA) — both require auth
	mux.Handle("/ui/", authMW(http.StripPrefix("/ui", webui.UserHandler())))
	mux.Handle("/admin/", authMW(middleware.RequireAdmin(http.StripPrefix("/admin", webui.AdminHandler()))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	slog.Info("agent initialized", "loaded_skills", len(deps.Exec.List()))

	return &Server{mux: mux, cfg: cfg}
}

// ListenAndServe starts the HTTP server with graceful shutdown.
func (s *Server) ListenAndServe() {
	correlationHandler := api.CorrelationMiddleware(s.mux)
	securityHandler := middleware.SecurityHeadersMiddleware(correlationHandler)
	metricsHandler := observability.MetricsMiddleware(securityHandler)

	if len(s.cfg.CORS.AllowedOrigins) == 1 && s.cfg.CORS.AllowedOrigins[0] == "*" {
		slog.Warn("CORS: AllowCredentials=true with AllowedOrigins=[\"*\"] accepts any origin — configure explicit origins for production")
	}
	corsHandler := middleware.CORSMiddleware(middleware.CORSConfig{
		AllowedOrigins:   s.cfg.CORS.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key", "Authorization"},
		AllowCredentials: true,
	})(metricsHandler)

	handler := otelhttp.NewHandler(corsHandler, "openbotstack",
		otelhttp.WithFilter(func(r *http.Request) bool {
			path := r.URL.Path
			return path != "/health" && path != "/healthz" && path != "/readyz" && path != "/metrics"
		}),
	)

	srv := &http.Server{
		Addr:         s.cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  600 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if s.cfg.TLS.CertFile != "" && s.cfg.TLS.KeyFile != "" {
			slog.Info("server listening with TLS", "addr", s.cfg.Server.Addr, "cert", s.cfg.TLS.CertFile)
			if err := srv.ListenAndServeTLS(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("server listening", "addr", s.cfg.Server.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	fmt.Println("openbotstack stopped")
}

// seedProviderConfig persists provider credentials from config.yaml to SQLite.
func seedProviderConfig(pdb *persistence.DB, providerName string, providerConfig config.LLMProviderConfig) {
	if providerConfig.APIKey == "" {
		return
	}
	var existing int
	_ = pdb.QueryRow("SELECT COUNT(*) FROM provider_config WHERE provider_name = ?", providerName).Scan(&existing)
	if existing > 0 {
		return
	}

	seedNow := time.Now().UTC().Format(time.RFC3339Nano)
	seedKey := providerConfig.APIKey
	if encKey := crypto.EncryptionKey(); encKey != nil {
		enc, err := crypto.Encrypt(encKey, seedKey)
		if err != nil {
			slog.Warn("failed to encrypt provider api key for storage", "error", err)
		} else {
			seedKey = enc
		}
	}
	_, err := pdb.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, 1, ?)`,
		providerName, providerConfig.BaseURL, seedKey, providerConfig.Model, seedNow)
	if err != nil {
		slog.Warn("failed to seed provider config into SQLite", "provider", providerName, "error", err)
	} else {
		slog.Info("seeded provider config into SQLite", "provider", providerName)
	}
}
