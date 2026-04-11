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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"


	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/config"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/memory"
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

	// Build info injected via -ldflags
	version   = "dev"
	commit    = "none"
	branch    = "unknown"
	buildTime = "unknown"
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

	// Initialize Memory Store
	var memoryStore memory.ShortTermStore
	var redisClient *redis.Client
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opt, err := redis.ParseURL(redisURL)
		if err != nil {
			slog.Error("failed to parse redis url", "error", err)
			os.Exit(1)
		}
		redisClient = redis.NewClient(opt)
		memoryStore = memory.NewRedisMemoryStore(redisClient)
		slog.Info("redis memory store initialized")
	} else {
		// Fallback to a simple in-memory implementation if REDIS_URL is not provided.
		// For now, we'll just leave it nil so history is empty.
		slog.Warn("no REDIS_URL provided, session history will be disabled")
	}

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
	var auditLogger audit.AuditLogger
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		slog.Info("connecting to database for audit logging")
		pool, err := pgxpool.New(context.Background(), dbURL)
		if err != nil {
			slog.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		pgLogger := audit.NewPGAuditLogger(pool)
		if err := pgLogger.Initialize(context.Background()); err != nil {
			slog.Error("failed to initialize audit log schema", "error", err)
			os.Exit(1)
		}
		auditLogger = pgLogger
		slog.Info("postgresql audit logger initialized")
	} else {
		auditLogger = audit.NewInMemoryAuditLogger()
		slog.Info("in-memory audit logger initialized (no DATABASE_URL provided)")
	}

	// Create combined router
	mux := http.NewServeMux()

	// API routes
	apiRouter := api.NewRouter(apiAgent)
	apiRouter.SetSkillProvider(exec)
	apiRouter.SetExecutionStore(api.NewAuditExecutionStore(auditLogger))
	apiRouter.SetHistoryProvider(&memoryHistoryProvider{store: memoryStore})

	// Wire build info
	apiRouter.SetBuildInfo(api.BuildInfo{
		Version:   version,
		Commit:    commit,
		Branch:    branch,
		BuildTime: buildTime,
	})

	// Wire health checkers
	var checkers []api.HealthChecker
	if redisClient != nil {
		checkers = append(checkers, api.NewRedisHealthChecker(func(ctx context.Context) error {
			return redisClient.Ping(ctx).Err()
		}))
	}
	if providerConfig.APIKey != "" {
		checkers = append(checkers, api.NewProviderHealthChecker(providerConfig.BaseURL, providerConfig.APIKey))
	}
	apiRouter.SetHealthCheckers(checkers...)

	// Configure JWT middleware if secret is provided
	if jwtSecret := os.Getenv("JWT_SECRET"); jwtSecret != "" {
		slog.Info("jwt authentication enabled")
		strict := os.Getenv("JWT_STRICT") == "true"
		mw := middleware.JWTMiddleware(middleware.JWTMiddlewareConfig{
			SecretKey: []byte(jwtSecret),
			Strict:    strict,
		})
		apiRouter.SetAuthMiddleware(mw)
	} else {
		slog.Warn("no JWT_SECRET provided, authentication is disabled")
	}

	mux.Handle("/health", apiRouter)
	mux.Handle("/healthz", apiRouter)
	mux.Handle("/readyz", apiRouter)
	mux.Handle("/metrics", apiRouter)
	mux.Handle("/version", apiRouter)
	mux.Handle("/v1/", apiRouter)

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
