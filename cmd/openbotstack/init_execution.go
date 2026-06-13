package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/types"
	plannerpkg "github.com/openbotstack/openbotstack-core/planner"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// InitExecution creates Wasm runtime, executor, host functions, and planner.
func (b *ServerBuilder) InitExecution() *ServerBuilder {
	b.requireInit("pdb", "InitExecution")
	b.requireInit("modelRouter", "InitExecution")
	wasmRuntime, err := wasm.NewRuntime()
	if err != nil {
		slog.Error("failed to initialize wasm runtime", "error", err)
		os.Exit(1)
	}

	hostFuncs := &wasm.HostFunctions{
		LLMGenerate: func(ctx context.Context, prompt string) (string, error) {
			mReq := types.GenerateRequest{
				Messages: []types.Message{types.NewTextMessage("user", prompt)},
			}
			prov, err := b.modelRouter.Route([]types.CapabilityType{types.CapTextGeneration}, types.ModelConstraints{})
			if err != nil {
				return "LLM not configured or suitable provider not found", nil
			}
			resp, err := prov.Generate(ctx, mReq)
			if err != nil {
				return "", err
			}
			return resp.Content, nil
		},
		Log: func(ctx context.Context, level, msg string) {
			slog.Info("wasm log", "level", level, "msg", msg)
		},
	}

	exec := executor.NewDefaultExecutorWithRuntime(wasmRuntime, nil)
	exec.SetTextGenerator(&executor.LLMTextGenerator{Router: b.modelRouter})

	if err := wasmRuntime.RegisterHostFunctions(context.Background(), hostFuncs); err != nil {
		slog.Error("failed to register host functions", "error", err)
		os.Exit(1)
	}

	skillsPath := os.Getenv("OBS_SKILLS_PATH")
	if skillsPath == "" {
		skillsPath = "data/skills"
	}
	// Ensure skills directory exists so the watcher can attach to it.
	if err := os.MkdirAll(skillsPath, 0755); err != nil {
		slog.Warn("failed to create skills directory", "path", skillsPath, "error", err)
	}
	if err := loadSkills(context.Background(), exec, skillsPath); err != nil {
		slog.Error("failed to load skills", "error", err)
	}

	plannerLimits := &plannerpkg.ExecutionLimits{
		MaxSteps:         10,
		MaxToolCalls:     15,
		MaxExecutionTime: 300 * time.Second,
	}
	dualPlanner := plannerpkg.NewLLMPlanner(b.modelRouter, plannerLimits)
	slog.Info("execution planner initialized with LLM router")

	b.hostFuncs = hostFuncs
	b.exec = exec
	b.dualPlanner = dualPlanner
	b.registryClient = toolrunner.NewRegistryClient(b.cfg.Sandbox.ToolRegistryURL)

	// Start skill watcher for hot-reload
	skillWatcher := NewSkillWatcher(exec, skillsPath)
	if err := skillWatcher.Start(context.Background()); err != nil {
		slog.Warn("skill watcher failed to start, hot-reload disabled", "error", err)
		skillWatcher = nil
	}
	b.skillWatcher = skillWatcher

	return b
}
