package builtin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type JSONQueryTool struct{}

func (t *JSONQueryTool) Name() string          { return "json_query" }
func (t *JSONQueryTool) Description() string   { return "Extracts a value from JSON using dot-notation path." }
func (t *JSONQueryTool) Parameters() map[string]string {
	return map[string]string{"json": "object", "path": "string"}
}
func (t *JSONQueryTool) Required() []string    { return []string{"json", "path"} }
func (t *JSONQueryTool) Permissions() []string { return nil }
func (t *JSONQueryTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	data, ok := input["json"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("json_query: json must be an object")
	}
	path, _ := input["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("json_query: path is required")
	}
	value, err := queryPath(data, strings.Split(path, "."))
	if err != nil {
		return nil, fmt.Errorf("json_query: %w", err)
	}
	return map[string]any{"value": value}, nil
}

func queryPath(current any, parts []string) (any, error) {
	if len(parts) == 0 {
		return current, nil
	}
	key := parts[0]
	rest := parts[1:]
	if m, ok := current.(map[string]any); ok {
		v, exists := m[key]
		if !exists {
			return nil, fmt.Errorf("key %q not found", key)
		}
		return queryPath(v, rest)
	}
	if arr, ok := current.([]any); ok {
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 || idx >= len(arr) {
			return nil, fmt.Errorf("invalid array index %q", key)
		}
		return queryPath(arr[idx], rest)
	}
	return nil, fmt.Errorf("cannot traverse into %T with key %q", current, key)
}
