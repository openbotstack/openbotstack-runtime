package builtin

import (
	"context"
	"crypto/rand"
	"fmt"
)

type UUIDGenerateTool struct{}

func (t *UUIDGenerateTool) Name() string          { return "uuid_generate" }
func (t *UUIDGenerateTool) Description() string   { return "Generates a random UUID v4." }
func (t *UUIDGenerateTool) Parameters() map[string]string { return nil }
func (t *UUIDGenerateTool) Required() []string    { return nil }
func (t *UUIDGenerateTool) Permissions() []string { return nil }
func (t *UUIDGenerateTool) Execute(_ context.Context, _ map[string]any) (map[string]any, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return nil, fmt.Errorf("uuid_generate: %w", err)
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return map[string]any{
		"uuid": fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]),
	}, nil
}
