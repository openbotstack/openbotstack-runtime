package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/execution"
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

// LLMAwareTool is an optional interface for builtin tools that need LLM access.
type LLMAwareTool interface {
	SetLLMAccess(access aitypes.LLMAccess)
}

// BuiltinToolRunner dispatches builtin.* tool calls to registered BuiltinTool implementations.
type BuiltinToolRunner struct {
	mu        sync.RWMutex
	tools     map[string]BuiltinTool
	llmAccess aitypes.LLMAccess
}

func NewBuiltinToolRunner() *BuiltinToolRunner {
	r := &BuiltinToolRunner{tools: make(map[string]BuiltinTool)}
	r.registerDefaults()
	return r
}

// SetLLMAccess injects LLM access into all LLMAwareTool implementations.
func (r *BuiltinToolRunner) SetLLMAccess(access aitypes.LLMAccess) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.llmAccess = access
	for _, t := range r.tools {
		if la, ok := t.(LLMAwareTool); ok {
			la.SetLLMAccess(access)
		}
	}
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
		&VisionAnalyzeTool{},
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

// AllPermissions returns the deduplicated set of permissions required by all registered tools.
func (r *BuiltinToolRunner) AllPermissions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool)
	var perms []string
	for _, t := range r.tools {
		for _, p := range t.Permissions() {
			if !seen[p] {
				seen[p] = true
				perms = append(perms, p)
			}
		}
	}
	return perms
}

// Execute implements toolrunner.ToolRunner. It dispatches to Run or RunWithPermissions
// based on whether the ExecutionContext carries granted permissions.
func (r *BuiltinToolRunner) Execute(ctx context.Context, toolID string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	var output map[string]any
	var err error
	if ec != nil && len(ec.GrantedPermissions) > 0 {
		output, err = r.RunWithPermissions(ctx, toolID, input, ec.GrantedPermissions)
	} else {
		output, err = r.Run(ctx, toolID, input)
	}
	if err != nil {
		return &execution.StepResult{StepName: toolID, Type: "tool", Error: err}, err
	}
	return &execution.StepResult{StepName: toolID, Type: "tool", Output: output}, nil
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

// ConfigureVisionTool enables or disables private network access for vision_analyze.
func (r *BuiltinToolRunner) ConfigureVisionTool(allowPrivateIPs bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools["vision_analyze"]; ok {
		if vt, ok := t.(*VisionAnalyzeTool); ok {
			vt.allowPrivateIPs = allowPrivateIPs
		}
	}
}
