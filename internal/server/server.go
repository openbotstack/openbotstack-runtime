package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
	coreExecution "github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/config"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	reasoningpkg "github.com/openbotstack/openbotstack-runtime/harness/reasoning"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/internal/skillutil"
	"github.com/openbotstack/openbotstack-runtime/web/webui"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Build runs the full phased initialization and returns a wired, ready-to-serve
// Server. It is the single entry into the composition root: package main parses
// flags and calls this. Any init failure is returned as an error rather than
// terminating the process, so the startup path is testable.
func Build(opts Options) (*Server, error) {
	b := NewServerBuilder(opts)
	b.InitInfrastructure().
		InitAI().
		InitExecution().
		InitCapabilities().
		InitAudit().
		InitAgent().
		InitMemory().
		InitTelemetry()

	if b.err != nil {
		b.Cleanup()
		return nil, b.err
	}

	deps, err := b.BuildDeps()
	if err != nil {
		b.Cleanup()
		return nil, err
	}
	skillAdmin := b.SkillAdmin()
	if deps.SkillWatcher != nil {
		skillAdmin.SetReloader(deps.SkillWatcher)
	}

	srv, err := newServer(deps, skillAdmin, b.Config(), opts)
	if err != nil {
		b.Cleanup()
		return nil, err
	}
	srv.cleanup = b.Cleanup
	return srv, nil
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
	RetentionPolicy     *rtAudit.RetentionPolicy
	ApprovalGateway     coreExecution.ApprovalGateway
	ReasoningStore      *reasoningpkg.InMemoryStore
	Telemetry           *api.TelemetryHandler
	MCPAdmin            api.MCPAdmin
	CapRegistry         capability.CapabilityRegistry
	SkillWatcher        *skillutil.SkillWatcher
}

// Server bundles the HTTP mux with serving configuration.
type Server struct {
	mux     *http.ServeMux
	cfg     *config.Config
	cleanup func()
}

// Cleanup runs deferred teardown for resources acquired during initialization.
func (s *Server) Cleanup() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

// newServer wires all HTTP routes, middleware, and API handlers.
func newServer(deps *ServerDeps, skillAdmin *api.SkillAdminService, cfg *config.Config, opts Options) (*Server, error) {
	mux := http.NewServeMux()

	var capLister api.CapabilityLister
	if deps.CapRegistry != nil {
		capLister = api.CapabilityListerFunc{Registry: deps.CapRegistry}
	}

	// Composite auth: API Key first, then JWT fallback
	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{
		DB:     deps.DB.DB,
		Strict: os.Getenv("OBS_AUTH_STRICT") == "true",
	})
	var authMW = apiKeyMW

	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		if len(jwtSecret) < 32 {
			return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters (got %d)", len(jwtSecret))
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
		History:          api.NewHistoryProvider(deps.MarkdownStore, deps.SessionStore),
		ReasoningStore:   deps.ReasoningStore,
		TelemetryHandler: deps.Telemetry,
		BuildInfo: api.BuildInfo{
			Version:   opts.Version,
			Commit:    opts.Commit,
			Branch:    opts.Branch,
			BuildTime: opts.BuildTime,
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
		DB:                   deps.DB.DB,
		ProviderLister:       &api.RouterProviderLister{Router: deps.ModelRouter},
		ProviderReloader:     &api.RouterProviderReloader{Router: deps.ModelRouter, Factory: deps.ProviderFactory},
		SkillAdmin:           skillAdmin,
		MCPAdmin:             deps.MCPAdmin,
		TelemetryHandler:     deps.Telemetry,
		CapabilityLister:     capLister,
		AuditQuerier:         deps.AuditLogger,
		ComplianceGenerator:  deps.ComplianceGenerator,
		RetentionPolicy:      deps.RetentionPolicy,
		ApprovalGateway:      deps.ApprovalGateway,
	})
	mux.Handle("/v1/admin/", authMW(adminRouter.Handler()))

	// UI routes (embedded dual SPA) — both require auth
	mux.Handle("/ui/", authMW(http.StripPrefix("/ui", webui.UserHandler())))
	mux.Handle("/admin/", authMW(http.StripPrefix("/admin", webui.AdminHandler())))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	slog.Info("agent initialized", "loaded_skills", len(deps.Exec.List()))

	return &Server{mux: mux, cfg: cfg}, nil
}

// ListenAndServe starts the HTTP server with graceful shutdown.
func (s *Server) ListenAndServe() error {
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

	errCh := make(chan error, 1)
	go func() {
		if s.cfg.TLS.CertFile != "" && s.cfg.TLS.KeyFile != "" {
			slog.Info("server listening with TLS", "addr", s.cfg.Server.Addr, "cert", s.cfg.TLS.CertFile)
			errCh <- srv.ListenAndServeTLS(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
		} else {
			slog.Info("server listening", "addr", s.cfg.Server.Addr)
			errCh <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}

	slog.Info("openbotstack stopped")
	return nil
}
