package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/openbotstack/openbotstack-core/mcp"
)

// mockTransport is a test transport that echoes back canned responses.
type mockTransport struct {
	responses map[string]json.RawMessage
	closed    bool
}

func (m *mockTransport) Send(_ context.Context, request json.RawMessage) (json.RawMessage, error) {
	var req mcp.JSONRPCRequest
	_ = json.Unmarshal(request, &req)
	if resp, ok := m.responses[req.Method]; ok {
		return resp, nil
	}
	return json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`), nil
}

func (m *mockTransport) SendNotification(_ json.RawMessage) error {
	return nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func TestClient_Initialize(t *testing.T) {
	transport := &mockTransport{}
	client := NewClient(transport)

	if err := client.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
}

func TestClient_ListTools(t *testing.T) {
	toolsResp := json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": 1,
		"result": {
			"tools": [
				{"name": "search", "description": "Search for items", "inputSchema": {"type": "object", "properties": {"query": {"type": "string"}}, "required": ["query"]}},
				{"name": "calculate", "description": "Calculate expression", "inputSchema": {"type": "object", "properties": {"expr": {"type": "string"}}}}
			]
		}
	}`)
	transport := &mockTransport{responses: map[string]json.RawMessage{
		"tools/list": toolsResp,
	}}
	client := NewClient(transport)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools count = %d, want 2", len(tools))
	}
	if tools[0].Name != "search" {
		t.Errorf("tools[0].Name = %q", tools[0].Name)
	}
	if tools[0].InputSchema == nil {
		t.Error("expected InputSchema")
	}
	if tools[0].InputSchema.Type != "object" {
		t.Errorf("schema type = %q", tools[0].InputSchema.Type)
	}
}

func TestClient_CallTool(t *testing.T) {
	callResp := json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": 1,
		"result": {
			"content": [{"type": "text", "text": "result: 42"}],
			"is_error": false
		}
	}`)
	transport := &mockTransport{responses: map[string]json.RawMessage{
		"tools/call": callResp,
	}}
	client := NewClient(transport)

	result, err := client.CallTool(context.Background(), "calculate", map[string]any{"expr": "6*7"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Error("expected no error")
	}
	if len(result.Content) != 1 {
		t.Fatalf("content count = %d, want 1", len(result.Content))
	}
	if result.Content[0].Text != "result: 42" {
		t.Errorf("text = %q", result.Content[0].Text)
	}
}

func TestClient_RPCError(t *testing.T) {
	errResp := json.RawMessage(`{
		"jsonrpc": "2.0",
		"id": 1,
		"error": {"code": -32601, "message": "Method not found"}
	}`)
	transport := &mockTransport{responses: map[string]json.RawMessage{
		"tools/list": errResp,
	}}
	client := NewClient(transport)

	_, err := client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Close(t *testing.T) {
	transport := &mockTransport{}
	client := NewClient(transport)
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !transport.closed {
		t.Error("transport not closed")
	}
}
