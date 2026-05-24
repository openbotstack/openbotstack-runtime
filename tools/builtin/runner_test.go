package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinToolRunner_UnknownTool(t *testing.T) {
	runner := NewBuiltinToolRunner()
	_, err := runner.Run(context.Background(), "builtin.unknown", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestBuiltinToolRunner_DispatchesToNow(t *testing.T) {
	runner := NewBuiltinToolRunner()
	result, err := runner.Run(context.Background(), "builtin.now", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["timestamp"] == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestBuiltinToolRunner_PermissionDenied(t *testing.T) {
	runner := NewBuiltinToolRunner()
	_, err := runner.RunWithPermissions(context.Background(), "builtin.read_file", map[string]any{"path": "/etc/passwd"}, nil)
	if err == nil {
		t.Fatal("expected permission denied error")
	}
}

func TestBuiltinToolRunner_PermissionGranted(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	os.WriteFile(testFile, []byte("ok"), 0644)
	runner := NewBuiltinToolRunner()
	// Override the registered read_file with one that allows our temp dir
	runner.mu.Lock()
	runner.tools["read_file"] = &ReadFileTool{AllowedDirs: []string{dir}}
	runner.mu.Unlock()
	_, err := runner.RunWithPermissions(context.Background(), "builtin.read_file", map[string]any{"path": testFile}, []string{"file.read"})
	// Should not fail on permission
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuiltinToolRunner_ToolsList(t *testing.T) {
	runner := NewBuiltinToolRunner()
	tools := runner.Tools()
	if len(tools) != 7 {
		t.Errorf("expected 7 tools, got %d", len(tools))
	}
}
