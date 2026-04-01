package runtime

import (
	"context"
	"fmt"

	"github.com/openbotstack/openbotstack-core/assistant"
	// These will map to our new implementations
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/runtime/llm"
	"github.com/openbotstack/openbotstack-runtime/runtime/memory"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// AssistantRuntime encapsulates the entire AI Assistant execution loop.
type AssistantRuntime struct {
	Soul          assistant.AssistantSoul
	MemoryManager memory.MemoryManager
	Planner       planner.ExecutionPlanner
	ToolRunner    toolrunner.ToolRunner
	ModelProvider llm.ModelProvider

	// A mock executor or specific component can be added here
	// to handle execution plans.
}

// Config holds components required to scaffold the runtime.
type Config struct {
	SoulPath      string
	MemoryManager memory.MemoryManager
	Planner       planner.ExecutionPlanner
	ToolRunner    toolrunner.ToolRunner
	ModelProvider llm.ModelProvider
}

// NewAssistantRuntime initializes the system and loads the soul layout.
func NewAssistantRuntime(cfg Config) (*AssistantRuntime, error) {
	if cfg.SoulPath == "" {
		cfg.SoulPath = "soul.md"
	}

	soul, err := assistant.LoadSoulFromMarkdown(cfg.SoulPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load soul %s: %w", cfg.SoulPath, err)
	}

	return &AssistantRuntime{
		Soul:          soul,
		MemoryManager: cfg.MemoryManager,
		Planner:       cfg.Planner,
		ToolRunner:    cfg.ToolRunner,
		ModelProvider: cfg.ModelProvider,
	}, nil
}

// ProcessChat represents the full runtime loop for a user request.
func (r *AssistantRuntime) ProcessChat(ctx context.Context, sessionID, message string) (<-chan string, error) {
	// Our runtime loop as requested:
	// User request -> Planner.GeneratePlan -> Executor.ExecutePlan -> ToolRunner -> ModelProvider -> Response

	// 1. Save user memory (if applicable using MemoryManager)

	// 2. Planner.GeneratePlan
	// In a complete implementation, this would use Planner to figure out the plan.
	// We'll simulate the plan extraction for now.
	
	// 3. Executor.ExecutePlan
	// Here we simulate picking up tools from the plan.
	
	// 4. ToolRunner
	// The executor would use ToolRunner to provide context from external sources.
	
	// 5. ModelProvider
	// Finally, the ModelProvider generates the response based on the tool insights and user message.
	
	systemPrompt := fmt.Sprintf("%s\n\n%s\n\n%s", r.Soul.SystemPrompt, r.Soul.Personality, r.Soul.Instructions)
	fullPrompt := fmt.Sprintf("System: %s\nUser: %s", systemPrompt, message)

	return r.ModelProvider.Stream(fullPrompt)
}
