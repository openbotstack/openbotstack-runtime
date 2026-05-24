package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/openbotstack/openbotstack-core/capability"
	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// mockMCPClient is a test double for mcpcore.Client.
type mockMCPClient struct {
	tools     []mcpcore.ClientTool
	callFunc  func(toolName string, args map[string]any) (*mcpcore.CallToolResult, error)
	closed    bool
	initCalls int
}

func (m *mockMCPClient) Initialize(_ context.Context) error {
	m.initCalls++
	return nil
}

func (m *mockMCPClient) ListTools(_ context.Context) ([]mcpcore.ClientTool, error) {
	return m.tools, nil
}

func (m *mockMCPClient) CallTool(ctx context.Context, toolName string, args map[string]any) (*mcpcore.CallToolResult, error) {
	if m.callFunc != nil {
		return m.callFunc(toolName, args)
	}
	return &mcpcore.CallToolResult{
		Content: []mcpcore.ContentBlock{{Type: "text", Text: "mock result"}},
	}, nil
}

func (m *mockMCPClient) Close() error {
	m.closed = true
	return nil
}

func setupTestManager(t *testing.T) (*MCPManager, *persistence.DB) {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewSQLiteMCPStore(db.DB)
	registry := capability.NewMemoryCapabilityRegistry()
	manager := NewMCPManager(store, registry)

	return manager, db
}

// testManagerWithMock creates a manager that uses mock clients.
func testManagerWithMock(t *testing.T) (*MCPManager, func(mcpcore.ServerConfig) mcpcore.Client) {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewSQLiteMCPStore(db.DB)
	registry := capability.NewMemoryCapabilityRegistry()
	manager := NewMCPManager(store, registry)

	var mu sync.Mutex
	mockClients := make(map[string]*mockMCPClient)
	manager.createClientFunc = func(cfg mcpcore.ServerConfig) (mcpcore.Client, error) {
		mc := &mockMCPClient{
			tools: []mcpcore.ClientTool{
				{Name: "tool1", Description: "Test tool 1"},
				{Name: "tool2", Description: "Test tool 2"},
			},
		}
		mu.Lock()
		mockClients[cfg.ID] = mc
		mu.Unlock()
		return mc, nil
	}

	return manager, func(cfg mcpcore.ServerConfig) mcpcore.Client {
		mu.Lock()
		defer mu.Unlock()
		return mockClients[cfg.ID]
	}
}

func TestMCPManager_AddRemoveServer(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Test", Transport: "stdio",
		Command: "echo", Enabled: true,
	}

	if err := mgr.AddServer(ctx, cfg); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	statuses := mgr.ListServers()
	if len(statuses) != 1 {
		t.Fatalf("servers = %d, want 1", len(statuses))
	}
	if statuses[0].Status != "connected" {
		t.Errorf("status = %q", statuses[0].Status)
	}
	if statuses[0].ToolCount != 2 {
		t.Errorf("tool count = %d, want 2", statuses[0].ToolCount)
	}

	// Verify tools registered in capability registry
	descs := mgr.ToolDescriptors()
	if len(descs) != 2 {
		t.Errorf("tool descriptors = %d, want 2", len(descs))
	}

	// Remove
	if err := mgr.RemoveServer(ctx, "srv1"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}
	statuses = mgr.ListServers()
	if len(statuses) != 0 {
		t.Errorf("after remove, servers = %d, want 0", len(statuses))
	}

	// Tools should be unregistered
	descs = mgr.ToolDescriptors()
	if len(descs) != 0 {
		t.Errorf("after remove, descriptors = %d, want 0", len(descs))
	}
}

func TestMCPManager_DisabledServer(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Disabled", Transport: "stdio",
		Command: "echo", Enabled: false,
	}

	if err := mgr.AddServer(ctx, cfg); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	statuses := mgr.ListServers()
	if len(statuses) != 1 {
		t.Fatalf("servers = %d, want 1", len(statuses))
	}
	if statuses[0].Status != "disconnected" {
		t.Errorf("status = %q, want disconnected", statuses[0].Status)
	}
}

func TestMCPManager_CallTool(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Test", Transport: "stdio",
		Command: "echo", Enabled: true,
	}
	mgr.AddServer(ctx, cfg)

	result, err := mgr.CallTool(ctx, "srv1", "tool1", map[string]any{"arg": "val"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.Content[0].Text != "mock result" {
		t.Errorf("result = %q", result.Content[0].Text)
	}
}

func TestMCPManager_CallTool_DisconnectedServer(t *testing.T) {
	db, _ := persistence.Open(":memory:")
	db.Migrate()
	store := NewSQLiteMCPStore(db.DB)
	registry := capability.NewMemoryCapabilityRegistry()
	mgr := NewMCPManager(store, registry)

	ctx := context.Background()
	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Test", Transport: "stdio",
		Command: "nonexistent_command", Enabled: false,
	}
	mgr.AddServer(ctx, cfg)

	_, err := mgr.CallTool(ctx, "srv1", "tool1", nil)
	if err == nil {
		t.Error("expected error for disconnected server")
	}
}

func TestMCPManager_Reconnect(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Test", Transport: "stdio",
		Command: "echo", Enabled: true,
	}
	mgr.AddServer(ctx, cfg)

	// Reconnect should succeed
	if err := mgr.ReconnectServer(ctx, "srv1"); err != nil {
		t.Fatalf("ReconnectServer: %v", err)
	}

	statuses := mgr.ListServers()
	if statuses[0].Status != "connected" {
		t.Errorf("after reconnect, status = %q", statuses[0].Status)
	}
}

func TestMCPManager_Shutdown(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	cfg := mcpcore.ServerConfig{
		ID: "srv1", Name: "Test", Transport: "stdio",
		Command: "echo", Enabled: true,
	}
	mgr.AddServer(ctx, cfg)

	mgr.Shutdown()
	statuses := mgr.ListServers()
	if len(statuses) != 0 {
		t.Errorf("after shutdown, servers = %d, want 0", len(statuses))
	}
}

func TestMCPManager_Concurrent(t *testing.T) {
	mgr, _ := testManagerWithMock(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cfg := mcpcore.ServerConfig{
				ID:        fmt.Sprintf("srv-%d", idx),
				Name:      fmt.Sprintf("Server %d", idx),
				Transport: "stdio",
				Command:   "echo",
				Enabled:   true,
			}
			mgr.AddServer(ctx, cfg)
		}(i)
	}
	wg.Wait()

	statuses := mgr.ListServers()
	if len(statuses) != 10 {
		t.Errorf("servers = %d, want 10", len(statuses))
	}

	descs := mgr.ToolDescriptors()
	if len(descs) != 20 { // 10 servers × 2 tools each
		t.Errorf("descriptors = %d, want 20", len(descs))
	}
}
