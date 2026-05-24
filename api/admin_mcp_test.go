package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/openbotstack/openbotstack-core/access/auth"
	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/mcp"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// mockMCPAdmin implements MCPAdmin for testing.
type mockMCPAdmin struct {
	mu       sync.Mutex
	servers  map[string]mcpcore.ServerConfig
	tools    map[string][]mcpcore.ClientTool
	err      error // if set, all mutating calls return this error
}

func newMockMCPAdmin() *mockMCPAdmin {
	return &mockMCPAdmin{
		servers: make(map[string]mcpcore.ServerConfig),
		tools:   make(map[string][]mcpcore.ClientTool),
	}
}

func (m *mockMCPAdmin) AddServer(_ context.Context, cfg mcpcore.ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.servers[cfg.ID] = cfg
	return nil
}

func (m *mockMCPAdmin) RemoveServer(_ context.Context, serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	delete(m.servers, serverID)
	return nil
}

func (m *mockMCPAdmin) UpdateServer(_ context.Context, serverID string, cfg mcpcore.ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.servers[serverID] = cfg
	return nil
}

func (m *mockMCPAdmin) ListServers() []mcpcore.ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []mcpcore.ServerStatus
	for id, cfg := range m.servers {
		result = append(result, mcpcore.ServerStatus{
			ID:        id,
			Name:      cfg.Name,
			Transport: cfg.Transport,
			Status:    "connected",
			ToolCount: len(m.tools[id]),
		})
	}
	return result
}

func (m *mockMCPAdmin) GetServerTools(_ context.Context, serverID string) ([]mcpcore.ClientTool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.tools[serverID], nil
}

func (m *mockMCPAdmin) ReconnectServer(_ context.Context, serverID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *mockMCPAdmin) HealthCheck(_ context.Context) []mcp.ServerHealth {
	return []mcp.ServerHealth{}
}

// setupMCPAdminTest creates an admin test environment with MCPAdmin wired in.
func setupMCPAdminTest(t *testing.T, mock *mockMCPAdmin) http.Handler {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.SeedDefaults(); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	cfg := AdminRouterConfig{
		DB:       db.DB,
		MCPAdmin: mock,
	}
	ar := NewAdminRouter(cfg)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		ar.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestAdminMCPServers_List(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	// Empty list
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var servers []mcpcore.ServerStatus
	if err := json.NewDecoder(rec.Body).Decode(&servers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(servers))
	}

	// Add a server and list again
	mock.servers["test-srv"] = mcpcore.ServerConfig{
		ID: "test-srv", Name: "Test", Transport: "stdio", Enabled: true,
	}
	rec = doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if err := json.NewDecoder(rec.Body).Decode(&servers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].ID != "test-srv" {
		t.Errorf("id = %q, want %q", servers[0].ID, "test-srv")
	}
}

func TestAdminMCPServers_Create(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/mcp/servers", map[string]interface{}{
		"id":        "my-server",
		"name":      "My Server",
		"transport": "stdio",
		"command":   "/usr/bin/mcp-server",
		"enabled":   true,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] != "my-server" {
		t.Errorf("id = %v, want %q", resp["id"], "my-server")
	}
	if resp["status"] != "added" {
		t.Errorf("status = %v, want %q", resp["status"], "added")
	}

	// Verify server was stored
	cfg, ok := mock.servers["my-server"]
	if !ok {
		t.Fatal("server not stored in mock")
	}
	if cfg.Name != "My Server" {
		t.Errorf("name = %q, want %q", cfg.Name, "My Server")
	}
}

func TestAdminMCPServers_CreateValidation(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	tests := []struct {
		name string
		body map[string]interface{}
	}{
		{"missing id", map[string]interface{}{"name": "Test", "transport": "stdio"}},
		{"missing transport", map[string]interface{}{"id": "test", "name": "Test"}},
		{"invalid transport", map[string]interface{}{"id": "test", "transport": "websocket"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdminRequest(t, handler, "POST", "/v1/admin/mcp/servers", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for %s; body: %s", rec.Code, http.StatusBadRequest, tt.name, rec.Body.String())
			}
		})
	}
}

func TestAdminMCPServers_Delete(t *testing.T) {
	mock := newMockMCPAdmin()
	mock.servers["to-remove"] = mcpcore.ServerConfig{
		ID: "to-remove", Name: "Remove Me", Transport: "sse", Enabled: true,
	}
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/mcp/servers/to-remove", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "removed" {
		t.Errorf("status = %v, want %q", resp["status"], "removed")
	}

	// Verify server was deleted
	if _, ok := mock.servers["to-remove"]; ok {
		t.Error("server should have been removed")
	}
}

func TestAdminMCPServer_Get(t *testing.T) {
	mock := newMockMCPAdmin()
	mock.servers["get-srv"] = mcpcore.ServerConfig{
		ID: "get-srv", Name: "Get Me", Transport: "stdio", Enabled: true,
	}
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers/get-srv", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var status mcpcore.ServerStatus
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.ID != "get-srv" {
		t.Errorf("id = %q, want %q", status.ID, "get-srv")
	}
}

func TestAdminMCPServer_GetNotFound(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers/nonexistent", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAdminMCPServer_Update(t *testing.T) {
	mock := newMockMCPAdmin()
	mock.servers["update-srv"] = mcpcore.ServerConfig{
		ID: "update-srv", Name: "Old Name", Transport: "stdio", Enabled: true,
	}
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/mcp/servers/update-srv", map[string]interface{}{
		"name":    "New Name",
		"enabled": false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "updated" {
		t.Errorf("status = %v, want %q", resp["status"], "updated")
	}

	// Verify update
	if mock.servers["update-srv"].Name != "New Name" {
		t.Errorf("name = %q, want %q", mock.servers["update-srv"].Name, "New Name")
	}
}

func TestAdminMCPServer_Tools(t *testing.T) {
	mock := newMockMCPAdmin()
	mock.servers["tool-srv"] = mcpcore.ServerConfig{
		ID: "tool-srv", Name: "Tool Server", Transport: "stdio", Enabled: true,
	}
	mock.tools["tool-srv"] = []mcpcore.ClientTool{
		{Name: "tool1", Description: "First tool"},
		{Name: "tool2", Description: "Second tool"},
	}
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers/tool-srv/tools", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var tools []mcpcore.ClientTool
	if err := json.NewDecoder(rec.Body).Decode(&tools); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "tool1" {
		t.Errorf("tool[0].Name = %q, want %q", tools[0].Name, "tool1")
	}
}

func TestAdminMCPServer_Reconnect(t *testing.T) {
	mock := newMockMCPAdmin()
	mock.servers["reconn-srv"] = mcpcore.ServerConfig{
		ID: "reconn-srv", Name: "Reconnect", Transport: "sse", Enabled: true,
	}
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/mcp/servers/reconn-srv/reconnect", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "reconnecting" {
		t.Errorf("status = %v, want %q", resp["status"], "reconnecting")
	}
}

func TestAdminMCPServers_NoAdmin(t *testing.T) {
	// Test that endpoints work gracefully when MCPAdmin is nil
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.SeedDefaults(); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	ar := NewAdminRouter(AdminRouterConfig{DB: db.DB})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		ar.Handler().ServeHTTP(w, r.WithContext(ctx))
	})

	// GET /servers should return empty array
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/mcp/servers", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// POST /servers should return internal error
	rec = doAdminRequest(t, handler, "POST", "/v1/admin/mcp/servers", map[string]interface{}{
		"id": "test", "transport": "stdio",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestAdminCapabilities(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/capabilities", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Without CapabilityLister, should return empty array
	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty array without CapabilityLister, got %v", resp)
	}
}

func TestAdminMCPServers_MethodNotAllowed(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/mcp/servers", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminMCPServers_SSETransport(t *testing.T) {
	mock := newMockMCPAdmin()
	handler := setupMCPAdminTest(t, mock)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/mcp/servers", map[string]interface{}{
		"id":        "sse-srv",
		"name":      "SSE Server",
		"transport": "sse",
		"url":       "http://localhost:8080/mcp",
		"enabled":   true,
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	cfg, ok := mock.servers["sse-srv"]
	if !ok {
		t.Fatal("server not stored in mock")
	}
	if cfg.Transport != "sse" {
		t.Errorf("transport = %q, want %q", cfg.Transport, "sse")
	}
}
