package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteFileTool struct {
	AllowedDirs []string
	MaxBytes    int64
}

func (t *WriteFileTool) Name() string        { return "write_file" }
func (t *WriteFileTool) Description() string { return "Writes content to a file in an allowed directory." }
func (t *WriteFileTool) Parameters() map[string]string {
	return map[string]string{"path": "string", "content": "string", "append": "boolean"}
}
func (t *WriteFileTool) Required() []string    { return []string{"path", "content"} }
func (t *WriteFileTool) Permissions() []string { return []string{"file.write"} }
func (t *WriteFileTool) Execute(_ context.Context, input map[string]any) (map[string]any, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("write_file: path is required")
	}
	content, _ := input["content"].(string)
	maxBytes := t.MaxBytes
	if maxBytes == 0 {
		maxBytes = 1024 * 1024 // 1MB default
	}
	if int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("write_file: content too large (%d bytes, max %d)", len(content), maxBytes)
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("write_file: invalid path: %w", err)
	}
	if !t.isAllowed(absPath) {
		return nil, fmt.Errorf("write_file: access denied: %s", absPath)
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	appendMode, _ := input["append"].(bool)
	if appendMode {
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	f, err := os.OpenFile(absPath, flag, 0600)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return nil, fmt.Errorf("write_file: %w", err)
	}
	return map[string]any{"bytes_written": n, "path": absPath}, nil
}

func (t *WriteFileTool) isAllowed(absPath string) bool {
	return isPathAllowed(absPath, t.AllowedDirs)
}
