package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/capability"
	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-runtime/mcp/jsonrpc"
)

// MCPManager manages the lifecycle of MCP server connections.
type MCPManager struct {
	mu               sync.RWMutex
	servers          map[string]*managedServer
	connecting       map[string]struct{} // guards against concurrent connectServer for same ID
	store            *SQLiteMCPStore
	registry         *capability.MemoryCapabilityRegistry
	createClientFunc func(cfg mcpcore.ServerConfig) (mcpcore.Client, error)
}

type managedServer struct {
	config mcpcore.ServerConfig
	client mcpcore.Client
	tools  []mcpcore.ClientTool
	status string // "connected" | "disconnected" | "error"
	err    string
}

// NewMCPManager creates a new MCP manager.
func NewMCPManager(store *SQLiteMCPStore, registry *capability.MemoryCapabilityRegistry) *MCPManager {
	m := &MCPManager{
		servers:    make(map[string]*managedServer),
		connecting: make(map[string]struct{}),
		store:      store,
		registry:   registry,
	}
	m.createClientFunc = m.defaultCreateClient
	return m
}

// Start loads server configs from DB and connects all enabled servers.
func (m *MCPManager) Start(ctx context.Context) error {
	configs, err := m.store.List(ctx)
	if err != nil {
		return fmt.Errorf("load mcp servers: %w", err)
	}
	for _, cfg := range configs {
		if !cfg.Enabled {
			m.mu.Lock()
			m.servers[cfg.ID] = &managedServer{config: cfg, status: "disconnected"}
			m.mu.Unlock()
			continue
		}
		if err := m.connectServer(ctx, cfg); err != nil {
			slog.Warn("mcp: failed to connect server", "id", cfg.ID, "error", err)
		}
	}
	return nil
}

// Shutdown disconnects all servers.
func (m *MCPManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, srv := range m.servers {
		if srv.client != nil {
			if err := srv.client.Close(); err != nil {
				slog.Warn("mcp: error closing server", "id", id, "error", err)
			}
		}
		for _, tool := range srv.tools {
			toolID := fmt.Sprintf("mcp.%s.%s", id, tool.Name)
			if err := m.registry.Unregister(context.Background(), toolID); err != nil {
				slog.Warn("mcp: failed to unregister tool during shutdown", "toolID", toolID, "error", err)
			}
		}
	}
	m.servers = make(map[string]*managedServer)
}

// AddServer persists a new server config and connects to it.
func (m *MCPManager) AddServer(ctx context.Context, cfg mcpcore.ServerConfig) error {
	if err := m.store.Create(ctx, cfg); err != nil {
		return err
	}
	if cfg.Enabled {
		return m.connectServer(ctx, cfg)
	}
	m.mu.Lock()
	m.servers[cfg.ID] = &managedServer{config: cfg, status: "disconnected"}
	m.mu.Unlock()
	return nil
}

// RemoveServer disconnects and removes a server.
func (m *MCPManager) RemoveServer(ctx context.Context, serverID string) error {
	// Delete from DB first; if this fails, in-memory state is untouched.
	if err := m.store.Delete(ctx, serverID); err != nil {
		return err
	}
	m.disconnectServer(ctx, serverID)
	return nil
}

// disconnectServer cleans up in-memory state for a server.
func (m *MCPManager) disconnectServer(ctx context.Context, serverID string) {
	m.mu.Lock()
	srv, ok := m.servers[serverID]
	if ok {
		if srv.client != nil {
			_ = srv.client.Close()
		}
		for _, tool := range srv.tools {
			toolID := fmt.Sprintf("mcp.%s.%s", serverID, tool.Name)
			if err := m.registry.Unregister(ctx, toolID); err != nil {
				slog.WarnContext(ctx, "mcp: failed to unregister tool during disconnect", "toolID", toolID, "error", err)
			}
		}
		delete(m.servers, serverID)
	}
	m.mu.Unlock()
}

// UpdateServer updates a server's config and reconnects if needed.
func (m *MCPManager) UpdateServer(ctx context.Context, serverID string, cfg mcpcore.ServerConfig) error {
	if err := m.store.Update(ctx, cfg); err != nil {
		return err
	}
	m.disconnectServer(ctx, serverID)
	if cfg.Enabled {
		return m.connectServer(ctx, cfg)
	}
	m.mu.Lock()
	m.servers[serverID] = &managedServer{config: cfg, status: "disconnected"}
	m.mu.Unlock()
	return nil
}

// ListServers returns the status of all known servers.
func (m *MCPManager) ListServers() []mcpcore.ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]mcpcore.ServerStatus, 0, len(m.servers))
	for id, srv := range m.servers {
		statuses = append(statuses, mcpcore.ServerStatus{
			ID:        id,
			Name:      srv.config.Name,
			Transport: srv.config.Transport,
			Status:    srv.status,
			ToolCount: len(srv.tools),
			Error:     sanitizeError(srv.err),
		})
	}
	return statuses
}

// GetServerTools returns the tools available on a specific server.
func (m *MCPManager) GetServerTools(_ context.Context, serverID string) ([]mcpcore.ClientTool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	srv, ok := m.servers[serverID]
	if !ok {
		return nil, fmt.Errorf("server %q not found", serverID)
	}
	tools := make([]mcpcore.ClientTool, len(srv.tools))
	copy(tools, srv.tools)
	return tools, nil
}

// CallTool invokes a tool on a specific server.
func (m *MCPManager) CallTool(ctx context.Context, serverID, toolName string, args map[string]any) (*mcpcore.CallToolResult, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverID]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("server %q not found", serverID)
	}
	client := srv.client
	m.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("server %q is not connected", serverID)
	}
	return client.CallTool(ctx, toolName, args)
}

// ReconnectServer forces a reconnection attempt.
// It disconnects the old client first to prevent resource leaks and duplicate registrations.
func (m *MCPManager) ReconnectServer(ctx context.Context, serverID string) error {
	m.mu.RLock()
	srv, ok := m.servers[serverID]
	if !ok {
		m.mu.RUnlock()
		return fmt.Errorf("server %q not found", serverID)
	}
	cfg := srv.config
	m.mu.RUnlock()

	// Disconnect old client before reconnecting
	m.disconnectServer(ctx, serverID)
	return m.connectServer(ctx, cfg)
}

// ToolDescriptors returns all MCP tools as SkillDescriptors.
func (m *MCPManager) ToolDescriptors() []aitypes.SkillDescriptor {
	return m.registry.ListByKind(capability.CapabilityKindMCP)
}

func (m *MCPManager) connectServer(ctx context.Context, cfg mcpcore.ServerConfig) error {
	// Guard against concurrent connections for the same server ID.
	m.mu.Lock()
	if _, dup := m.connecting[cfg.ID]; dup {
		m.mu.Unlock()
		return fmt.Errorf("connection already in progress for %q", cfg.ID)
	}
	m.connecting[cfg.ID] = struct{}{}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connecting, cfg.ID)
		m.mu.Unlock()
	}()

	client, err := m.createClientFunc(cfg)
	if err != nil {
		m.mu.Lock()
		m.servers[cfg.ID] = &managedServer{
			config: cfg,
			status: "error",
			err:    err.Error(),
		}
		m.mu.Unlock()
		return fmt.Errorf("create client for %q: %w", cfg.ID, err)
	}

	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		m.mu.Lock()
		m.servers[cfg.ID] = &managedServer{
			config: cfg,
			status: "error",
			err:    err.Error(),
		}
		m.mu.Unlock()
		return fmt.Errorf("initialize %q: %w", cfg.ID, err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		m.mu.Lock()
		m.servers[cfg.ID] = &managedServer{
			config: cfg,
			status: "error",
			err:    err.Error(),
		}
		m.mu.Unlock()
		return fmt.Errorf("list tools from %q: %w", cfg.ID, err)
	}

	// Register tools into capability registry
	for _, tool := range tools {
		toolID := fmt.Sprintf("mcp.%s.%s", cfg.ID, tool.Name)
		// Unregister first in case of reconnection (idempotent)
		if err := m.registry.Unregister(ctx, toolID); err != nil {
			slog.WarnContext(ctx, "mcp: failed to unregister tool during reconnect", "toolID", toolID, "error", err)
		}
		adapter := capability.NewFromMCP(cfg.ID, tool)
		if err := m.registry.Register(ctx, adapter); err != nil {
			slog.Warn("mcp: failed to register tool", "tool", tool.Name, "server", cfg.ID, "error", err)
		}
	}

	m.mu.Lock()
	m.servers[cfg.ID] = &managedServer{
		config: cfg,
		client: client,
		tools:  tools,
		status: "connected",
	}
	m.mu.Unlock()

	slog.Info("mcp: connected server", "id", cfg.ID, "tools", len(tools))
	return nil
}

func (m *MCPManager) defaultCreateClient(cfg mcpcore.ServerConfig) (mcpcore.Client, error) {
	switch cfg.Transport {
	case "stdio":
		env := mergeEnv(cfg.Env, cfg.Auth)
		transport, err := jsonrpc.NewStdioTransport(cfg.Command, cfg.Args, env)
		if err != nil {
			return nil, err
		}
		return jsonrpc.NewClient(transport), nil
	case "sse":
		headers := authHeaders(cfg.Auth)
		transport := jsonrpc.NewSSETransport(cfg.URL, headers)
		return jsonrpc.NewClient(transport), nil
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

// mergeEnv combines base env vars with auth env vars.
func mergeEnv(base map[string]string, auth *mcpcore.ServerAuth) map[string]string {
	env := make(map[string]string)
	for k, v := range base {
		env[k] = v
	}
	if auth != nil {
		for k, v := range auth.EnvVars() {
			env[k] = v
		}
	}
	return env
}

// authHeaders extracts HTTP headers from auth config.
func authHeaders(auth *mcpcore.ServerAuth) map[string]string {
	if auth == nil {
		return nil
	}
	headers := auth.HTTPHeaders()
	if headers == nil && auth.Type != "" && auth.Type != "none" {
		slog.Warn("mcp: unrecognized auth type, no headers applied", "type", auth.Type)
	}
	return headers
}

// sanitizeError truncates and sanitizes error messages to avoid leaking
// sensitive data (tokens, full response bodies) through the admin API.
func sanitizeError(err string) string {
	if len(err) > 200 {
		err = err[:200] + "..."
	}
	// Strip common token patterns
	err = strings.ReplaceAll(err, "Bearer ", "Bearer ***")
	return err
}
