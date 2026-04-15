// Package context provides the runtime implementation of context assembly.
// It builds the complete LLM prompt from persona, memory, and user request.
package context

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	corecontext "github.com/openbotstack/openbotstack-core/context"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
)

// Compile-time interface compliance check.
var _ corecontext.ContextAssembler = (*RuntimeContextAssembler)(nil)

// RuntimeContextAssembler implements corecontext.ContextAssembler.
// It combines persona, memory, and skills into a complete LLM context.
type RuntimeContextAssembler struct {
	registry      agent.SkillRegistry
	memoryManager abstraction.MemoryManager
}

// NewRuntimeContextAssembler creates a new context assembler.
// registry and memoryManager may be nil; missing components are skipped gracefully.
func NewRuntimeContextAssembler(registry agent.SkillRegistry, memoryManager abstraction.MemoryManager) *RuntimeContextAssembler {
	return &RuntimeContextAssembler{
		registry:      registry,
		memoryManager: memoryManager,
	}
}

// Assemble builds the complete context for an LLM request.
func (a *RuntimeContextAssembler) Assemble(
	ctx context.Context,
	assistant corecontext.AssistantContext,
	request corecontext.UserRequest,
	conversationHistory []skills.Message,
) (*corecontext.AssembledContext, error) {
	// 1. Build system prompt from persona + base prompt
	systemPrompt := buildPersonaSystemPrompt(assistant.Persona, assistant.BaseSystemPrompt)

	// 2. Retrieve relevant memories (best-effort)
	var relevantMemories []abstraction.MemoryEntry
	if a.memoryManager != nil && request.Message != "" {
		memories, err := a.memoryManager.RetrieveSimilar(ctx, request.Message, 5)
		if err != nil {
			slog.WarnContext(ctx, "context assembler: memory retrieval failed",
				"error", err, "profile_id", assistant.ProfileID)
		} else {
			relevantMemories = memories
		}
	}

	// 3. Build messages array
	var messages []skills.Message

	// Inject memory context as system message if found
	if len(relevantMemories) > 0 {
		var memParts []string
		for _, m := range relevantMemories {
			memParts = append(memParts, "- "+m.Content)
		}
		messages = append(messages, skills.Message{
			Role:    "system",
			Content: "Relevant context from memory:\n" + strings.Join(memParts, "\n"),
		})
	}

	// Append conversation history
	messages = append(messages, conversationHistory...)

	// 4. Build available tools from enabled skills
	var availableTools []skills.ToolDefinition
	if a.registry != nil && len(assistant.EnabledSkillIDs) > 0 {
		seen := make(map[string]bool, len(assistant.EnabledSkillIDs))
		for _, id := range assistant.EnabledSkillIDs {
			if seen[id] {
				continue // deduplicate skill IDs
			}
			seen[id] = true
			s, err := a.registry.Get(id)
			if err != nil {
				slog.DebugContext(ctx, "context assembler: skill not found in registry",
					"skill_id", id, "profile_id", assistant.ProfileID)
				continue
			}
			availableTools = append(availableTools, skills.ToolDefinition{
				Name:        s.ID(),
				Description: s.Description(),
				Parameters:  s.InputSchema(),
			})
		}
	}

	return &corecontext.AssembledContext{
		SystemPrompt:    systemPrompt,
		Messages:        messages,
		AvailableTools:  availableTools,
		Constraints:     skills.ModelConstraints{}, // caller is responsible for setting limits
		RelevantMemories: relevantMemories,
	}, nil
}

// buildPersonaSystemPrompt combines persona attributes with the base system prompt.
func buildPersonaSystemPrompt(persona corecontext.Persona, basePrompt string) string {
	var sb strings.Builder

	if basePrompt != "" {
		sb.WriteString(basePrompt)
	}

	if persona.Tone != "" {
		fmt.Fprintf(&sb, "\nTone: %s", persona.Tone)
	}
	if persona.Verbosity != "" {
		fmt.Fprintf(&sb, "\nVerbosity: %s", persona.Verbosity)
	}
	if persona.Domain != "" {
		fmt.Fprintf(&sb, "\nDomain: %s", persona.Domain)
	}

	return strings.TrimSpace(sb.String())
}
