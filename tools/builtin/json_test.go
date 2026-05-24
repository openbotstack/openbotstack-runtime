package builtin

import (
	"context"
	"testing"
)

func TestJSONParseTool(t *testing.T) {
	tool := &JSONParseTool{}
	result, err := tool.Execute(context.Background(), map[string]any{
		"raw_text": `{"name": "Alice", "age": 30}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["parsed"] == nil {
		t.Fatal("expected parsed field")
	}
}

func TestJSONParseTool_InvalidJSON(t *testing.T) {
	tool := &JSONParseTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"raw_text": `{invalid}`,
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONQueryTool(t *testing.T) {
	tool := &JSONQueryTool{}
	result, err := tool.Execute(context.Background(), map[string]any{
		"json": map[string]any{"user": map[string]any{"name": "Bob", "tags": []any{"a", "b"}}},
		"path": "user.name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["value"] != "Bob" {
		t.Errorf("value = %v, want Bob", result["value"])
	}
}

func TestJSONQueryTool_ArrayIndex(t *testing.T) {
	tool := &JSONQueryTool{}
	result, err := tool.Execute(context.Background(), map[string]any{
		"json": map[string]any{"items": []any{"x", "y", "z"}},
		"path": "items.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["value"] != "y" {
		t.Errorf("value = %v, want y", result["value"])
	}
}

func TestJSONQueryTool_MissingPath(t *testing.T) {
	tool := &JSONQueryTool{}
	_, err := tool.Execute(context.Background(), map[string]any{
		"json": map[string]any{"a": 1},
		"path": "b.c",
	})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}
