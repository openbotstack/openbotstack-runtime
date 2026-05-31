package mcp

import (
	"context"
	"testing"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-core/ai/types"
)

func TestHealthCheck_NoServers(t *testing.T) {
	m := &MCPManager{}
	result := m.HealthCheck(context.Background())
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}

func TestValidateTools_ValidTool(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "search",
			Description: "Search documents",
			InputSchema: &types.JSONSchema{Type: "object"},
		},
	}
	validations := validateTools(tools)
	if len(validations) != 1 {
		t.Fatalf("expected 1 validation, got %d", len(validations))
	}
	if !validations[0].Valid {
		t.Errorf("tool should be valid, issues: %v", validations[0].Issues)
	}
}

func TestValidateTools_MissingName(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "",
			Description: "No name tool",
			InputSchema: &types.JSONSchema{Type: "object"},
		},
	}
	validations := validateTools(tools)
	if validations[0].Valid {
		t.Error("tool with empty name should be invalid")
	}
}

func TestValidateTools_MissingDescription(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "search",
			Description: "",
			InputSchema: &types.JSONSchema{Type: "object"},
		},
	}
	validations := validateTools(tools)
	// Description empty adds a warning issue but doesn't make it invalid
	if len(validations[0].Issues) == 0 {
		t.Error("tool with empty description should have an issue")
	}
}

func TestValidateTools_NilInputSchema(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "search",
			Description: "Search",
			InputSchema: nil,
		},
	}
	validations := validateTools(tools)
	if validations[0].Valid {
		t.Error("tool with nil input_schema should be invalid")
	}
}

func TestValidateTools_InputSchemaNoType(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "search",
			Description: "Search",
			InputSchema: &types.JSONSchema{Type: ""},
		},
	}
	validations := validateTools(tools)
	if validations[0].Valid {
		t.Error("tool with input_schema missing type should be invalid")
	}
}

func TestValidateTools_AllValid(t *testing.T) {
	tools := []mcpcore.ClientTool{
		{
			Name:        "search",
			Description: "Search documents",
			InputSchema: &types.JSONSchema{Type: "object"},
		},
		{
			Name:        "summarize",
			Description: "Summarize text",
			InputSchema: &types.JSONSchema{Type: "object"},
		},
	}
	validations := validateTools(tools)
	for i, v := range validations {
		if !v.Valid {
			t.Errorf("tool[%d] should be valid, issues: %v", i, v.Issues)
		}
	}
}
