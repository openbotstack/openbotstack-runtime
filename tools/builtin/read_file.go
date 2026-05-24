package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type ReadFileTool struct {
	AllowedDirs []string
	MaxBytes    int64
}

func (t *ReadFileTool) Name() string        { return "read_file" }
func (t *ReadFileTool) Description() string { return "Reads a file from an allowed directory." }
func (t *ReadFileTool) Parameters() map[string]string {
	return map[string]string{"path": "string"}
}
func (t *ReadFileTool) Required() []string    { return []string{"path"} }
func (t *ReadFileTool) Permissions() []string { return []string{"file.read"} }
func (t *ReadFileTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("read_file: path is required")
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read_file: invalid path: %w", err)
	}
	if !t.isAllowed(absPath) {
		return nil, fmt.Errorf("read_file: access denied: %s", absPath)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	maxBytes := t.MaxBytes
	if maxBytes == 0 {
		maxBytes = 1024 * 1024
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("read_file: file too large (%d bytes, max %d)", info.Size(), maxBytes)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}
	return map[string]any{"content": string(data), "size": len(data)}, nil
}

func (t *ReadFileTool) isAllowed(absPath string) bool {
	return isPathAllowed(absPath, t.AllowedDirs)
}
