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
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/control/skills"
	plannerpkg "github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/config"
	dualloop "github.com/openbotstack/openbotstack-runtime/agent"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/loop"
	"github.com/openbotstack/openbotstack-runtime/memory"
	contextassembler "github.com/openbotstack/openbotstack-runtime/context"
	"github.com/openbotstack/openbotstack-runtime/internal/adapters"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
	"github.com/openbotstack/openbotstack-runtime/toolrunner/tool_invocation"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/web/webui"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// Load Configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("failed to load config", "error", err)
	}

	// Setup structured logging with configurable log level
	logLevel := parseLogLevel(cfg.Observability.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Initialize OpenTelemetry (Prometheus metrics + optional tracing)
	otelCleanup, err := observability.Setup(context.Background(), cfg.Observability, "dev")
	if err != nil {
		slog.Error("failed to initialize OpenTelemetry", "error", err)
		os.Exit(1)
	}
	defer otelCleanup()

	// Initialize OTel metric instruments (counters, histograms)
	if err := observability.InitMetrics(); err != nil {
		slog.Error("failed to initialize OTel metrics", "error", err)
		os.Exit(1)
	}
	if err := observability.InitAppMetrics(); err != nil {
		slog.Error("failed to initialize app metrics", "error", err)
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

	switch providerName {
	case "modelscope":
		providerConfig = cfg.Providers.LLM.ModelScope
	case "claude":
		providerConfig = cfg.Providers.LLM.Claude
	default:
		providerConfig = cfg.Providers.LLM.OpenAI
	}

	if providerConfig.APIKey != "" {
		// Create the correct provider type based on configuration
		var llmProvider providers.ModelProvider
		switch providerName {
		case "modelscope":
			llmProvider = providers.NewModelScopeProvider(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		case "claude":
			llmProvider = providers.NewClaudeProvider(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
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

	// Wire TextGenerator for declarative (non-Wasm) skill execution
	exec.SetTextGenerator(&adapters.LLMTextGenerator{Router: modelRouter})

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

	// Dual-loop planner (used when agent mode is "dual_loop")
	// Use generous limits for local/thinking models that need extra time.
	dualPlannerLimits := &plannerpkg.ExecutionLimits{
		MaxSteps:         10,
		MaxToolCalls:     5,
		MaxExecutionTime: 45 * time.Second,
	}
	dualPlanner := plannerpkg.NewLLMPlanner(modelRouter, dualPlannerLimits)

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
	defer func() { _ = pdb.Close() }()
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

	// Seed provider config from config.yaml into SQLite (if not already present).
	// This runs independently of OBS_SEED_DEFAULTS because provider config is
	// operational state, not default user data.
	if providerConfig.APIKey != "" {
		var existing int
		_ = pdb.QueryRow("SELECT COUNT(*) FROM provider_config WHERE provider_name = ?", providerName).Scan(&existing)
		if existing == 0 {
			seedNow := time.Now().UTC().Format(time.RFC3339Nano)
			_, err := pdb.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
				VALUES (?, ?, ?, ?, 1, ?)`,
				providerName, providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model, seedNow)
			if err != nil {
				slog.Warn("failed to seed provider config into SQLite", "provider", providerName, "error", err)
			} else {
				slog.Info("seeded provider config into SQLite", "provider", providerName)
			}
		}
	}

	// Initialize stores with SQLite
	sessionStateStore := memory.NewSQLiteSessionStateStore(pdb.DB,
		memory.WithStrictTenant(os.Getenv("OBS_AUTH_STRICT") == "true"),
	)
	quotaStore := ratelimit.NewSQLiteQuotaStore(pdb.DB)
	rateLimiter := ratelimit.NewSQLiteRateLimiter(pdb.DB, quotaStore)

	// Create Assistant Identity (Runtime)
	// In a real system, these would be loaded from a database based on the request context.
	// For now, we use defaults to satisfy the interface.
	art := &assistant.AssistantRuntime{
		AssistantID: "default",
	}

	// Create RegistryClient early for ToolRunner wiring
	registryClient := toolrunner.NewRegistryClient(cfg.Sandbox.ToolRegistryURL)

	// Create Agent (orchestrates Planner + Executor)
	var apiAgent agent.Agent
	if cfg.Agent.Mode == "dual_loop" {
		innerCfg := loop.InnerLoopConfig{
			MaxTurns:       cfg.Agent.DualLoop.MaxTurns,
			MaxToolCalls:   cfg.Agent.DualLoop.MaxToolCalls,
			MaxTurnRuntime: cfg.Agent.DualLoop.MaxTurnRuntime,
		}
		compactor := loop.NewDefaultContextCompactor(cfg.Agent.DualLoop.MaxRetainedTurns)
		toolRunner := toolrunner.NewRegistryToolRunner(registryClient)
		innerLoop := loop.NewDefaultInnerLoop(innerCfg, dualPlanner, toolRunner, compactor, nil)
		innerLoop.SetSkillExecutor(exec)

		outerCfg := loop.OuterLoopConfig{
			MaxWorkflowSteps:  cfg.Agent.DualLoop.MaxWorkflowSteps,
			MaxSessionRuntime: cfg.Agent.DualLoop.MaxSessionRuntime,
		}
		outerLoop := loop.NewDefaultOuterLoop(outerCfg, innerLoop, &loop.NoOpCheckpoint{}, nil, nil)

		dla := dualloop.NewDualLoopAgent(dualPlanner, exec, art, innerLoop, outerLoop)
		apiAgent = dla
		// Setters called below after convStore/contextAssembler are ready
		slog.Info("dual-loop agent initialized",
			"max_turns", innerCfg.MaxTurns,
			"max_workflow_steps", outerCfg.MaxWorkflowSteps,
		)
	} else {
		da := agent.NewDefaultAgent(planner, exec, exec, art)
		apiAgent = da
		slog.Info("single-pass agent initialized")
	}

	// Initialize Markdown Memory Store (3+1 layered model)
	markdownStore, err := memory.NewMarkdownMemoryStore(cfg.Memory.DataDir)
	if err != nil {
		slog.Error("failed to create markdown memory store", "error", err)
		os.Exit(1)
	}
	slog.Info("markdown memory store initialized", "data_dir", cfg.Memory.DataDir)

	// Wrap with summarizer if enabled
	var convStore agent.ConversationStore = markdownStore
	if cfg.Memory.SummaryEnabled {
		summarizer := memory.NewConversationSummarizer(markdownStore, modelRouter, cfg.Memory.SummaryThreshold)
		convStore = memory.NewSummarizingConversationStore(markdownStore, summarizer)
		slog.Info("conversation summarization enabled", "threshold", cfg.Memory.SummaryThreshold)
	}

	// Wrap with dual-write decorator (Markdown content + SQLite metadata)
	convStore = memory.NewDualWriteConversationStore(convStore, sessionStateStore)

	// Sync existing Markdown sessions to SQLite (one-time migration)
	if err := memory.SyncMarkdownToSQLite(context.Background(), markdownStore, sessionStateStore); err != nil {
		slog.Warn("failed to sync existing sessions to SQLite", "error", err)
	}

	// Start periodic reconciliation to repair any SQLite/Markdown drift
	stopReconciliation := memory.StartReconciliation(context.Background(), markdownStore, sessionStateStore, 5*time.Minute)
	defer stopReconciliation()

	// Inject shared dependencies into agent (type-specific setter calls)
	switch a := apiAgent.(type) {
	case *agent.DefaultAgent:
		a.SetConversationStore(convStore)
		a.SetMaxHistoryMessages(cfg.Memory.MaxHistoryMessages)
	case *dualloop.DualLoopAgent:
		a.SetConversationStore(convStore)
		a.SetMaxHistoryMessages(cfg.Memory.MaxHistoryMessages)
	}

	// Initialize MemoryManager Bridge (markdown-first, optional vector search)
	memoryBridge := memory.NewMarkdownMemoryBridge(markdownStore, nil)

	// Initialize optional vector search layer (requires PostgreSQL + pgvector)
	if cfg.Vector.Enabled && cfg.Vector.DatabaseURL != "" {
		pgPool, err := pgxpool.New(context.Background(), cfg.Vector.DatabaseURL)
		if err != nil {
			slog.Error("failed to parse vector database URL", "error", err)
			os.Exit(1)
		}
		// Validate the connection actually works (pgxpool.New doesn't ping)
		if err := pgPool.Ping(context.Background()); err != nil {
			slog.Error("failed to connect to vector database", "error", err)
			pgPool.Close()
			os.Exit(1)
		}
		defer pgPool.Close() //nolint:errcheck // cleanup on shutdown

		vectorStore := memory.NewPgVectorStore(pgPool, cfg.Vector.Dimensions)
		if err := vectorStore.Migrate(context.Background()); err != nil {
			slog.Error("failed to migrate vector store", "error", err)
			os.Exit(1)
		}

		embeddingSvc := memory.NewEmbeddingService(modelRouter, cfg.Vector.Model, cfg.Vector.Dimensions)
		memoryBridge.SetVectorStore(vectorStore)
		memoryBridge.SetEmbeddingService(embeddingSvc)

		// Wire async indexer into conversation store
		indexer := memory.NewAsyncEmbeddingIndexer(embeddingSvc, vectorStore)
		if summarizingStore, ok := convStore.(*memory.SummarizingConversationStore); ok {
			summarizingStore.SetIndexer(indexer)
		}
		slog.Info("vector search enabled",
			"model", cfg.Vector.Model,
			"dimensions", cfg.Vector.Dimensions,
		)
	} else {
		slog.Info("vector search disabled (keyword matching only)")
	}

	// Initialize ContextAssembler (persona + memory -> prompt enrichment)
	contextAssembler := contextassembler.NewRuntimeContextAssembler(exec, memoryBridge)
	switch a := apiAgent.(type) {
	case *agent.DefaultAgent:
		a.SetContextAssembler(contextAssembler)
	case *dualloop.DualLoopAgent:
		a.SetContextAssembler(contextAssembler)
		a.SetMemoryManager(memoryBridge)
	}
	slog.Info("context assembler initialized")

	// Wire Tool Invocation Pipeline for Wasm skill HTTP access.
	// WireHTTPFetch must be called AFTER RegisterHostFunctions because both share
	// the same hostFuncs pointer. The Wasm host module closures dereference hf at
	// call time, so setting HTTPFetch here is visible to already-registered functions.
	httpAllowlist := wasm.NewHTTPAllowlist(cfg.Sandbox.HTTPAllowlist)
	sandboxedClient := wasm.NewSandboxedHTTPClientWithSSRF(httpAllowlist, nil)
	// registryClient already created above (for ToolRunner wiring)
	toolPipeline := tool_invocation.NewToolInvocationPipeline(sandboxedClient, registryClient, 30*time.Second)
	tool_invocation.WireHTTPFetch(hostFuncs, toolPipeline)
	slog.Info("tool invocation pipeline wired",
		"allowlist", cfg.Sandbox.HTTPAllowlist,
		"registry_url", cfg.Sandbox.ToolRegistryURL,
	)

	slog.Info("agent initialized", "loaded_skills", len(exec.List()))

	// Initialize Audit Logger
	auditLogger := audit.NewSQLiteAuditLogger(pdb.DB)
	slog.Info("sqlite audit logger initialized")

	// Wire AuditLogger into SkillExecutor for execution write-path
	exec.SetAuditLogger(auditLogger)
	slog.Info("audit logger wired to executor")

	// Create combined router
	mux := http.NewServeMux()

	// API routes
	apiRouter := api.NewRouter(apiAgent)
	skillAdmin := adapters.NewSkillAdminAdapter(exec)
	apiRouter.SetSkillProvider(exec)
	apiRouter.SetSkillDisabledChecker(skillAdmin.IsDisabled)
	apiRouter.SetExecutionStore(api.NewAuditExecutionStore(auditLogger))
	apiRouter.SetHistoryProvider(adapters.NewHistoryProvider(markdownStore, sessionStateStore))
	apiRouter.SetBuildInfo(api.BuildInfo{
		Version:   version,
		Commit:    commit,
		Branch:    branch,
		BuildTime: buildTime,
	})

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
	mux.Handle("/version", apiRouter)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			api.MetricsHandler().ServeHTTP(w, r)
		})
	rateLimitMW := middleware.RateLimitMiddleware(rateLimiter)
	mux.Handle("/v1/", rateLimitMW(apiRouter))

	// /v1/me is a user-level endpoint — any authenticated user can call it.
	// Not behind RequireAdmin. Insert after /v1/ catch-all (ServeMux longest-prefix wins).
	mux.Handle("/v1/me", authMW(http.HandlerFunc(api.HandleMe)))

	// Admin endpoints require auth (API Key or JWT) AND admin role
	adminRouter := api.NewAdminRouter(pdb.DB)
	mux.Handle("/v1/admin/", authMW(adminRouter.Handler()))
		adminRouter.SetProviderLister(&adapters.ModelRouterLister{Router: modelRouter})
		adminRouter.SetProviderReloader(&adapters.ProviderReloader{Router: modelRouter})
		adminRouter.SetSkillAdmin(skillAdmin)

	// UI routes (embedded frontends — dual SPA)
	mux.Handle("/ui/", http.StripPrefix("/ui", webui.UserHandler()))
	mux.Handle("/admin/", http.StripPrefix("/admin", webui.AdminHandler()))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/ui/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Correlation ID middleware for structured logging.
	// Runs inside the OTel span so correlation_id can be attached to the span.
	correlationHandler := api.CorrelationMiddleware(mux)

	// OTel HTTP metrics middleware (records request counts and durations).
	metricsHandler := observability.MetricsMiddleware(correlationHandler)

	// CORS middleware for web UI compatibility.
	// Configurable via config.yaml; defaults to ["*"] for development.
	corsHandler := middleware.CORSMiddleware(middleware.CORSConfig{
		AllowedOrigins:   cfg.CORS.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key", "Authorization"},
		AllowCredentials: true,
	})(metricsHandler)

	// OTel HTTP instrumentation (creates spans for each request).
	// Must be the outermost middleware so the span exists when inner middleware runs.
	// Execution order: otelhttp → CORS → MetricsMiddleware → CorrelationMiddleware → mux → auth → RateLimit → handlers
	handler := otelhttp.NewHandler(corsHandler, "openbotstack",
		otelhttp.WithFilter(func(r *http.Request) bool {
			// Skip health/metrics endpoints from tracing overhead.
			path := r.URL.Path
			return path != "/health" && path != "/healthz" && path != "/readyz" && path != "/metrics"
		}),
	)

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
		if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
			slog.Info("server listening with TLS", "addr", cfg.Server.Addr,
				"cert", cfg.TLS.CertFile)
			if err := srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
		} else {
			slog.Info("server listening", "addr", cfg.Server.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("server error", "error", err)
				os.Exit(1)
			}
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

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
