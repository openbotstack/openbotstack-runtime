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

	"github.com/openbotstack/openbotstack-runtime/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/audit"
	"github.com/openbotstack/openbotstack-runtime/config"
	"github.com/openbotstack/openbotstack-runtime/executor"
	"github.com/openbotstack/openbotstack-runtime/llm"
	"github.com/openbotstack/openbotstack-runtime/wasm"
	"github.com/openbotstack/openbotstack-runtime/web/webui"
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

	// Initialize Host API (LLM)
	var llmClient *llm.Client

	// Determine provider
	providerName := cfg.Providers.LLM.Default
	var providerConfig config.LLMProviderConfig

	if providerName == "modelscope" {
		providerConfig = cfg.Providers.LLM.ModelScope
	} else {
		providerConfig = cfg.Providers.LLM.OpenAI
	}

	if providerConfig.APIKey != "" {
		llmClient = llm.NewClient(providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		slog.Info("llm client initialized", "provider", providerName, "model", providerConfig.Model)
	} else {
		slog.Warn("LLM API key not set, LLM features will be disabled")
	}

	hostFuncs := &wasm.HostFunctions{
		LLMGenerate: func(ctx context.Context, prompt string) (string, error) {
			if llmClient == nil {
				return "LLM not configured", nil
			}
			return llmClient.Generate(ctx, prompt)
		},
		Log: func(ctx context.Context, level, msg string) {
			slog.Info("wasm log", "level", level, "msg", msg)
		},
	}

	// Initialize Executor
	exec := executor.NewDefaultExecutorWithRuntime(wasmRuntime)

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
	if llmClient == nil {
		slog.Error("LLM client not configured. Check config.yaml or OBS_LLM_API_KEY")
		os.Exit(1)
	}
	planner := agent.NewLLMPlanner(llmClient)
	slog.Info("planner initialized with LLM")

	// Create Agent (orchestrates Planner + Executor)
	apiAgent := agent.NewDefaultAgent(planner, exec, exec)
	slog.Info("agent initialized", "loaded_skills", len(exec.List()))

	// Initialize Audit Logger
	auditLogger := audit.NewPGAuditLogger()

	// Create combined router
	mux := http.NewServeMux()

	// API routes
	apiRouter := api.NewRouter(apiAgent)
	apiRouter.SetSkillProvider(exec)
	apiRouter.SetExecutionStore(api.NewAuditExecutionStore(auditLogger))
	mux.Handle("/health", apiRouter)
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

	// Create server
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
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
