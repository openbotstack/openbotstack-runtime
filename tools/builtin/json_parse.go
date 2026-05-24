package builtin

import (
	"context"
	"encoding/json"
	"fmt"
)

type JSONParseTool struct{}

func (t *JSONParseTool) Name() string          { return "json_parse" }
func (t *JSONParseTool) Description() string   { return "Parses raw text into structured JSON." }
func (t *JSONParseTool) Parameters() map[string]string { return map[string]string{"raw_text": "string"} }
func (t *JSONParseTool) Required() []string    { return []string{"raw_text"} }
func (t *JSONParseTool) Permissions() []string { return nil }
func (t *JSONParseTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	raw, _ := input["raw_text"].(string)
	if raw == "" {
		return nil, fmt.Errorf("json_parse: raw_text is required")
	}
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("json_parse: %w", err)
	}
	return map[string]any{"parsed": parsed}, nil
}
