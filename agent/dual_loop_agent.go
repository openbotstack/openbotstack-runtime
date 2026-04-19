package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	csSkills "github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/assistant"
	corecontext "github.com/openbotstack/openbotstack-core/context"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-runtime/loop"
)

// DualLoopAgent implements agent.Agent using the dual bounded loop kernel.
// It delegates task execution to the outer/inner loop pair instead of the
// single-pass Plan→Execute pipeline used by DefaultAgent.
type DualLoopAgent struct {
	planner           planner.ExecutionPlanner
	skillRegistry     agent.SkillRegistry
	runtime           *assistant.AssistantRuntime
	innerLoop         loop.InnerLoop
	outerLoop         loop.OuterLoop
	conversationStore agent.ConversationStore
	contextAssembler  corecontext.ContextAssembler
	maxHistoryMessages int
	workflowResolver  WorkflowResolver
}

// NewDualLoopAgent creates a new DualLoopAgent with the given dependencies.
func NewDualLoopAgent(
	plannerExec planner.ExecutionPlanner,
	registry agent.SkillRegistry,
	runtime *assistant.AssistantRuntime,
	innerLoop loop.InnerLoop,
	outerLoop loop.OuterLoop,
) *DualLoopAgent {
	return &DualLoopAgent{
		planner:            plannerExec,
		skillRegistry:      registry,
		runtime:            runtime,
		innerLoop:          innerLoop,
		outerLoop:          outerLoop,
		maxHistoryMessages: 50,
	}
}

// SetConversationStore configures the conversation memory backend.
func (a *DualLoopAgent) SetConversationStore(store agent.ConversationStore) {
	a.conversationStore = store
}

// SetContextAssembler configures the context assembler for pre-planning enrichment.
func (a *DualLoopAgent) SetContextAssembler(ca corecontext.ContextAssembler) {
	a.contextAssembler = ca
}

// SetMaxHistoryMessages sets the maximum number of recent messages to load.
func (a *DualLoopAgent) SetMaxHistoryMessages(n int) {
	a.maxHistoryMessages = n
}

// SetWorkflowResolver configures the workflow resolver for multi-task matching.
func (a *DualLoopAgent) SetWorkflowResolver(r WorkflowResolver) {
	a.workflowResolver = r
}

// HandleMessage implements agent.Agent.
func (a *DualLoopAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	// Step 1: Gather available skills
	skillDescriptors, err := a.gatherSkillDescriptors()
	if err != nil {
		return nil, fmt.Errorf("dual_loop_agent: failed to gather skills: %w", err)
	}
	if len(skillDescriptors) == 0 {
		return nil, agent.ErrNoSkillsAvailable
	}

	// Step 2: Load conversation history (best-effort)
	var conversationHistory []agent.Message
	if a.conversationStore != nil && req.SessionID != "" {
		conversationHistory = a.loadHistory(ctx, req)
	}

	// Step 2.5: Enrich history via ContextAssembler (best-effort)
	if a.contextAssembler != nil {
		skillMsgs := agentMsgsToSkillMsgs(conversationHistory)
		assembled, err := a.contextAssembler.Assemble(ctx,
			corecontext.AssistantContext{
				ProfileID:       a.runtime.AssistantID,
				EnabledSkillIDs: skillIDsFromDescriptors(skillDescriptors),
			},
			corecontext.UserRequest{
				Message:        req.Message,
				ConversationID: req.SessionID,
				TenantID:       req.TenantID,
				UserID:         req.UserID,
			},
			skillMsgs,
		)
		if err != nil {
			slog.WarnContext(ctx, "dual_loop_agent: context assembly failed", "error", err)
		} else if assembled != nil && len(assembled.Messages) > 0 {
			conversationHistory = skillMsgsToAgentMsgs(assembled.Messages)
		}
	}

	// Step 3: Build PlannerContext
	plannerSkills := agentSkillToPlannerSkill(skillDescriptors)
	pCtx := &planner.PlannerContext{
		AssistantID:   a.runtime.AssistantID,
		Soul:          a.runtime.Soul,
		MemoryContext: nil, // V1: empty (MemoryContext requires MemoryManager.RetrieveSimilar integration)
		Skills:        plannerSkills,
		UserRequest:   req.Message,
	}

	// Step 4: Determine tasks (single or multi-task workflow)
	var tasks []loop.TaskInput
	if a.workflowResolver != nil {
		wf, input, resolveErr := a.workflowResolver.Resolve(req.Message)
		if resolveErr != nil {
			slog.WarnContext(ctx, "workflow resolver error, falling back to single task", "error", resolveErr)
		} else if wf != nil {
			tasks, err = DecomposeToTasks(wf, input, pCtx)
			if err != nil {
				slog.WarnContext(ctx, "workflow decomposition error, falling back to single task", "error", err)
			}
		}
	}

	// Fallback: single task
	if len(tasks) == 0 {
		tasks = []loop.TaskInput{
			{
				TaskDescription: req.Message,
				PlannerContext:  pCtx,
			},
		}
	}

	// Step 4: Create ExecutionContext
	ec := execution.NewExecutionContext(ctx, "", a.runtime.AssistantID, req.SessionID, req.TenantID, req.UserID)
	ec.LoopMode = "dual_loop"

	// Step 5: Run through the dual loop
	workflowResult, err := a.outerLoop.Run(ctx, tasks, ec)
	if err != nil {
		// Store messages even on error (best-effort)
		a.storeMessages(ctx, req, &agent.MessageResponse{
			SessionID: req.SessionID,
			Message:   fmt.Sprintf("Error: %v", err),
		})
		return nil, err
	}

	// Step 6: Convert WorkflowResult → MessageResponse
	resp := a.convertResult(workflowResult, req)

	// Step 7: Store messages (best-effort)
	a.storeMessages(ctx, req, resp)

	return resp, nil
}

// gatherSkillDescriptors builds skill descriptors from the registry.
func (a *DualLoopAgent) gatherSkillDescriptors() ([]agent.SkillDescriptor, error) {
	ids := a.skillRegistry.List()
	descs := make([]agent.SkillDescriptor, 0, len(ids))
	for _, id := range ids {
		s, err := a.skillRegistry.Get(id)
		if err != nil || s == nil {
			continue
		}
		descs = append(descs, skillToDescriptor(id, s))
	}
	return descs, nil
}

// skillToDescriptor extracts descriptor fields from a Skill.
func skillToDescriptor(id string, s skills.Skill) agent.SkillDescriptor {
	return agent.SkillDescriptor{
		ID:          s.ID(),
		Name:        s.Name(),
		Description: s.Description(),
		InputSchema: func() *csSkills.JSONSchema {
			if schema := s.InputSchema(); schema != nil {
				return schema
			}
			return nil
		}(),
	}
}

// convertResult maps a WorkflowResult to a MessageResponse.
func (a *DualLoopAgent) convertResult(wr *loop.WorkflowResult, req agent.MessageRequest) *agent.MessageResponse {
	// Collect the final message from all task results
	var message string
	var allSkills []string

	for _, tr := range wr.TaskResults {
		for _, turn := range tr.TurnResults {
			allSkills = append(allSkills, turn.ActionsExecuted...)
			// Use the last observation as the message
			for _, obs := range turn.Observations {
				message = obs
			}
		}
	}

	if message == "" {
		message = "Task completed."
	}

	// Build SkillUsed (comma-separated unique skills)
	skillUsed := ""
	if len(allSkills) > 0 {
		seen := make(map[string]bool)
		unique := make([]string, 0, len(allSkills))
		for _, s := range allSkills {
			if !seen[s] {
				seen[s] = true
				unique = append(unique, s)
			}
		}
		for i, s := range unique {
			if i > 0 {
				skillUsed += ", "
			}
			skillUsed += s
		}
	}

	// Log stop condition for observability (not exposed to user)
	slog.InfoContext(context.Background(), "dual loop completed",
		"stop_condition", wr.StopCondition.Reason,
		"tasks", len(wr.TaskResults),
		"total_turns", wr.Metrics.TotalTurns,
		"total_tool_calls", wr.Metrics.TotalToolCalls,
		"total_runtime", wr.Metrics.TotalRuntime,
	)

	return &agent.MessageResponse{
		SessionID: req.SessionID,
		Message:   message,
		SkillUsed: skillUsed,
	}
}

// storeMessages persists user message and assistant response (best-effort).
func (a *DualLoopAgent) storeMessages(ctx context.Context, req agent.MessageRequest, resp *agent.MessageResponse) {
	if a.conversationStore == nil || req.SessionID == "" {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	if err := a.conversationStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Role:      "user",
		Content:   req.Message,
		Timestamp: now,
	}); err != nil {
		slog.WarnContext(ctx, "dual_loop_agent: failed to store user message", "error", err)
	}

	if err := a.conversationStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Role:      "assistant",
		Content:   resp.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		slog.WarnContext(ctx, "dual_loop_agent: failed to store assistant message", "error", err)
	}
}

// loadHistory retrieves conversation history + summary (mirrors DefaultAgent logic).
func (a *DualLoopAgent) loadHistory(ctx context.Context, req agent.MessageRequest) []agent.Message {
	var history []agent.Message

	summary, err := a.conversationStore.GetSummary(ctx, req.TenantID, req.UserID, req.SessionID)
	if err == nil && summary != "" {
		history = append(history, agent.Message{
			Role:    "system",
			Content: "Previous conversation summary:\n" + summary,
		})
	}

	msgs, err := a.conversationStore.GetHistory(ctx, req.TenantID, req.UserID, req.SessionID, a.maxHistoryMessages)
	if err == nil && len(msgs) > 0 {
		history = append(history, msgs...)
	}

	return history
}

// skillIDsFromDescriptors extracts skill IDs from descriptors.
func skillIDsFromDescriptors(descs []agent.SkillDescriptor) []string {
	ids := make([]string, 0, len(descs))
	for _, d := range descs {
		ids = append(ids, d.ID)
	}
	return ids
}

// agentMsgsToSkillMsgs converts agent.Message to skills.Message (mirrors DefaultAgent).
func agentMsgsToSkillMsgs(msgs []agent.Message) []csSkills.Message {
	result := make([]csSkills.Message, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, csSkills.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return result
}

// skillMsgsToAgentMsgs converts skills.Message to agent.Message (mirrors DefaultAgent).
func skillMsgsToAgentMsgs(msgs []csSkills.Message) []agent.Message {
	result := make([]agent.Message, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, agent.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return result
}
