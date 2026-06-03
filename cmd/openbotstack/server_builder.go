package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-core/control/agent"
	plannerpkg "github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/api"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
	"github.com/openbotstack/openbotstack-runtime/config"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	harnesspkg "github.com/openbotstack/openbotstack-runtime/harness"
	reasoningpkg "github.com/openbotstack/openbotstack-runtime/harness/reasoning"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	mcppkg "github.com/openbotstack/openbotstack-runtime/mcp"
	"github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
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
	auditPurger := &auditPurgerFunc{fn: func(cutoff time.Time, tenantID string) (int64, error) {
		return b.auditLogger.PurgeBefore(context.Background(), cutoff, tenantID)
	}}
	retentionPolicy := rtAudit.NewRetentionPolicy(rtAudit.DefaultRetentionConfig(), auditPurger)
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
		RetentionPolicy:     retentionPolicy,
		ApprovalGateway:     approvalStore,
		ReasoningStore:      b.reasoningStore,
		Telemetry:           b.telemetry,
		MCPAdmin:            mcpAdminIfc,
		SkillWatcher:        b.skillWatcher,
		CapRegistry:         b.capRegistry,
	}
}

// SkillAdmin returns a SkillAdminService for the admin API.
func (b *ServerBuilder) SkillAdmin() *api.SkillAdminService {
	return api.NewSkillAdminService(b.exec)
}

// auditPurgerFunc adapts a function to the audit.Purger interface.
type auditPurgerFunc struct {
	fn func(cutoff time.Time, tenantID string) (int64, error)
}

func (a *auditPurgerFunc) PurgeBefore(cutoff time.Time, tenantID string) (int64, error) {
	return a.fn(cutoff, tenantID)
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
		provider, err := b.modelRouter.Route([]types.CapabilityType{types.CapTextGeneration}, types.ModelConstraints{})
		if err != nil {
			return "", fmt.Errorf("llm generator: route failed: %w", err)
		}

		msgs := []types.Message{}
		if systemPrompt != "" {
			msgs = append(msgs, types.Message{Role: "system", Content: systemPrompt})
		}
		msgs = append(msgs, types.Message{Role: "user", Content: userMessage})

		resp, err := provider.Generate(ctx, types.GenerateRequest{
			Messages:  msgs,
			MaxTokens: 4096,
		})
		if err != nil {
			return "", fmt.Errorf("llm generator: generate failed: %w", err)
		}
		return resp.Content, nil
	}
}
