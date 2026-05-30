package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/capability"
	csSkills "github.com/openbotstack/openbotstack-core/control/skills"
	corecontext "github.com/openbotstack/openbotstack-core/context"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
	rtmemory "github.com/openbotstack/openbotstack-runtime/memory"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

// HarnessAgentConfig holds all dependencies for constructing a HarnessAgent.
type HarnessAgentConfig struct {
	// Execution pipeline
	Planner planner.ExecutionPlanner
	Harness *harness.ExecutionHarness

	// Skill resolution
	Registry     agent.SkillRegistry
	CapRegistry  capability.CapabilityRegistry
	SkillDisabled func(id string) bool

	// Session state (optional — nil = feature disabled)
	ConversationStore  agent.ConversationStore
	ContextAssembler   corecontext.ContextAssembler
	MemoryManager      abstraction.MemoryManager
	ReasoningStore     harness.ReasoningStorer
	MaxHistoryMessages int // defaults to 50 if zero

	// Permission grants for builtin tools (read_file, write_file, web_fetch).
	// Populated from config and OBS_FILE_ALLOWED_DIRS.
	GrantedPermissions []string

	// Assistant identity
	Runtime          *assistant.AssistantRuntime
	WorkflowResolver WorkflowResolver
}

// HarnessAgent implements agent.Agent using the Execution Harness model.
// It uses deterministic plan execution via ExecutionHarness + bounded reasoning via ReasoningLoop.
type HarnessAgent struct {
	// Execution pipeline: planning and execution
	planner planner.ExecutionPlanner
	harness *harness.ExecutionHarness

	// Skill resolution: what capabilities are available
	skillRegistry agent.SkillRegistry
	capRegistry   capability.CapabilityRegistry
	skillDisabled func(id string) bool

	// Session state: conversation history, context enrichment, memory
	conversationStore  agent.ConversationStore
	contextAssembler   corecontext.ContextAssembler
	memoryManager      abstraction.MemoryManager
	reasoningStore     harness.ReasoningStorer
	maxHistoryMessages int

	// Permission grants for builtin tool execution (defense-in-depth).
	grantedPermissions []string

	// Assistant identity and workflow
	runtime          *assistant.AssistantRuntime
	workflowResolver WorkflowResolver
}

// NewHarnessAgent creates a new HarnessAgent from a HarnessAgentConfig.
func NewHarnessAgent(cfg HarnessAgentConfig) *HarnessAgent {
	maxHist := cfg.MaxHistoryMessages
	if maxHist <= 0 {
		maxHist = 50
	}
	return &HarnessAgent{
		planner:            cfg.Planner,
		skillRegistry:      cfg.Registry,
		capRegistry:        cfg.CapRegistry,
		runtime:            cfg.Runtime,
		harness:            cfg.Harness,
		conversationStore:  cfg.ConversationStore,
		contextAssembler:   cfg.ContextAssembler,
		memoryManager:      cfg.MemoryManager,
		workflowResolver:    cfg.WorkflowResolver,
		skillDisabled:      cfg.SkillDisabled,
		reasoningStore:     cfg.ReasoningStore,
		grantedPermissions: cfg.GrantedPermissions,
		maxHistoryMessages: maxHist,
	}
}

// SetConversationStore sets the conversation store for history persistence.
func (a *HarnessAgent) SetConversationStore(cs agent.ConversationStore) { a.conversationStore = cs }

// SetMaxHistoryMessages sets the maximum number of history messages to load.
func (a *HarnessAgent) SetMaxHistoryMessages(n int) { a.maxHistoryMessages = n }

// SetContextAssembler sets the context assembler for history enrichment.
func (a *HarnessAgent) SetContextAssembler(ca corecontext.ContextAssembler) { a.contextAssembler = ca }

// SetMemoryManager sets the memory manager for retrieval.
func (a *HarnessAgent) SetMemoryManager(mm abstraction.MemoryManager) { a.memoryManager = mm }

// HandleMessage implements agent.Agent.
func (a *HarnessAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
	}

	// Emit progress immediately so the client knows work has started.
	// All heavy work (DB queries, context assembly, memory retrieval) runs
	// after this so the user sees instant feedback instead of a blank screen.
	emit := func(eventType, content string) {
		if req.ProgressCallback != nil {
			req.ProgressCallback(eventType, content, 0, "")
		}
	}
	emit("analyzing", "正在分析请求...")
	emit("loading_context", "正在加载上下文...")

	// Step 1: Gather available skills
	skillDescriptors, capDescriptors, err := a.gatherSkillDescriptors()
	if err != nil {
		return nil, fmt.Errorf("harness_agent: failed to gather skills: %w", err)
	}
	if len(skillDescriptors) == 0 && len(capDescriptors) == 0 {
		return nil, agent.ErrNoSkillsAvailable
	}

	// Step 2: Load conversation history
	var conversationHistory []agent.Message
	if a.conversationStore != nil && req.SessionID != "" {
		conversationHistory = a.loadHistory(ctx, req)
	}

	// Step 2.5: Enrich history via ContextAssembler
	if a.contextAssembler != nil {
		skillMsgs := agent.MessagesToSkillMsgs(conversationHistory)
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
			slog.WarnContext(ctx, "harness_agent: context assembly failed", "error", err)
		} else if assembled != nil && len(assembled.Messages) > 0 {
			conversationHistory = agent.SkillMsgsToMessages(assembled.Messages)
		}
	}

	// Step 3: Build PlannerContext with memory context
	plannerSkills := skillDescriptors

	var memoryContext []assistant.SearchResult
	if a.memoryManager != nil && req.Message != "" {
		memCtx := rtmemory.ScopeWithMemory(ctx, rtmemory.MemoryScope{
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			SessionID: req.SessionID,
		})
		entries, err := a.memoryManager.RetrieveSimilar(memCtx, req.Message, defaultMemoryRetrievalLimit)
		if err != nil {
			slog.WarnContext(ctx, "harness_agent: memory retrieval failed", "error", err)
		} else if len(entries) > 0 {
			memoryContext = make([]assistant.SearchResult, len(entries))
			for i, e := range entries {
				memoryContext[i] = assistant.SearchResult{
					Content: []byte(e.Content),
					Score:   1.0,
				}
			}
		}
	}

	pCtx := &planner.PlannerContext{
		AssistantID:   a.runtime.AssistantID,
		Soul:          a.runtime.Soul,
		MemoryContext: memoryContext,
		Skills:        plannerSkills,
		Capabilities:  capDescriptors,
		UserRequest:   req.Message,
	}

	// Step 4: Determine tasks (single or multi-task workflow)
	var tasks []harness.TaskInput
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

	if len(tasks) == 0 {
		tasks = []harness.TaskInput{
			{
				TaskDescription: req.Message,
				PlannerContext:  pCtx,
			},
		}
	}

	// Step 5: Create ExecutionContext
	execID := uuid.NewString()
	ec := execution.NewExecutionContext(ctx, execID, a.runtime.AssistantID, req.SessionID, req.TenantID, req.UserID)
	ec.LoopMode = "harness"
	ec.GrantedPermissions = a.grantedPermissions

	if req.ProgressCallback != nil {
		ec.ProgressFn = req.ProgressCallback
	}

	// Heavy work complete — signal transition to planning
	emit("planning", "")

	// Inject ProgressFn into each task's PlannerContext for streaming plan generation.
	for i := range tasks {
		if tasks[i].PlannerContext != nil && ec.ProgressFn != nil {
			tasks[i].PlannerContext.ProgressFn = func(eventType, content string) {
				ec.ProgressFn(eventType, content, 0, "")
			}
		}
	}

	// Step 6: Execute via harness
	var message string
	var skillUsed string
	var lastResult *harness.HarnessResult

	if len(tasks) == 1 {
		// Single task: direct harness execution
		result, err := harness.PlanAndRun(ctx, a.planner, a.harness, tasks[0], ec)
		if err != nil {
			// Return user-visible error instead of 500 for execution failures
			// (validation errors, timeouts, etc. are not internal errors)
			errMsg := fmt.Sprintf("Execution failed: %v", err)
			slog.WarnContext(ctx, "harness_agent: execution failed", "error", err)
			resp := &agent.MessageResponse{
				SessionID:   req.SessionID,
				Message:     errMsg,
				ExecutionID: execID,
			}
				a.storeErrorTrace(execID, req.TenantID, err)
			a.storeMessages(ctx, req, resp)
			return resp, nil
		}
		message = a.extractMessage(result)
		skillUsed = a.extractSkills(result)
		lastResult = result
	} else {
		// Multi-task: sequential execution per task
		summary := "Completed tasks:\n"
		for i, task := range tasks {
			result, err := harness.PlanAndRun(ctx, a.planner, a.harness, task, ec)
			if err != nil {
				slog.WarnContext(ctx, "harness_agent: task failed", "task", i, "error", err)
				summary += fmt.Sprintf("- Task %d: Error: %v\n", i+1, err)
				continue
			}
			msg := a.extractMessage(result)
			summary += fmt.Sprintf("- Task %d: %s\n", i+1, msg)
			lastResult = result
		}
		message = summary
	}

	// Store reasoning trail for visualization
	if a.reasoningStore != nil && lastResult != nil {
		auditTrail := stepResultsToAuditTrail(lastResult.StepResults, execID)
		a.reasoningStore.StoreTrail(execID, auditTrail)
		trace := harness.BuildExecutionTrace(lastResult, execID, req.TenantID)
		a.reasoningStore.StoreTrace(execID, trace)
	}

	if message == "" {
		message = "Task completed."
	}

	resp := &agent.MessageResponse{
		SessionID:   req.SessionID,
		Message:     message,
		SkillUsed:   skillUsed,
		ExecutionID: execID,
	}

	a.storeMessages(ctx, req, resp)
	return resp, nil
}

// extractMessage gets the primary message from a harness result.
// Only returns output from skill/llm steps — tool step outputs contain
// raw structured data meant for the trace/reasoning API, not the user bubble.
func (a *HarnessAgent) extractMessage(result *harness.HarnessResult) string {
	if len(result.StepResults) == 0 {
		return ""
	}
	// Walk backwards to find the last skill or llm step output
	for i := len(result.StepResults) - 1; i >= 0; i-- {
		sr := result.StepResults[i]
		if sr.Type == "tool" {
			continue // skip raw tool outputs
		}
		if sr.Output != nil {
			return fmt.Sprintf("%v", sr.Output)
		}
		if sr.Error != nil {
			return fmt.Sprintf("Step %q failed: %s", sr.StepName, sanitizeStepError(sr.Error))
		}
	}
	// All steps were tool calls — provide a summary instead of raw JSON
	toolNames := make([]string, 0, len(result.StepResults))
	for _, sr := range result.StepResults {
		if sr.StepName != "" && sr.Type == "tool" {
			toolNames = append(toolNames, sr.StepName)
		}
	}
	if len(toolNames) > 0 {
		return fmt.Sprintf("Completed %d tool calls: %s", len(toolNames), strings.Join(toolNames, ", "))
	}
	return ""
}

// extractSkills collects unique skill/tool names from results.
func (a *HarnessAgent) extractSkills(result *harness.HarnessResult) string {
	seen := make(map[string]bool)
	var unique []string
	for _, sr := range result.StepResults {
		if sr.StepName != "" && !seen[sr.StepName] {
			seen[sr.StepName] = true
			unique = append(unique, sr.StepName)
		}
	}
	skillUsed := ""
	for i, s := range unique {
		if i > 0 {
			skillUsed += ", "
		}
		skillUsed += s
	}
	return skillUsed
}

// storeErrorTrace creates a minimal trace for failed executions so the
// execution viewer can show the error rather than "not found".
func (a *HarnessAgent) storeErrorTrace(execID, tenantID string, execErr error) {
	if a.reasoningStore == nil {
		return
	}
	trace := &harness.ExecutionTraceData{
		ExecutionID: execID,
		TenantID:    tenantID,
		StopReason:  "error",
		StopDetail:  execErr.Error(),
		Steps: []harness.StepTraceData{{
			StepID:   execID,
			StepName: "execution",
			StepType: "llm",
			Status:   "failed",
			Error:    execErr.Error(),
		}},
	}
	a.reasoningStore.StoreTrace(execID, trace)
}

// stepResultsToAuditTrail converts HarnessResult step results to AuditEvent slice.
func stepResultsToAuditTrail(results []execution.StepResult, traceID string) []audit.AuditEvent {
	entries := make([]audit.AuditEvent, 0, len(results))
	for _, sr := range results {
		entry := audit.AuditEvent{
			Source:     audit.SourceExecutor,
			TraceID:    traceID,
			StepID:     sr.StepID,
			StepName:   sr.StepName,
			StepType:   sr.Type,
			Status:     "completed",
			ToolOutput: sr.Output,
			Duration:   sr.Duration,
		}
		if sr.Error != nil {
			entry.Status = "error"
			entry.Error = sr.Error.Error()
		}
		entries = append(entries, entry)
	}
	return entries
}

// gatherSkillDescriptors builds skill descriptors from the registry.
// When capRegistry is available, it also returns CapabilityDescriptors
// preserving Kind information for the planner pipeline.
func (a *HarnessAgent) gatherSkillDescriptors() ([]csSkills.SkillDescriptor, []capability.CapabilityDescriptor, error) {
	if a.capRegistry != nil {
		caps := a.capRegistry.List()
		descs := make([]csSkills.SkillDescriptor, 0, len(caps))
		capDescs := make([]capability.CapabilityDescriptor, 0, len(caps))
		for _, c := range caps {
			if a.skillDisabled != nil && a.skillDisabled(c.ID) {
				continue
			}
			descs = append(descs, csSkills.SkillDescriptor{
				ID:          c.ID,
				Name:        c.Name,
				Description: c.Description,
				InputSchema: c.InputSchema,
			})
			capDescs = append(capDescs, c)
		}
		return descs, capDescs, nil
	}
	// Fallback: original skill registry path
	ids := a.skillRegistry.List()
	descs := make([]csSkills.SkillDescriptor, 0, len(ids))
	for _, id := range ids {
		if a.skillDisabled != nil && a.skillDisabled(id) {
			continue
		}
		s, err := a.skillRegistry.Get(id)
		if err != nil || s == nil {
			continue
		}
		descs = append(descs, skillToDescriptor(id, s))
	}
	return descs, nil, nil
}

// storeMessages persists user message and assistant response.
func (a *HarnessAgent) storeMessages(ctx context.Context, req agent.MessageRequest, resp *agent.MessageResponse) {
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
		slog.WarnContext(ctx, "harness_agent: failed to store user message", "error", err)
	}

	if err := a.conversationStore.AppendMessage(ctx, agent.SessionMessage{
		TenantID:    req.TenantID,
		UserID:      req.UserID,
		SessionID:   req.SessionID,
		Role:        "assistant",
		Content:     resp.Message,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
		ExecutionID: resp.ExecutionID,
	}); err != nil {
		slog.WarnContext(ctx, "harness_agent: failed to store assistant message", "error", err)
	}
}

// loadHistory retrieves conversation history + summary.
func (a *HarnessAgent) loadHistory(ctx context.Context, req agent.MessageRequest) []agent.Message {
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

// sanitizeStepError returns a user-safe error message.
// Internal details (LLM provider errors, auth failures, connection strings)
// are replaced with generic descriptions to avoid leaking sensitive info.
func sanitizeStepError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// Map known internal error patterns to user-safe messages.
	switch {
	case strings.Contains(msg, "authentication failed"):
		return "LLM provider authentication error"
	case strings.Contains(msg, "connection refused"):
		return "LLM provider connection error"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "execution timed out"
	case strings.Contains(msg, "context canceled"):
		return "execution was cancelled"
	case strings.Contains(msg, "no tool runner configured"):
		return "tool execution is not available"
	case strings.Contains(msg, "no skill executor configured"):
		return "skill execution is not available"
	case strings.Contains(msg, "no reasoning loop configured"):
		return "LLM reasoning is not available"
	case strings.Contains(msg, "denied by hook"):
		return "execution denied by policy"
	case strings.Contains(msg, "denied:"):
		return "execution denied by policy"
	}

	// Default: return the error as-is for unexpected errors (for debuggability).
	// In production, consider truncating to a max length.
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}
