package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/capability"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"

	corecontext "github.com/openbotstack/openbotstack-core/context"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-runtime/harness"
	rtcontext "github.com/openbotstack/openbotstack-runtime/context"
	rtmemory "github.com/openbotstack/openbotstack-runtime/memory"
)

// skillLister is a minimal interface for listing and getting skills.
// Defined here to decouple from the full skills.SkillRegistry.
type skillLister interface {
	List() []string
	Get(id string) (skills.Skill, error)
}

// HarnessAgentConfig holds all dependencies for constructing a HarnessAgent.
type HarnessAgentConfig struct {
	// Execution pipeline
	Planner planner.ExecutionPlanner
	Harness *harness.ExecutionHarness

	// Skill resolution
	Registry      skillLister
	CapRegistry   capability.CapabilityRegistry
	SkillDisabled func(id string) bool

	// Session state (optional — nil = feature disabled)
	ConversationStore   coreagent.ConversationStore
	ContextAssembler    corecontext.ContextAssembler
	MemoryManager       abstraction.MemoryManager
	ConversationMgr     *rtmemory.ConversationManager
	ReasoningStore      harness.ReasoningStorer
	MaxHistoryMessages  int // defaults to 50 if zero

	// Permission grants for builtin tools (read_file, write_file, web_fetch).
	GrantedPermissions []string

	// Assistant identity
	Runtime          *assistant.AssistantRuntime
	WorkflowResolver WorkflowResolver
}

// HarnessAgent implements coreagent.Agent using the Execution Harness model.
type HarnessAgent struct {
	planner            planner.ExecutionPlanner
	harness            *harness.ExecutionHarness
	skillRegistry      skillLister
	capRegistry        capability.CapabilityRegistry
	skillDisabled      func(id string) bool
	conversationStore  coreagent.ConversationStore
	contextAssembler   corecontext.ContextAssembler
	memoryManager      abstraction.MemoryManager
	conversationMgr    *rtmemory.ConversationManager
	reasoningStore     harness.ReasoningStorer
	maxHistoryMessages int
	grantedPermissions []string
	runtime            *assistant.AssistantRuntime
	workflowResolver   WorkflowResolver
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
		conversationMgr:    cfg.ConversationMgr,
		workflowResolver:    cfg.WorkflowResolver,
		skillDisabled:      cfg.SkillDisabled,
		reasoningStore:     cfg.ReasoningStore,
		grantedPermissions: cfg.GrantedPermissions,
		maxHistoryMessages: maxHist,
	}
}

func (a *HarnessAgent) SetConversationStore(cs coreagent.ConversationStore) { a.conversationStore = cs }
func (a *HarnessAgent) SetMaxHistoryMessages(n int)                        { a.maxHistoryMessages = n }
func (a *HarnessAgent) SetContextAssembler(ca corecontext.ContextAssembler) { a.contextAssembler = ca }
func (a *HarnessAgent) SetMemoryManager(mm abstraction.MemoryManager)      { a.memoryManager = mm }
func (a *HarnessAgent) SetConversationManager(cm *rtmemory.ConversationManager) { a.conversationMgr = cm }

// buildPlannerContext assembles the PlannerContext: loads history, enriches via
// ContextAssembler, retrieves memory, and constructs the planning context.
// When a ConversationManager is configured, it consolidates history + memory retrieval
// into a single call to avoid duplicate RetrieveSimilar calls.
func (a *HarnessAgent) buildPlannerContext(ctx context.Context, req coreagent.MessageRequest, skillDescs []aitypes.SkillDescriptor, capDescs []capability.CapabilityDescriptor) (*planner.PlannerContext, error) {
	var conversationHistory []aitypes.Message
	var memoryEntries []abstraction.MemoryEntry

	// Use ConversationManager for consolidated retrieval when available
	if a.conversationMgr != nil && req.SessionID != "" {
		convCtx, err := a.conversationMgr.GetConversationContext(ctx, req.SessionID, req.Message, req.TenantID, req.UserID)
		if err != nil {
			slog.WarnContext(ctx, "harness_agent: conversation context loading failed", "error", err)
		} else if convCtx != nil {
			conversationHistory = convCtx.History
			memoryEntries = convCtx.MemoryEntries
		}
	} else {
		// Fallback: load history directly when ConversationManager is not configured
		if a.conversationStore != nil && req.SessionID != "" {
			conversationHistory = a.loadHistory(ctx, req)
		}

		// Fallback: retrieve memories directly
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
				memoryEntries = entries
			}
		}
	}

	// Pass pre-fetched memories to context assembler to prevent duplicate retrieval
	if a.contextAssembler != nil {
		var assembled *corecontext.AssembledContext
		var err error
		assistantCtx := corecontext.AssistantContext{
			ProfileID:       a.runtime.AssistantID,
			EnabledSkillIDs: skillIDsFromDescriptors(skillDescs),
		}
		userReq := corecontext.UserRequest{
			Message:        req.Message,
			ConversationID: req.SessionID,
			TenantID:       req.TenantID,
			UserID:         req.UserID,
		}
		if ca, ok := a.contextAssembler.(*rtcontext.RuntimeContextAssembler); ok && memoryEntries != nil {
			assembled, err = ca.AssembleWithMemories(ctx, assistantCtx, userReq, conversationHistory, memoryEntries)
		} else {
			assembled, err = a.contextAssembler.Assemble(ctx, assistantCtx, userReq, conversationHistory)
		}
		if err != nil {
			slog.WarnContext(ctx, "harness_agent: context assembly failed", "error", err)
		} else if assembled != nil && len(assembled.Messages) > 0 {
			conversationHistory = assembled.Messages
		}
	}

	var memoryContext []planner.SearchResult
	if len(memoryEntries) > 0 {
		memoryContext = make([]planner.SearchResult, len(memoryEntries))
		for i, e := range memoryEntries {
			memoryContext[i] = planner.SearchResult{
				Content: []byte(e.Content),
				Score:   1.0,
			}
		}
	}

	return &planner.PlannerContext{
		AssistantID:   a.runtime.AssistantID,
		Soul:          a.runtime.Soul,
		MemoryContext: memoryContext,
		Skills:        skillDescs,
		Capabilities:  capDescs,
		UserRequest:   req.Message,
	}, nil
}

// resolveTasks determines execution tasks via workflow decomposition,
// falling back to a single task if no workflow matches.
func (a *HarnessAgent) resolveTasks(ctx context.Context, req coreagent.MessageRequest, pCtx *planner.PlannerContext) []harness.TaskInput {
	if a.workflowResolver == nil {
		return []harness.TaskInput{{TaskDescription: req.Message, PlannerContext: pCtx}}
	}

	wf, input, resolveErr := a.workflowResolver.Resolve(req.Message)
	if resolveErr != nil {
		slog.WarnContext(ctx, "workflow resolver error, falling back to single task", "error", resolveErr)
		return []harness.TaskInput{{TaskDescription: req.Message, PlannerContext: pCtx}}
	}
	if wf == nil {
		return []harness.TaskInput{{TaskDescription: req.Message, PlannerContext: pCtx}}
	}

	tasks, err := DecomposeToTasks(wf, input, pCtx)
	if err != nil {
		slog.WarnContext(ctx, "workflow decomposition error, falling back to single task", "error", err)
		return []harness.TaskInput{{TaskDescription: req.Message, PlannerContext: pCtx}}
	}
	if len(tasks) == 0 {
		return []harness.TaskInput{{TaskDescription: req.Message, PlannerContext: pCtx}}
	}
	return tasks
}

// HandleMessage implements coreagent.Agent.
// It runs the 6-phase execution pipeline as a thin coordinator.
func (a *HarnessAgent) HandleMessage(ctx context.Context, req coreagent.MessageRequest) (*coreagent.MessageResponse, error) {
	if req.SessionID == "" {
		req.SessionID = uuid.NewString()
	}

	emit := func(eventType, content string) {
		if req.ProgressCallback != nil {
			req.ProgressCallback(eventType, content, 0, "")
		}
	}
	emit("analyzing", "正在分析请求...")
	emit("loading_context", "正在加载上下文...")

	// Phase 1: Gather skills
	skillDescriptors, capDescriptors, err := a.gatherSkillDescriptors()
	if err != nil {
		return nil, fmt.Errorf("harness_agent: failed to gather skills: %w", err)
	}
	if len(skillDescriptors) == 0 && len(capDescriptors) == 0 {
		return nil, planner.ErrNoSkillsAvailable
	}

	// Phase 2: Assemble planner context
	pCtx, err := a.buildPlannerContext(ctx, req, skillDescriptors, capDescriptors)
	if err != nil {
		return nil, fmt.Errorf("harness_agent: failed to build context: %w", err)
	}

	// Phase 3: Resolve tasks
	tasks := a.resolveTasks(ctx, req, pCtx)

	// Phase 4: Prepare execution context
	execID := uuid.NewString()
	ec := a.prepareExecutionContext(ctx, execID, req)

	emit("planning", "")
	a.propagateProgressToTasks(tasks, ec)

	// Phase 5: Execute tasks
	lastResult, message, skillUsed, execErr := a.executeTasks(ctx, tasks, ec)
	if execErr != nil {
		errMsg := fmt.Sprintf("Execution failed: %v", execErr)
		slog.WarnContext(ctx, "harness_agent: execution failed", "error", execErr)
		resp := &coreagent.MessageResponse{
			SessionID:   req.SessionID,
			Message:     errMsg,
			ExecutionID: execID,
		}
		a.storeErrorTrace(execID, req.TenantID, execErr)
		a.storeMessages(ctx, req, resp)
		return resp, nil
	}

	if message == "" {
		message = "Task completed."
	}

	resp := &coreagent.MessageResponse{
		SessionID:   req.SessionID,
		Message:     message,
		SkillUsed:   skillUsed,
		ExecutionID: execID,
	}

	// Phase 6: Finalize
	a.finalize(ctx, execID, req.TenantID, lastResult, req, resp)

	return resp, nil
}

// prepareExecutionContext builds the ExecutionContext for this request (Phase 4).
func (a *HarnessAgent) prepareExecutionContext(ctx context.Context, execID string, req coreagent.MessageRequest) *execution.ExecutionContext {
	ec := execution.NewExecutionContext(ctx, execID, a.runtime.AssistantID, req.SessionID, req.TenantID, req.UserID)
	ec.LoopMode = "harness"
	ec.GrantedPermissions = a.grantedPermissions
	if req.ProgressCallback != nil {
		ec.ProgressFn = req.ProgressCallback
	}
	return ec
}

// propagateProgressToTasks wires the execution context progress callback
// into each task's PlannerContext so plan-level events surface via SSE.
func (a *HarnessAgent) propagateProgressToTasks(tasks []harness.TaskInput, ec *execution.ExecutionContext) {
	for i := range tasks {
		if tasks[i].PlannerContext != nil && ec.ProgressFn != nil {
			tasks[i].PlannerContext.ProgressFn = func(eventType, content string) {
				ec.ProgressFn(eventType, content, 0, "")
			}
		}
	}
}

// executeTasks runs plan+execute for each task (Phase 5).
// For a single task, returns the direct result. For multiple tasks,
// aggregates into a summary. Returns (lastResult, message, skillUsed, error).
func (a *HarnessAgent) executeTasks(ctx context.Context, tasks []harness.TaskInput, ec *execution.ExecutionContext) (*harness.HarnessResult, string, string, error) {
	if len(tasks) == 1 {
		result, err := harness.PlanAndRun(ctx, a.planner, a.harness, tasks[0], ec)
		if err != nil {
			return nil, "", "", err
		}
		return result, a.extractMessage(result), a.extractSkills(result), nil
	}

	summary := "Completed tasks:\n"
	var lastResult *harness.HarnessResult
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
	return lastResult, summary, "", nil
}

// finalize stores reasoning traces and conversation messages (Phase 6).
func (a *HarnessAgent) finalize(ctx context.Context, execID, tenantID string, result *harness.HarnessResult, req coreagent.MessageRequest, resp *coreagent.MessageResponse) {
	if a.reasoningStore != nil && result != nil {
		auditTrail := stepResultsToAuditTrail(result.StepResults, execID)
		a.reasoningStore.StoreTrail(execID, auditTrail)
		trace := harness.BuildExecutionTrace(result, execID, tenantID)
		a.reasoningStore.StoreTrace(execID, trace)
	}
	a.storeMessages(ctx, req, resp)
}

// --- Helper methods ---

func (a *HarnessAgent) extractMessage(result *harness.HarnessResult) string {
	if len(result.StepResults) == 0 {
		return ""
	}
	for i := len(result.StepResults) - 1; i >= 0; i-- {
		sr := result.StepResults[i]
		if sr.Type == "tool" {
			continue
		}
		if sr.Output != nil {
			return fmt.Sprintf("%v", sr.Output)
		}
		if sr.Error != nil {
			return fmt.Sprintf("Step %q failed: %s", sr.StepName, sanitizeStepError(sr.Error))
		}
	}
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

func (a *HarnessAgent) gatherSkillDescriptors() ([]aitypes.SkillDescriptor, []capability.CapabilityDescriptor, error) {
	if a.capRegistry != nil {
		caps := a.capRegistry.List()
		descs := make([]aitypes.SkillDescriptor, 0, len(caps))
		capDescs := make([]capability.CapabilityDescriptor, 0, len(caps))
		for _, c := range caps {
			if a.skillDisabled != nil && a.skillDisabled(c.ID) {
				continue
			}
			descs = append(descs, aitypes.SkillDescriptor{
				ID:          c.ID,
				Name:        c.Name,
				Description: c.Description,
				InputSchema: c.InputSchema,
				Kind:        c.Kind,
				SourceID:    c.SourceID,
			})
			capDescs = append(capDescs, c)
		}
		return descs, capDescs, nil
	}
	ids := a.skillRegistry.List()
	descs := make([]aitypes.SkillDescriptor, 0, len(ids))
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

func (a *HarnessAgent) storeMessages(ctx context.Context, req coreagent.MessageRequest, resp *coreagent.MessageResponse) {
	if a.conversationStore == nil || req.SessionID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := a.conversationStore.AppendMessage(ctx, coreagent.SessionMessage{
		TenantID: req.TenantID, UserID: req.UserID, SessionID: req.SessionID,
		Role: "user", Content: req.Message, Timestamp: now,
	}); err != nil {
		slog.WarnContext(ctx, "harness_agent: failed to store user message", "error", err)
	}
	if err := a.conversationStore.AppendMessage(ctx, coreagent.SessionMessage{
		TenantID: req.TenantID, UserID: req.UserID, SessionID: req.SessionID,
		Role: "assistant", Content: resp.Message,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano), ExecutionID: resp.ExecutionID,
	}); err != nil {
		slog.WarnContext(ctx, "harness_agent: failed to store assistant message", "error", err)
	}
}

func (a *HarnessAgent) loadHistory(ctx context.Context, req coreagent.MessageRequest) []aitypes.Message {
	var history []aitypes.Message
	summary, err := a.conversationStore.GetSummary(ctx, req.TenantID, req.UserID, req.SessionID)
	if err == nil && summary != "" {
		history = append(history, aitypes.Message{
			Role: "system", Content: "Previous conversation summary:\n" + summary,
		})
	}
	msgs, err := a.conversationStore.GetHistory(ctx, req.TenantID, req.UserID, req.SessionID, a.maxHistoryMessages)
	if err == nil && len(msgs) > 0 {
		history = append(history, msgs...)
	}
	return history
}

func sanitizeStepError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
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
	case strings.Contains(msg, "denied by hook") || strings.Contains(msg, "denied:"):
		return "execution denied by policy"
	}
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}
