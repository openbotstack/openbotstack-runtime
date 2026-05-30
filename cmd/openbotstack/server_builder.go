package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
	coretelemetry "github.com/openbotstack/openbotstack-core/telemetry"
	plannerpkg "github.com/openbotstack/openbotstack-core/planner"
	agentpkg "github.com/openbotstack/openbotstack-runtime/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	"github.com/openbotstack/openbotstack-runtime/config"
	contextassembler "github.com/openbotstack/openbotstack-runtime/context"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	harnesspkg "github.com/openbotstack/openbotstack-runtime/harness"
	reasoningpkg "github.com/openbotstack/openbotstack-runtime/harness/reasoning"
	"github.com/openbotstack/openbotstack-runtime/internal/adapters"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	mcppkg "github.com/openbotstack/openbotstack-runtime/mcp"
	"github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	runtimetelemetry "github.com/openbotstack/openbotstack-runtime/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
	"github.com/openbotstack/openbotstack-runtime/toolrunner/tool_invocation"
)

// ServerBuilder encapsulates phased initialization of the OpenBotStack runtime.
// Each Init* method stores outputs for later phases. Call Build() to produce ServerDeps.
type ServerBuilder struct {
	cfg         *config.Config
	pdb         *persistence.DB
	otelCleanup func()

	// Phase outputs
	modelRouter     *router.DefaultRouter
	providerFactory *providers.ProviderFactory
	providerName    string
	providerConfig  config.LLMProviderConfig
	hostFuncs       *wasm.HostFunctions
	exec            *executor.DefaultExecutor
	dualPlanner     *plannerpkg.LLMPlanner
	registryClient  *toolrunner.RegistryClient
	capRegistry     capability.CapabilityRegistry
	mcpManager      *mcppkg.MCPManager
	mcpRunner       toolrunner.ToolRunner
	builtinRunner   *builtintools.BuiltinToolRunner
	apiAgent        agent.Agent
	reasoningStore  *reasoningpkg.InMemoryStore
	hookMgr         *harnesspkg.HookManager
	markdownStore   *memory.MarkdownMemoryStore
	sessionStore    memory.SessionStateStore
	telemetry       *api.TelemetryHandler
	auditLogger     *audit.SQLiteAuditLogger
	skillWatcher    *SkillWatcher
}

func NewServerBuilder() *ServerBuilder { return &ServerBuilder{} }

// Config returns the loaded configuration.
func (b *ServerBuilder) Config() *config.Config { return b.cfg }

// Cleanup runs deferred teardown for resources acquired during initialization.
func (b *ServerBuilder) Cleanup() {
	if b.otelCleanup != nil {
		b.otelCleanup()
	}
	if b.pdb != nil {
		_ = b.pdb.Close()
	}
	if b.skillWatcher != nil {
		b.skillWatcher.Stop()
	}
	if b.mcpManager != nil {
		b.mcpManager.Shutdown()
	}
}

// InitInfrastructure loads config, sets up logging, OpenTelemetry, and SQLite.
func (b *ServerBuilder) InitInfrastructure() *ServerBuilder {
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal("failed to load config", "error", err)
	}

	logLevel := parseLogLevel(cfg.Observability.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	otelCleanup, err := observability.Setup(context.Background(), cfg.Observability, "dev")
	if err != nil {
		slog.Error("failed to initialize OpenTelemetry", "error", err)
		os.Exit(1)
	}
	if err := observability.InitMetrics(); err != nil {
		slog.Error("failed to initialize OTel metrics", "error", err)
		os.Exit(1)
	}
	if err := observability.InitAppMetrics(); err != nil {
		slog.Error("failed to initialize app metrics", "error", err)
		os.Exit(1)
	}

	if *listenAddr != ":8080" {
		cfg.Server.Addr = *listenAddr
	}

	slog.Info("starting openbotstack",
		"addr", cfg.Server.Addr,
		"mode", *runMode,
		"llm_provider", cfg.Providers.LLM.Default,
	)

	dbPath := os.Getenv("OBS_DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data/openbotstack.db"
	}
	pdb, err := persistence.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	if err := pdb.Migrate(); err != nil {
		slog.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateSignatureColumn(); err != nil {
		slog.Error("failed to migrate signature column", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateStepContextColumns(); err != nil {
		slog.Error("failed to migrate step context columns", "error", err)
		os.Exit(1)
	}
	if err := pdb.MigrateAPIKeyRoleColumn(); err != nil {
		slog.Error("failed to migrate api key role column", "error", err)
		os.Exit(1)
	}
	slog.Info("sqlite database initialized", "path", dbPath)

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

	b.cfg = cfg
	b.pdb = pdb
	b.otelCleanup = otelCleanup
	return b
}

// InitAI creates the model router and registers the configured provider.
func (b *ServerBuilder) InitAI() *ServerBuilder {
	modelRouter := router.NewDefaultRouter()
	providerName := b.cfg.Providers.LLM.Default
	var providerConfig config.LLMProviderConfig

	switch providerName {
	case "modelscope":
		providerConfig = b.cfg.Providers.LLM.ModelScope
	case "claude":
		providerConfig = b.cfg.Providers.LLM.Claude
	default:
		providerConfig = b.cfg.Providers.LLM.OpenAI
	}

	providerFactory := providers.NewProviderFactory()

	if providerConfig.APIKey != "" {
		llmProvider := providerFactory.Create(providerName, providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		if err := modelRouter.Register(llmProvider); err != nil {
			slog.Error("failed to register provider", "error", err)
		} else {
			slog.Info("llm provider registered", "provider", providerName, "model", providerConfig.Model, "base_url", providerConfig.BaseURL)
		}
	} else {
		slog.Warn("LLM API key not set, LLM features will be disabled")
	}

	seedProviderConfig(b.pdb, providerName, providerConfig, true)

	b.modelRouter = modelRouter
	b.providerFactory = providerFactory
	b.providerName = providerName
	b.providerConfig = providerConfig
	return b
}

// InitExecution creates Wasm runtime, executor, host functions, and planner.
func (b *ServerBuilder) InitExecution() *ServerBuilder {
	wasmRuntime, err := wasm.NewRuntime()
	if err != nil {
		slog.Error("failed to initialize wasm runtime", "error", err)
		os.Exit(1)
	}

	hostFuncs := &wasm.HostFunctions{
		LLMGenerate: func(ctx context.Context, prompt string) (string, error) {
			mReq := skills.GenerateRequest{
				Messages: []skills.Message{{Role: "user", Content: prompt}},
			}
			prov, err := b.modelRouter.Route([]skills.CapabilityType{skills.CapTextGeneration}, skills.ModelConstraints{})
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

	exec := executor.NewDefaultExecutorWithRuntime(wasmRuntime, nil)
	exec.SetTextGenerator(&adapters.LLMTextGenerator{Router: b.modelRouter})

	if err := wasmRuntime.RegisterHostFunctions(context.Background(), hostFuncs); err != nil {
		slog.Error("failed to register host functions", "error", err)
		os.Exit(1)
	}

	skillsPath := os.Getenv("OBS_SKILLS_PATH")
	if skillsPath == "" {
		skillsPath = "./skills"
	}
	if err := loadSkills(context.Background(), exec, skillsPath); err != nil {
		slog.Error("failed to load skills", "error", err)
	}

	plannerLimits := &plannerpkg.ExecutionLimits{
		MaxSteps:         10,
		MaxToolCalls:     15,
		MaxExecutionTime: 300 * time.Second,
	}
	dualPlanner := plannerpkg.NewLLMPlanner(b.modelRouter, plannerLimits)
	slog.Info("execution planner initialized with LLM router")

	b.hostFuncs = hostFuncs
	b.exec = exec
	b.dualPlanner = dualPlanner
	b.registryClient = toolrunner.NewRegistryClient(b.cfg.Sandbox.ToolRegistryURL)

	// Start skill watcher for hot-reload
	skillWatcher := NewSkillWatcher(exec, skillsPath)
	if err := skillWatcher.Start(context.Background()); err != nil {
		slog.Warn("skill watcher failed to start, hot-reload disabled", "error", err)
		skillWatcher = nil
	}
	b.skillWatcher = skillWatcher

	return b
}

// InitCapabilities creates the CapabilityRegistry, registers skills and builtins,
// then sets up MCPManager and MCPToolRunner.
func (b *ServerBuilder) InitCapabilities() *ServerBuilder {
	capRegistry := capability.NewMemoryCapabilityRegistry()
	ctx := context.Background()

	registrar := NewCapabilityRegistrar(capRegistry, b.exec)
	builtinRunner := registrar.RegisterAll(ctx)

	mcpStore := mcppkg.NewSQLiteMCPStore(b.pdb.DB)
	mcpManager := mcppkg.NewMCPManager(mcpStore, capRegistry)

	for _, srv := range b.cfg.MCP.ToCoreServers() {
		existing, _ := mcpStore.Get(ctx, srv.ID)
		if existing != nil {
			continue
		}
		if err := mcpStore.Create(ctx, srv); err != nil {
			slog.Warn("MCP: failed to seed server from config", "id", srv.ID, "error", err)
		} else {
			slog.Info("MCP: seeded server from config", "id", srv.ID, "name", srv.Name)
		}
	}

	if len(b.cfg.MCP.Servers) > 0 {
		slog.Info("MCP: starting with configured servers", "count", len(b.cfg.MCP.Servers))
	} else {
		slog.Info("MCP: no servers configured, manager available for admin API")
	}

	if err := mcpManager.Start(ctx); err != nil {
		slog.Warn("MCP: failed to start some servers", "error", err)
	}

	mcpRunner := mcppkg.NewMCPToolRunner(mcpManager)
	slog.Info("MCP: initialized", "configured_servers", len(b.cfg.MCP.Servers))

	b.capRegistry = capRegistry
	b.mcpManager = mcpManager
	b.mcpRunner = mcpRunner
	b.builtinRunner = builtinRunner
	return b
}

// InitAgent creates either a HarnessAgent or DefaultAgent based on config.
func (b *ServerBuilder) InitAgent() *ServerBuilder {
	art := &assistant.AssistantRuntime{AssistantID: "default"}

	var apiAgent agent.Agent
	var reasoningStore *reasoningpkg.InMemoryStore
	var hookMgr *harnesspkg.HookManager

	{
		harnessCfg := harnesspkg.DefaultHarnessConfig()
		harnessCfg.MaxSteps = b.cfg.Agent.DualLoop.MaxSteps
		harnessCfg.MaxSessionRuntime = b.cfg.Agent.DualLoop.MaxSessionRuntime

		toolRunner := toolrunner.NewRegistryToolRunner(b.registryClient)

		stepExec := harnesspkg.NewStepExecutor(toolRunner, b.exec, harnesspkg.StepExecutorDeps{
			MCPRunner:     b.mcpRunner,
			BuiltinRunner: b.builtinRunner,
		})

		reasoningCfg := harnesspkg.DefaultReasoningLoopConfig()
		reasoningCfg.MaxTurns = b.cfg.Agent.DualLoop.MaxTurns
		reasoningCfg.MaxToolCalls = b.cfg.Agent.DualLoop.MaxToolCalls
		reasoningCfg.MaxTurnRuntime = b.cfg.Agent.DualLoop.MaxTurnRuntime
		reasoningCfg.RepeatPlanStop = true

		compactionStrategy := harnesspkg.NewThresholdCompactionStrategy(
			harnesspkg.DefaultCompactionTrigger(),
			b.cfg.Agent.DualLoop.MaxRetainedTurns,
		)
		compactor := harnesspkg.NewContextCompactorAdapter(compactionStrategy)

		reasoningLoop := harnesspkg.NewDefaultReasoningLoop(reasoningCfg, b.dualPlanner, stepExec, compactor)

		hookMgr = harnesspkg.NewHookManager()

		h := harnesspkg.NewExecutionHarness(harnessCfg, toolRunner, b.exec, harnesspkg.HarnessDeps{
			ReasoningLoop: reasoningLoop,
			HookManager:   hookMgr,
			LLMGenerator:  b.buildLLMGenerator(),
			AuditLogger:   b.auditLogger,
			MCPRunner:     b.mcpRunner,
			BuiltinRunner: b.builtinRunner,
		})

		reasoningStore = reasoningpkg.NewInMemoryStore()

		apiAgent = agentpkg.NewHarnessAgent(agentpkg.HarnessAgentConfig{
			Planner:            b.dualPlanner,
			Registry:           b.exec,
			Runtime:            art,
			Harness:            h,
			WorkflowResolver:   agentpkg.NewKeywordWorkflowResolver(),
			ReasoningStore:     reasoningStore,
			CapRegistry:        b.capRegistry,
			GrantedPermissions: b.builtinRunner.AllPermissions(),
		})
		slog.Info("harness agent initialized",
			"max_steps", harnessCfg.MaxSteps,
			"max_session_runtime", harnessCfg.MaxSessionRuntime,
			"reasoning_max_turns", reasoningCfg.MaxTurns,
			"reasoning_max_tool_calls", reasoningCfg.MaxToolCalls,
		)
	}


	b.apiAgent = apiAgent
	b.reasoningStore = reasoningStore
	b.hookMgr = hookMgr
	return b
}

// InitMemory sets up Markdown store, conversation store, memory bridge, and context assembler.
func (b *ServerBuilder) InitMemory() *ServerBuilder {
	sessionStateStore := memory.NewSQLiteSessionStateStore(b.pdb.DB,
		memory.WithStrictTenant(os.Getenv("OBS_AUTH_STRICT") == "true"),
	)

	markdownStore, err := memory.NewMarkdownMemoryStore(b.cfg.Memory.DataDir)
	if err != nil {
		slog.Error("failed to create markdown memory store", "error", err)
		os.Exit(1)
	}
	slog.Info("markdown memory store initialized", "data_dir", b.cfg.Memory.DataDir)

	var convStore agent.ConversationStore = markdownStore
	if b.cfg.Memory.SummaryEnabled {
		summarizer := memory.NewConversationSummarizer(markdownStore, b.modelRouter, b.cfg.Memory.SummaryThreshold)
		convStore = memory.NewSummarizingConversationStore(markdownStore, summarizer)
		slog.Info("conversation summarization enabled", "threshold", b.cfg.Memory.SummaryThreshold)
	}
	convStore = memory.NewDualWriteConversationStore(convStore, sessionStateStore)

	switch a := b.apiAgent.(type) {
	case *agentpkg.HarnessAgent:
		a.SetConversationStore(convStore)
		a.SetMaxHistoryMessages(b.cfg.Memory.MaxHistoryMessages)
	}

	memoryBridge := memory.NewMarkdownMemoryBridge(markdownStore, nil)

	if b.cfg.Vector.Enabled && b.cfg.Vector.DatabaseURL != "" {
		b.initVectorSearch(markdownStore, memoryBridge, convStore)
	} else {
		slog.Info("vector search disabled (keyword matching only)")
	}

	contextAssembler := contextassembler.NewRuntimeContextAssembler(b.exec, memoryBridge)
	switch a := b.apiAgent.(type) {
	case *agentpkg.HarnessAgent:
		a.SetContextAssembler(contextAssembler)
		a.SetMemoryManager(memoryBridge)
	}
	slog.Info("context assembler initialized")

	b.markdownStore = markdownStore
	b.sessionStore = sessionStateStore
	return b
}

// InitTelemetry creates telemetry stores and wires instrumentor into harness hooks.
func (b *ServerBuilder) InitTelemetry() *ServerBuilder {
	spanStore := store.NewRingBufferSpanStore(1000)
	eventStore := store.NewRingBufferEventStore(500)
	meter := coretelemetry.NewMemoryMeter()
	telemetryInstrumentor := runtimetelemetry.NewInstrumentor(spanStore, eventStore, meter)
	telemetryHandler := api.NewTelemetryHandler(spanStore, eventStore, meter, telemetryInstrumentor)

	if b.hookMgr != nil {
		telemetryInstrumentor.RegisterHooks(b.hookMgr)
		slog.Info("telemetry instrumentor registered on harness")
	}

	httpAllowlistObj := wasm.NewHTTPAllowlist(b.cfg.Sandbox.HTTPAllowlist)
	sandboxedClient := wasm.NewSandboxedHTTPClientWithSSRF(httpAllowlistObj, nil)
	toolPipeline := tool_invocation.NewToolInvocationPipeline(sandboxedClient, b.registryClient, 30*time.Second)
	tool_invocation.WireHTTPFetch(b.hostFuncs, toolPipeline)
	slog.Info("tool invocation pipeline wired",
		"allowlist", b.cfg.Sandbox.HTTPAllowlist,
		"registry_url", b.cfg.Sandbox.ToolRegistryURL,
	)

	b.telemetry = telemetryHandler
	return b
}

// InitAudit creates the audit logger and wires it into the executor.
func (b *ServerBuilder) InitAudit() *ServerBuilder {
	auditLogger := audit.NewSQLiteAuditLogger(b.pdb.DB)
	slog.Info("sqlite audit logger initialized")
	b.exec.SetAuditLogger(auditLogger)
	slog.Info("audit logger wired to executor")
	b.auditLogger = auditLogger
	return b
}

// Build assembles all phase outputs into ServerDeps.
func (b *ServerBuilder) Build() ServerDeps {
	// Compliance & governance
	complianceSigningKey := os.Getenv("JWT_SECRET")
	if len(complianceSigningKey) >= 32 {
		chainSigner := rtAudit.NewHMACChainSigner([]byte(complianceSigningKey))
		b.auditLogger.SetSigner(chainSigner)
		slog.Info("audit chain signing enabled")
	}
	complianceGen := rtAudit.NewComplianceReportGenerator(b.auditLogger, []byte(complianceSigningKey))
	auditPurger := &adapters.AuditPurger{PurgerFunc: func(cutoff time.Time, tenantID string) (int64, error) {
		return b.auditLogger.PurgeBefore(context.Background(), cutoff, tenantID)
	}}
	retentionPolicy := rtAudit.NewRetentionPolicy(rtAudit.DefaultRetentionConfig(), auditPurger)
	retentionMgr := &adapters.RetentionManagerAdapter{Policy: retentionPolicy}
	approvalStore := harnesspkg.NewInMemoryApprovalStore(30 * time.Minute)
	slog.Info("compliance modules initialized", "retention_enabled", retentionPolicy.Config().Enabled)

	rateLimiter := ratelimit.NewSQLiteRateLimiter(b.pdb.DB, ratelimit.NewSQLiteQuotaStore(b.pdb.DB))

	var mcpAdminIfc api.MCPAdmin
	if b.mcpManager != nil {
		mcpAdminIfc = b.mcpManager
	}

	return ServerDeps{
		Agent:               b.apiAgent,
		Exec:                b.exec,
		ModelRouter:         b.modelRouter,
		ProviderFactory:     b.providerFactory,
		DB:                  b.pdb,
		MarkdownStore:       b.markdownStore,
		SessionStore:        b.sessionStore,
		RateLimiter:         rateLimiter,
		AuditLogger:         b.auditLogger,
		ComplianceGenerator: complianceGen,
		RetentionManager:    retentionMgr,
		ApprovalGateway:     approvalStore,
		ReasoningStore:      b.reasoningStore,
		Telemetry:           b.telemetry,
		MCPAdmin:            mcpAdminIfc,
		SkillWatcher:        b.skillWatcher,
		CapRegistry:         b.capRegistry,
	}
}

// SkillAdmin returns a SkillAdminAdapter for the admin API.
func (b *ServerBuilder) SkillAdmin() *adapters.SkillAdminAdapter {
	return adapters.NewSkillAdminAdapter(b.exec)
}

// initVectorSearch initializes optional PostgreSQL + pgvector for semantic search.
func (b *ServerBuilder) initVectorSearch(markdownStore *memory.MarkdownMemoryStore, memoryBridge *memory.MarkdownMemoryBridge, convStore agent.ConversationStore) {
	pgPool, err := pgxpool.New(context.Background(), b.cfg.Vector.DatabaseURL)
	if err != nil {
		slog.Error("failed to parse vector database URL", "error", err)
		os.Exit(1)
	}
	if err := pgPool.Ping(context.Background()); err != nil {
		slog.Error("failed to connect to vector database", "error", err)
		pgPool.Close()
		os.Exit(1)
	}

	vectorStore := memory.NewPgVectorStore(pgPool, b.cfg.Vector.Dimensions)
	if err := vectorStore.Migrate(context.Background()); err != nil {
		slog.Error("failed to migrate vector store", "error", err)
		os.Exit(1)
	}

	embeddingSvc := memory.NewEmbeddingService(b.modelRouter, b.cfg.Vector.Model, b.cfg.Vector.Dimensions)
	memoryBridge.SetRetrievalStrategy(memory.NewVectorFirstStrategy(markdownStore, vectorStore, embeddingSvc))

	indexer := memory.NewAsyncEmbeddingIndexer(embeddingSvc, vectorStore)
	if summarizingStore, ok := convStore.(*memory.SummarizingConversationStore); ok {
		summarizingStore.SetIndexer(indexer)
	}
	slog.Info("vector search enabled", "model", b.cfg.Vector.Model, "dimensions", b.cfg.Vector.Dimensions)
}

// parseLogLevel converts a log level string to slog.Level.
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

// buildLLMGenerator creates a function that generates direct LLM text responses.
func (b *ServerBuilder) buildLLMGenerator() harnesspkg.LLMGenerator {
	return func(ctx context.Context, systemPrompt, userMessage string) (string, error) {
		provider, err := b.modelRouter.Route([]skills.CapabilityType{skills.CapTextGeneration}, skills.ModelConstraints{})
		if err != nil {
			return "", fmt.Errorf("llm generator: route failed: %w", err)
		}

		msgs := []skills.Message{}
		if systemPrompt != "" {
			msgs = append(msgs, skills.Message{Role: "system", Content: systemPrompt})
		}
		msgs = append(msgs, skills.Message{Role: "user", Content: userMessage})

		resp, err := provider.Generate(ctx, skills.GenerateRequest{
			Messages:  msgs,
			MaxTokens: 4096,
		})
		if err != nil {
			return "", fmt.Errorf("llm generator: generate failed: %w", err)
		}
		return resp.Content, nil
	}
}
