package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// BuiltinTool is a platform-primitive tool with no business logic.
type BuiltinTool interface {
	Name() string
	Description() string
	Parameters() map[string]string  // param name → type description
	Required() []string
	Permissions() []string
	Execute(ctx context.Context, input map[string]any) (map[string]any, error)
}

// BuiltinToolRunner dispatches builtin.* tool calls to registered BuiltinTool implementations.
type BuiltinToolRunner struct {
	mu    sync.RWMutex
	tools map[string]BuiltinTool
}

func NewBuiltinToolRunner() *BuiltinToolRunner {
	r := &BuiltinToolRunner{tools: make(map[string]BuiltinTool)}
	r.registerDefaults()
	return r
}

func (r *BuiltinToolRunner) registerDefaults() {
	for _, t := range []BuiltinTool{
		&NowTool{},
		&ReadFileTool{},
		&WriteFileTool{},
		&WebFetchTool{},
		&JSONParseTool{},
		&JSONQueryTool{},
		&UUIDGenerateTool{},
	} {
		r.tools[t.Name()] = t
	}
}

func (r *BuiltinToolRunner) Run(ctx context.Context, toolID string, input map[string]any) (map[string]any, error) {
	r.mu.RLock()
	tool, ok := r.tools[stripPrefix(toolID)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("builtin tool %q not found", toolID)
	}
	return tool.Execute(ctx, input)
}

func (r *BuiltinToolRunner) RunWithPermissions(ctx context.Context, toolID string, input map[string]any, granted []string) (map[string]any, error) {
	r.mu.RLock()
	tool, ok := r.tools[stripPrefix(toolID)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("builtin tool %q not found", toolID)
	}
	if !hasPermissions(tool.Permissions(), granted) {
		return nil, fmt.Errorf("permission denied: tool %q requires %v", toolID, tool.Permissions())
	}
	return tool.Execute(ctx, input)
}

func (r *BuiltinToolRunner) Tools() []BuiltinTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]BuiltinTool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

func stripPrefix(id string) string {
	return strings.TrimPrefix(id, "builtin.")
}

func hasPermissions(required, granted []string) bool {
	if len(required) == 0 {
		return true
	}
	grantedSet := make(map[string]bool, len(granted))
	for _, p := range granted {
		grantedSet[p] = true
	}
	for _, p := range required {
		if !grantedSet[p] {
			return false
		}
	}
	return true
}

// isPathAllowed checks whether absPath falls under one of the allowed directories.
// Both absPath and each allowed dir are resolved to absolute form before comparison.
func isPathAllowed(absPath string, allowedDirs []string) bool {
	for _, dir := range allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if strings.HasPrefix(absPath, absDir+string(filepath.Separator)) || absPath == absDir {
			return true
		}
	}
	return false
}

// ConfigureFileTools updates AllowedDirs and MaxBytes on the read_file and
// write_file built-in tools. This must be called before any concurrent use.
func (r *BuiltinToolRunner) ConfigureFileTools(allowedDirs []string, maxBytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools["read_file"]; ok {
		if rt, ok := t.(*ReadFileTool); ok {
			rt.AllowedDirs = allowedDirs
			rt.MaxBytes = maxBytes
		}
	}
	if t, ok := r.tools["write_file"]; ok {
		if wt, ok := t.(*WriteFileTool); ok {
			wt.AllowedDirs = allowedDirs
			wt.MaxBytes = maxBytes
		}
	}
}
