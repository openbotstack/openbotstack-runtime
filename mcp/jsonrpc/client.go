package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	skills "github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/mcp"
)

// Client implements mcp.Client over a Transport.
type Client struct {
	transport Transport
	nextID    atomic.Int64
}

// NewClient creates a new JSON-RPC client backed by the given transport.
func NewClient(transport Transport) *Client {
	return &Client{transport: transport}
}

func (c *Client) nextRequestID() int64 {
	return c.nextID.Add(1)
}

// Initialize performs the MCP handshake with the server.
func (c *Client) Initialize(ctx context.Context) error {
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "openbotstack",
				"version": "1.0.0",
			},
		},
	}
	_, err := c.sendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Send initialized notification
	notif := mcp.JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	return c.sendNotification(ctx, notif)
}

// ListTools discovers all tools available on the server.
func (c *Client) ListTools(ctx context.Context) ([]mcp.ClientTool, error) {
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/list",
		Params:  map[string]any{},
	}
	raw, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}

	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description,omitempty"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools: %w", err)
	}

	tools := make([]mcp.ClientTool, 0, len(result.Tools))
	for _, t := range result.Tools {
		schema := convertSchema(t.InputSchema)
		tools = append(tools, mcp.ClientTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return tools, nil
}

// CallTool invokes a tool on the server with the given arguments.
func (c *Client) CallTool(ctx context.Context, toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	req := mcp.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "tools/call",
		Params: mcp.ToolCallParams{
			Name:      toolName,
			Arguments: arguments,
		},
	}
	raw, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("call tool %q: %w", toolName, err)
	}

	var callResult mcp.CallToolResult
	if err := json.Unmarshal(raw, &callResult); err != nil {
		return nil, fmt.Errorf("unmarshal tool result: %w", err)
	}
	return &callResult, nil
}

// Close shuts down the client connection.
func (c *Client) Close() error {
	return c.transport.Close()
}

func (c *Client) sendRequest(ctx context.Context, req mcp.JSONRPCRequest) (json.RawMessage, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, err := c.transport.Send(ctx, data)
	if err != nil {
		return nil, err
	}

	var resp mcp.JSONRPCResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return jsonRawResult(resp.Result)
}

func (c *Client) sendNotification(ctx context.Context, notif mcp.JSONRPCNotification) error {
	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	return c.transport.SendNotification(data)
}

func jsonRawResult(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage(`{}`), nil
	}
	return json.Marshal(v)
}

// convertSchema converts a raw map[string]any schema to a structured JSONSchema.
func convertSchema(raw map[string]any) *skills.JSONSchema {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var schema skills.JSONSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil
	}
	return &schema
}
