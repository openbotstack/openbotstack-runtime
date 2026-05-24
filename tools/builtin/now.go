package builtin

import (
	"context"
	"time"
)

type NowTool struct{}

func (t *NowTool) Name() string          { return "now" }
func (t *NowTool) Description() string   { return "Returns the current UTC timestamp in ISO 8601 format." }
func (t *NowTool) Parameters() map[string]string { return nil }
func (t *NowTool) Required() []string    { return nil }
func (t *NowTool) Permissions() []string { return nil }
func (t *NowTool) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	return map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339)}, nil
}
