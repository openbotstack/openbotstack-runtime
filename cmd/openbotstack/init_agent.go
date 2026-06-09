package main

import (
	"log/slog"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/control/agent"
	agentpkg "github.com/openbotstack/openbotstack-runtime/agent"
	harnesspkg "github.com/openbotstack/openbotstack-runtime/harness"
	reasoningpkg "github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// InitAgent creates either a HarnessAgent or DefaultAgent based on config.
func (b *ServerBuilder) InitAgent() *ServerBuilder {
	art := &assistant.AssistantRuntime{AssistantID: "default"}

	var apiAgent agent.Agent
	var reasoningStore *reasoningpkg.InMemoryStore
	var hookMgr *harnesspkg.HookManager

	{
		harnessCfg := harnesspkg.DefaultHarnessConfig()
		harnessCfg.MaxSteps = b.cfg.Agent.DualLoop.MaxSteps
		harnessCfg.MaxSessionRuntime = b.cfg.Agent.DualLoop.MaxSessionRuntime

		toolRunner := b.registryClient // RegistryClient implements ToolRunner directly

		stepExec := harnesspkg.NewStepExecutor(toolRunner, b.exec, harnesspkg.StepExecutorDeps{
			MCPRunner:     b.mcpRunner,
			BuiltinRunner: b.builtinRunner,
		})

		// Wire the StepDispatcher into the executor so ExecutePlan can dispatch
		// steps without depending on the harness package directly.
		b.exec.SetStepDispatcher(stepExec)

		reasoningCfg := harnesspkg.DefaultReasoningLoopConfig()
		reasoningCfg.MaxTurns = b.cfg.Agent.DualLoop.MaxTurns
		reasoningCfg.MaxToolCalls = b.cfg.Agent.DualLoop.MaxToolCalls
		reasoningCfg.MaxTurnRuntime = b.cfg.Agent.DualLoop.MaxTurnRuntime
		reasoningCfg.RepeatPlanStop = true

		compactionStrategy := harnesspkg.NewThresholdCompactionStrategy(
			harnesspkg.DefaultCompactionTrigger(),
			b.cfg.Agent.DualLoop.MaxRetainedTurns,
		)
		compactor := harnesspkg.NewContextCompactorAdapter(compactionStrategy)

		reasoningLoop := harnesspkg.NewDefaultReasoningLoop(reasoningCfg, b.dualPlanner, stepExec, compactor)

		hookMgr = harnesspkg.NewHookManager()

		h := harnesspkg.NewExecutionHarness(harnessCfg, toolRunner, b.exec, harnesspkg.HarnessDeps{
			ReasoningLoop: reasoningLoop,
			HookManager:   hookMgr,
			LLMGenerator:  b.buildLLMGenerator(),
			AuditLogger:   b.auditLogger,
			MCPRunner:     b.mcpRunner,
			BuiltinRunner: b.builtinRunner,
		})

		reasoningStore = reasoningpkg.NewInMemoryStore()

		apiAgent = agentpkg.NewHarnessAgent(agentpkg.HarnessAgentConfig{
			Planner:            b.dualPlanner,
			Registry:           b.exec,
			Runtime:            art,
			Harness:            h,
			WorkflowResolver:   agentpkg.NewKeywordWorkflowResolver(),
			ReasoningStore:     reasoningStore,
			CapRegistry:        b.capRegistry,
			GrantedPermissions: b.builtinRunner.AllPermissions(),
		})
		slog.Info("harness agent initialized",
			"max_steps", harnessCfg.MaxSteps,
			"max_session_runtime", harnessCfg.MaxSessionRuntime,
			"reasoning_max_turns", reasoningCfg.MaxTurns,
			"reasoning_max_tool_calls", reasoningCfg.MaxToolCalls,
		)
	}

	b.apiAgent = apiAgent
	b.reasoningStore = reasoningStore
	b.hookMgr = hookMgr
	return b
}
