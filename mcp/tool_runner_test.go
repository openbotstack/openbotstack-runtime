package mcp

import (
	"testing"
)

func TestParseMCPToolName(t *testing.T) {
	tests := []struct {
		input    string
		serverID string
		toolName string
		wantErr  bool
	}{
		{"mcp.server1.search", "server1", "search", false},
		{"mcp.my-srv.tool_name", "my-srv", "tool_name", false},
		{"mcp.a.b.c", "a", "b.c", false},
		{"tool.name", "", "", true},
		{"mcp.", "", "", true},
		{"mcp.serveronly", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		srvID, tool, err := parseMCPToolName(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseMCPToolName(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseMCPToolName(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if srvID != tt.serverID {
			t.Errorf("parseMCPToolName(%q) serverID = %q, want %q", tt.input, srvID, tt.serverID)
		}
		if tool != tt.toolName {
			t.Errorf("parseMCPToolName(%q) toolName = %q, want %q", tt.input, tool, tt.toolName)
		}
	}
}
