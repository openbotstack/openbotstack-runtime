// Package main provides the OpenBotStack runtime entrypoint.
//
// This is the single executable that runs the OpenBotStack platform.
// It can be configured to run different components via flags or config:
//   - API server (user plane)
//   - Admin endpoints (management plane)
//   - Worker processes (skill execution)
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

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/config"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/web/webui"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/control/skills"
)

var (
	configPath = flag.String("config", "./config.yaml", "Path to config file")
	listenAddr = flag.String("addr", ":8080", "Listen address")
	runMode    = flag.String("mode", "all", "Run mode: all, api, worker")
)

func main() {
	flag.Parse()

	// Setup structured logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load Configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// CLI flags override config if explicitly set (simple check for now, can be improved)
	if *listenAddr != ":8080" {
		cfg.Server.Addr = *listenAddr
	}

	slog.Info("starting openbotstack",
		"addr", cfg.Server.Addr,
		"mode", *runMode,
		"llm_provider", cfg.Providers.LLM.Default,
	)

	// Initialize Wasm Runtime
	wasmRuntime, err := wasm.NewRuntime()
	if err != nil {
		slog.Error("failed to initialize wasm runtime", "error", err)
		os.Exit(1)
	}
	defer wasmRuntime.Close() //nolint:errcheck // best-effort cleanup on shutdown

	// Initialize Model Router
	modelRouter := router.NewDefaultRouter()

	// Determine provider
	providerName := cfg.Providers.LLM.Default
	var providerConfig config.LLMProviderConfig

	if providerName == "modelscope" {
		providerConfig = cfg.Providers.LLM.ModelScope
	} else {
		providerConfig = cfg.Providers.LLM.OpenAI
	}

	if providerConfig.APIKey != "" {
		// Create the correct provider type based on configuration
		var llmProvider providers.ModelProvider
		switch providerName {
		case "modelscope":
			llmProvider = providers.NewModelScopeProvider(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		default:
			llmProvider = providers.NewOpenAIProvider(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		}

		if err := modelRouter.Register(llmProvider); err != nil {
			slog.Error("failed to register provider", "error", err)
		} else {
			slog.Info("llm provider registered", "provider", providerName, "model", providerConfig.Model, "base_url", providerConfig.BaseURL)
		}
	} else {
		slog.Warn("LLM API key not set, LLM features will be disabled")
	}

	hostFuncs := &wasm.HostFunctions{
		LLMGenerate: func(ctx context.Context, prompt string) (string, error) {
			mReq := skills.GenerateRequest{
				Messages: []skills.Message{
					{Role: "user", Content: prompt},
				},
			}
			prov, err := modelRouter.Route([]skills.CapabilityType{skills.CapTextGeneration}, skills.ModelConstraints{})
			if err != nil {
				return "LLM not configured or suitable provider not found", nil
			}
			resp, err := prov.Generate(ctx, mReq)
			if err != nil {
				return "", err
			}
			return resp.Content, nil
		},
		Log: func(ctx context.Context, level, msg string) {
			slog.Info("wasm log", "level", level, "msg", msg)
		},
	}

	// Initialize Executor
	exec := executor.NewDefaultExecutorWithRuntime(wasmRuntime, nil)

	// Register Host Functions with Wasm Runtime (linked to our hostFuncs)
	if err := wasmRuntime.RegisterHostFunctions(context.Background(), hostFuncs); err != nil {
		slog.Error("failed to register host functions", "error", err)
		os.Exit(1)
	}

	// Load Skills
	skillsPath := os.Getenv("OBS_SKILLS_PATH")
	if skillsPath == "" {
		skillsPath = "./examples/skills"
	}
	if err := loadSkills(context.Background(), exec, skillsPath); err != nil {
		slog.Error("failed to load skills", "error", err)
	}

	// Create Planner
	// LLM configuration IS REQUIRED for production operation
	planner := agent.NewLLMPlanner(modelRouter)
	slog.Info("planner initialized with LLM router")

	// Initialize SQLite Persistence
	dbPath := os.Getenv("OBS_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "openbotstack.db"
	}
	pdb, err := persistence.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer pdb.Close()
	if err := pdb.Migrate(); err != nil {
		slog.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateTenantColumn(); err != nil {
		slog.Error("failed to migrate tenant column", "error", err)
		os.Exit(1)
	}
	slog.Info("sqlite database initialized", "path", dbPath)

	// Seed default tenant, admin user, and API key if no tenants exist
	if os.Getenv("OBS_SEED_DEFAULTS") != "false" {
		seedKey, err := pdb.SeedDefaults()
		if err != nil {
			slog.Error("failed to seed defaults", "error", err)
			os.Exit(1)
		}
		if seedKey != "" {
			fmt.Println("⚠️  Default admin API Key (save this, it won't be shown again):")
			fmt.Printf("    %s\n", seedKey)
			fmt.Println()
			fmt.Println("    Tenant: default  User: admin  Role: admin")
		}
	}

	// Initialize stores with SQLite
	memoryStore := memory.NewSQLiteMemoryStore(pdb.DB)
	_ = ratelimit.NewSQLiteQuotaStore(pdb.DB) // wired when rate limiting middleware is added

	// Create Assistant Identity (Runtime)
	// In a real system, these would be loaded from a database based on the request context.
	// For now, we use defaults to satisfy the interface.
	art := &assistant.AssistantRuntime{
		AssistantID: "default",
	}

	// Create Agent (orchestrates Planner + Executor)
	apiAgent := agent.NewDefaultAgent(planner, exec, exec, art)
	slog.Info("agent initialized", "loaded_skills", len(exec.List()))

	// Initialize Audit Logger
	auditLogger := audit.NewSQLiteAuditLogger(pdb.DB)
	slog.Info("sqlite audit logger initialized")

	// Create combined router
	mux := http.NewServeMux()

	// API routes
	apiRouter := api.NewRouter(apiAgent)
	apiRouter.SetSkillProvider(exec)
	apiRouter.SetExecutionStore(api.NewAuditExecutionStore(auditLogger))
	apiRouter.SetHistoryProvider(&memoryHistoryProvider{store: memoryStore})

	// Configure composite auth: API Key first, then JWT fallback
	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{
		DB:     pdb.DB,
		Strict: os.Getenv("OBS_AUTH_STRICT") == "true",
	})

	var authMW func(http.Handler) http.Handler
	authMW = apiKeyMW // Start with API Key middleware

	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		jwtMW := middleware.JWTMiddleware(middleware.JWTMiddlewareConfig{
			SecretKey: []byte(jwtSecret),
			Strict:    os.Getenv("JWT_STRICT") == "true",
		})
		// Compose: API Key first, then JWT as fallback
		// If API Key already set user, JWT middleware skips (check in jwt.go)
		authMW = func(next http.Handler) http.Handler {
			return apiKeyMW(jwtMW(next))
		}
		slog.Info("composite auth enabled (API Key + JWT)")
	} else {
		slog.Info("API Key authentication enabled")
	}
	apiRouter.SetAuthMiddleware(authMW)

	mux.Handle("/health", apiRouter)
	mux.Handle("/healthz", apiRouter)
	mux.Handle("/readyz", apiRouter)
	mux.Handle("/metrics", apiRouter)
	mux.Handle("/v1/", apiRouter)

	// Admin endpoints require auth (API Key or JWT) AND admin role
	adminRouter := api.NewAdminRouter(pdb.DB)
	mux.Handle("/v1/admin/", authMW(adminRouter.Handler()))

	// UI routes (embedded frontend)
	mux.Handle("/ui/", http.StripPrefix("/ui", webui.Handler()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Wrap with correlation ID middleware for structured logging
	handler := api.CorrelationMiddleware(mux)

	// Create server
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server in goroutine
	go func() {
		slog.Info("server listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	slog.Info("shutting down gracefully...")

	// Shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	fmt.Println("openbotstack stopped")
}

// memoryHistoryProvider adapts a memory.ShortTermStore to the api.HistoryProvider interface.
type memoryHistoryProvider struct {
	store memory.ShortTermStore
}

func (p *memoryHistoryProvider) GetSessionHistory(ctx context.Context, sessionID string) ([]api.Message, error) {
	if p.store == nil {
		return []api.Message{}, nil
	}
	entries, err := p.store.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	messages := make([]api.Message, 0, len(entries))
	for _, entry := range entries {
		// By default assume user role unless we track it explicitly in tags
		// We'll map "role:assistant" tag if present, else default to "user"
		role := "user"
		for _, tag := range entry.Tags {
			if tag == "role:assistant" {
				role = "assistant"
				break
			}
		}
		messages = append(messages, api.Message{
			Role:    role,
			Content: entry.Content,
		})
	}
	return messages, nil
}
