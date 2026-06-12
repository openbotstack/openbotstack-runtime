package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/capability"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
)

// CapabilityRegistrar unifies registration of all capability types
// (skills, builtin tools) into a single CapabilityRegistry.
type CapabilityRegistrar struct {
	capRegistry capability.CapabilityRegistry
	exec        *executor.DefaultExecutor
}

// NewCapabilityRegistrar creates a registrar for the given registry and executor.
func NewCapabilityRegistrar(capRegistry capability.CapabilityRegistry, exec *executor.DefaultExecutor) *CapabilityRegistrar {
	return &CapabilityRegistrar{capRegistry: capRegistry, exec: exec}
}

// RegisterSkills registers all loaded skills as capabilities.
func (r *CapabilityRegistrar) RegisterSkills(ctx context.Context) {
	for _, id := range r.exec.List() {
		skill, err := r.exec.GetSkill(ctx, id)
		if err != nil || skill == nil {
			slog.Warn("capability registrar: skip skill", "id", id, "error", err)
			continue
		}
		adapter := capability.NewFromSkill(skill)
		if err := r.capRegistry.Register(ctx, adapter); err != nil {
			slog.Warn("capability registrar: register skill", "id", id, "error", err)
		}
	}
}

// RegisterBuiltins creates a BuiltinToolRunner, configures file tools,
// and registers all builtin tools as capabilities. Returns the shared runner.
func (r *CapabilityRegistrar) RegisterBuiltins() *builtintools.BuiltinToolRunner {
	runner := builtintools.NewBuiltinToolRunner()

	// Configure file tools with allowed dirs and size limits.
	allowedDirs := []string{"./data"}
	if envDirs := os.Getenv("OBS_FILE_ALLOWED_DIRS"); envDirs != "" {
		allowedDirs = strings.Split(envDirs, ",")
	}
	maxBytes := int64(1024 * 1024)
	runner.ConfigureFileTools(allowedDirs, maxBytes)
	slog.Info("file tools configured", "allowed_dirs", allowedDirs, "max_bytes", maxBytes)

	// Configure vision tool. SSRF protection (blocking private/loopback addresses)
	// is ON by default — the safe posture for a governance-focused stack. Allow
	// private networks only when explicitly opted in, e.g. for internal-only
	// deployments where image/document hosts live on a private network.
	allowPrivateNetworks := os.Getenv("OBS_VISION_ALLOW_PRIVATE_NETWORKS") == "true"
	runner.ConfigureVisionTool(allowPrivateNetworks)
	if allowPrivateNetworks {
		slog.Warn("vision tool: SSRF protection DISABLED — private/loopback network access allowed (OBS_VISION_ALLOW_PRIVATE_NETWORKS=true)")
	}

	for _, tool := range runner.Tools() {
		params := tool.Parameters()
		props := make(map[string]*types.JSONSchema, len(params))
		for name, typ := range params {
			props[name] = &types.JSONSchema{Type: typ}
		}
		adapter := capability.NewFromNative(
			"builtin."+tool.Name(),
			tool.Name(),
			tool.Description(),
			&types.JSONSchema{
				Type:       "object",
				Properties: props,
				Required:   tool.Required(),
			},
		)
		if err := r.capRegistry.Register(context.Background(), adapter); err != nil {
			slog.Warn("capability registrar: register builtin", "tool", tool.Name(), "error", err)
		}
	}
	slog.Info("builtin tools registered as capabilities", "count", len(runner.Tools()))
	return runner
}

// RegisterAll performs skill and builtin registration, returns the builtin runner.
func (r *CapabilityRegistrar) RegisterAll(ctx context.Context) *builtintools.BuiltinToolRunner {
	r.RegisterSkills(ctx)
	return r.RegisterBuiltins()
}
