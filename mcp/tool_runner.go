package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// MCPToolRunner executes MCP tools via the MCPManager.
// It implements the toolrunner.ToolRunner interface.
type MCPToolRunner struct {
	manager *MCPManager
}

// NewMCPToolRunner creates a runner that routes tool calls to MCP servers.
func NewMCPToolRunner(manager *MCPManager) *MCPToolRunner {
	return &MCPToolRunner{manager: manager}
}

// Execute runs an MCP tool. The tool name must be in "mcp.{serverID}.{toolName}" format.
func (r *MCPToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	start := time.Now()

	serverID, toolName, err := parseMCPToolName(name)
	if err != nil {
		return &execution.StepResult{
			StepName: name,
			Type:     "tool",
			Error:    err,
			Duration: time.Since(start),
		}, err
	}

	result, err := r.manager.CallTool(ctx, serverID, toolName, input)
	duration := time.Since(start)

	if err != nil {
		return &execution.StepResult{
			StepName: name,
			Type:     "tool",
			Error:    err,
			Duration: duration,
		}, err
	}

	var output any
	if result != nil && len(result.Content) > 0 {
		parts := make([]string, 0, len(result.Content))
		for _, c := range result.Content {
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
		output = strings.Join(parts, "\n")
	}

	sr := &execution.StepResult{
		StepName: name,
		Type:     "tool",
		Output:   output,
		Duration: duration,
	}
	if result != nil && result.IsError {
		sr.Error = fmt.Errorf("MCP tool error: %v", output)
	}
	return sr, nil
}

// parseMCPToolName splits "mcp.{serverID}.{toolName}" into components.
func parseMCPToolName(name string) (serverID, toolName string, err error) {
	if !strings.HasPrefix(name, "mcp.") {
		return "", "", fmt.Errorf("invalid MCP tool name %q: must start with 'mcp.'", name)
	}
	rest := name[4:]
	idx := strings.Index(rest, ".")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid MCP tool name %q: expected 'mcp.{serverID}.{toolName}'", name)
	}
	return rest[:idx], rest[idx+1:], nil
}
