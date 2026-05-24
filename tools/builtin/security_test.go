package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileTool_AllowedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)
	tool := &ReadFileTool{AllowedDirs: []string{dir}}
	result, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["content"] != "hello world" {
		t.Errorf("content = %v, want 'hello world'", result["content"])
	}
}

func TestReadFileTool_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := &ReadFileTool{AllowedDirs: []string{dir}}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error for path outside allowed dirs")
	}
}

func TestReadFileTool_DotDotTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := &ReadFileTool{AllowedDirs: []string{dir}}
	traversal := filepath.Join(dir, "..", "..", "etc", "passwd")
	_, err := tool.Execute(context.Background(), map[string]any{"path": traversal})
	if err == nil {
		t.Fatal("expected error for ../ traversal")
	}
}

func TestReadFileTool_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	os.WriteFile(path, make([]byte, 1024*1024+1), 0644)
	tool := &ReadFileTool{AllowedDirs: []string{dir}, MaxBytes: 1024 * 1024}
	_, err := tool.Execute(context.Background(), map[string]any{"path": path})
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

func TestWriteFileTool_AllowedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	tool := &WriteFileTool{AllowedDirs: []string{dir}}
	result, err := tool.Execute(context.Background(), map[string]any{"path": path, "content": "test data"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["bytes_written"] != 9 {
		t.Errorf("bytes_written = %v, want 9", result["bytes_written"])
	}
	data, _ := os.ReadFile(path)
	if string(data) != "test data" {
		t.Errorf("file content = %q, want 'test data'", string(data))
	}
}

func TestWriteFileTool_AppendMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.txt")
	os.WriteFile(path, []byte("first "), 0644)
	tool := &WriteFileTool{AllowedDirs: []string{dir}}
	tool.Execute(context.Background(), map[string]any{"path": path, "content": "second", "append": true})
	data, _ := os.ReadFile(path)
	if string(data) != "first second" {
		t.Errorf("file content = %q, want 'first second'", string(data))
	}
}

func TestWriteFileTool_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	tool := &WriteFileTool{AllowedDirs: []string{dir}}
	_, err := tool.Execute(context.Background(), map[string]any{"path": "/tmp/evil.txt", "content": "bad"})
	if err == nil {
		t.Fatal("expected error for path outside allowed dirs")
	}
}
