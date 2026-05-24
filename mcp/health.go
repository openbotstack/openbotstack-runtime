package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
)

// ServerHealth is the health status of a single MCP server.
type ServerHealth struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	ToolCount int       `json:"tool_count"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// HealthCheck probes all connected servers via ListTools.
// It does NOT change server status — only reports current health.
func (m *MCPManager) HealthCheck(ctx context.Context) []ServerHealth {
	m.mu.RLock()
	servers := make(map[string]*managedServer, len(m.servers))
	for k, v := range m.servers {
		servers[k] = v
	}
	m.mu.RUnlock()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []ServerHealth
	)

	for id, srv := range servers {
		wg.Add(1)
		go func(id string, srv *managedServer) {
			defer wg.Done()

			h := ServerHealth{
				ID:        id,
				Name:      srv.config.Name,
				CheckedAt: time.Now(),
			}

			if srv.status != "connected" || srv.client == nil {
				h.Error = fmt.Sprintf("server status: %s", srv.status)
				mu.Lock()
				results = append(results, h)
				mu.Unlock()
				return
			}

			tools, err := srv.client.ListTools(ctx)
			if err != nil {
				h.Error = err.Error()
				slog.Warn("mcp health check failed", "id", id, "error", err)
			} else {
				h.Healthy = true
				h.ToolCount = len(tools)
			}

			mu.Lock()
			results = append(results, h)
			mu.Unlock()
		}(id, srv)
	}
	wg.Wait()

	return results
}

// ValidateToolSchemas checks that all tools on a server have required fields.
func (m *MCPManager) ValidateToolSchemas(_ context.Context, serverID string) ([]ToolValidation, error) {
	m.mu.RLock()
	srv, ok := m.servers[serverID]
	if !ok {
		m.mu.RUnlock()
		return nil, fmt.Errorf("server %q not found", serverID)
	}
	tools := srv.tools
	m.mu.RUnlock()

	return validateTools(tools), nil
}

// validateTools validates a slice of tools and returns per-tool results.
func validateTools(tools []mcpcore.ClientTool) []ToolValidation {
	results := make([]ToolValidation, len(tools))
	for i, tool := range tools {
		v := ToolValidation{
			ToolName: tool.Name,
			Valid:    true,
		}

		if tool.Name == "" {
			v.Valid = false
			v.Issues = append(v.Issues, "tool name is empty")
		}
		if tool.Description == "" {
			v.Issues = append(v.Issues, "tool description is empty")
		}
		if tool.InputSchema == nil {
			v.Valid = false
			v.Issues = append(v.Issues, "input schema is missing")
		} else {
			if tool.InputSchema.Type == "" {
				v.Valid = false
				v.Issues = append(v.Issues, "input schema missing 'type' field")
			}
		}

		results[i] = v
	}
	return results
}

// ToolValidation is the result of validating a single tool's schema.
type ToolValidation struct {
	ToolName string   `json:"tool_name"`
	Valid    bool     `json:"valid"`
	Issues   []string `json:"issues,omitempty"`
}
