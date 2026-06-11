package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/capability"
	mcppkg "github.com/openbotstack/openbotstack-runtime/mcp"
)

// InitCapabilities creates the CapabilityRegistry, registers skills and builtins,
// then sets up MCPManager and MCPToolRunner.
func (b *ServerBuilder) InitCapabilities() *ServerBuilder {
	b.requireInit("exec", "InitCapabilities")
	capRegistry := capability.NewMemoryCapabilityRegistry()
	ctx := context.Background()

	registrar := NewCapabilityRegistrar(capRegistry, b.exec)
	builtinRunner := registrar.RegisterAll(ctx)

	// Inject LLM access for vision_analyze and future LLMAwareTools.
	if b.modelRouter != nil {
		llmAccess := NewRuntimeLLMAccess(b.modelRouter, 2048, 60*time.Second)
		builtinRunner.SetLLMAccess(llmAccess)
		slog.Info("builtin tools: LLM access injected for vision-capable tools")
	}

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
