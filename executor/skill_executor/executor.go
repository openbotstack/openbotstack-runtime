// Package executor implements skill execution with sandboxing.
package skill_executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-core/validation"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

var (
	// ErrSkillAlreadyLoaded is returned when loading a skill that's already loaded.
	ErrSkillAlreadyLoaded = errors.New("executor: skill already loaded")

	// ErrSkillNotFound is returned when skill doesn't exist.
	ErrSkillNotFound = errors.New("executor: skill not found")

	// ErrNilSkill is returned when skill is nil.
	ErrNilSkill = errors.New("executor: nil skill")

	// ErrEmptySkillID is returned when skill ID is empty.
	ErrEmptySkillID = errors.New("executor: empty skill ID")

	// ErrNoWasmBytes is returned when skill has no Wasm module.
	ErrNoWasmBytes = errors.New("executor: skill has no wasm bytes")
)

// WasmSkill extends skills.Skill with Wasm bytes.
type WasmSkill interface {
	skills.Skill
	WasmBytes() []byte
}

// TextGenerator generates text from a prompt using an LLM.
// Used for declarative skills that have no Wasm binary.
type TextGenerator interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
}

// StreamingTextGenerator extends TextGenerator with token-level streaming.
// The executor type-asserts TextGenerator to check for streaming support.
type StreamingTextGenerator interface {
	TextGenerator
	GenerateStreamText(ctx context.Context, prompt string, tokenFn func(string)) (string, error)
}

// DefaultExecutor implements SkillExecutor with real Wasm execution.
type DefaultExecutor struct {
	mu            sync.RWMutex
	skills        map[string]skills.Skill
	wasm          map[string][]byte // Wasm bytes per skill
	runtime       *wasm.Runtime
	tools         toolrunner.ToolRunner
	auditLogger   execution_logs.AuditLogger
	textGenerator TextGenerator // optional: for declarative (non-Wasm) skills
}

// NewDefaultExecutor creates a new executor.
func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{
		skills: make(map[string]skills.Skill),
		wasm:   make(map[string][]byte),
	}
}

// NewDefaultExecutorWithRuntime creates an executor with Wasm execution.
func NewDefaultExecutorWithRuntime(rt *wasm.Runtime, tools toolrunner.ToolRunner) *DefaultExecutor {
	return &DefaultExecutor{
		skills:  make(map[string]skills.Skill),
		wasm:    make(map[string][]byte),
		runtime: rt,
		tools:   tools,
	}
}

// SetToolRunner updates the tool runner.
func (e *DefaultExecutor) SetToolRunner(tools toolrunner.ToolRunner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tools = tools
}

// SetAuditLogger sets the audit logger for execution events.
// If called with nil, audit logging is disabled (safe to call).
func (e *DefaultExecutor) SetAuditLogger(l execution_logs.AuditLogger) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.auditLogger = l
}

// SetTextGenerator sets the LLM text generator for declarative skill execution.
// If nil, declarative skills will return a placeholder response.
func (e *DefaultExecutor) SetTextGenerator(tg TextGenerator) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.textGenerator = tg
}

// LoadSkill prepares a skill for execution.
func (e *DefaultExecutor) LoadSkill(ctx context.Context, s skills.Skill) error {
	if s == nil {
		return ErrNilSkill
	}

	if s.ID() == "" {
		return ErrEmptySkillID
	}

	if err := s.Validate(); err != nil {
		return fmt.Errorf("executor: invalid skill: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.skills[s.ID()]; exists {
		return ErrSkillAlreadyLoaded
	}

	e.skills[s.ID()] = s

	// If skill provides Wasm bytes, store them
	if ws, ok := s.(WasmSkill); ok {
		e.wasm[s.ID()] = ws.WasmBytes()
	}

	return nil
}

// LoadSkillWithWasm loads a skill with its Wasm module bytes.
func (e *DefaultExecutor) LoadSkillWithWasm(ctx context.Context, s skills.Skill, wasmBytes []byte) error {
	if s == nil {
		return ErrNilSkill
	}

	if s.ID() == "" {
		return ErrEmptySkillID
	}

	if err := s.Validate(); err != nil {
		return fmt.Errorf("executor: invalid skill: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.skills[s.ID()]; exists {
		return ErrSkillAlreadyLoaded
	}

	e.skills[s.ID()] = s
	if len(wasmBytes) > 0 {
		e.wasm[s.ID()] = wasmBytes
	}

	return nil
}

// UnloadSkill removes a skill from execution.
func (e *DefaultExecutor) UnloadSkill(ctx context.Context, skillID string) error {
	if skillID == "" {
		return ErrEmptySkillID
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.skills[skillID]; !exists {
		return ErrSkillNotFound
	}

	delete(e.skills, skillID)
	delete(e.wasm, skillID)
	return nil
}

// GetSkill returns a loaded skill by ID.
func (e *DefaultExecutor) GetSkill(ctx context.Context, skillID string) (skills.Skill, error) {
	if skillID == "" {
		return nil, ErrEmptySkillID
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	s, exists := e.skills[skillID]
	if !exists {
		return nil, ErrSkillNotFound
	}
	return s, nil
}

// ListSkills returns all loaded skill IDs.
func (e *DefaultExecutor) ListSkills(ctx context.Context) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ids := make([]string, 0, len(e.skills))
	for id := range e.skills {
		ids = append(ids, id)
	}
	return ids
}

// CanExecute checks if the skill can be executed.
func (e *DefaultExecutor) CanExecute(ctx context.Context, skillID string) (bool, error) {
	if skillID == "" {
		return false, ErrEmptySkillID
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	_, exists := e.skills[skillID]
	return exists, nil
}

// Execute runs a skill with the given input.
func (e *DefaultExecutor) Execute(ctx context.Context, req execution.ExecutionRequest) (*execution.ExecutionResult, error) {
	start := time.Now()

	if req.SkillID == "" {
		return &execution.ExecutionResult{
			Status:   execution.StatusFailed,
			Error:    "empty skill ID",
			Duration: time.Since(start),
		}, ErrEmptySkillID
	}

	// Track outcome for metrics
	execStatus := "success"
	defer func() {
		durationMs := float64(time.Since(start).Milliseconds())
		observability.RecordSkillExecution(ctx, req.SkillID, execStatus, durationMs)
	}()

	// Snapshot audit logger under read lock
	e.mu.RLock()
	al := e.auditLogger
	e.mu.RUnlock()

	// Emit audit: started
	e.emitAudit(ctx, al, audit.AuditEvent{
		ID:        uuid.NewString(),
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		RequestID: req.RequestID,
		Source:    audit.SourceExecutor,
		Action:    "skills.execute",
		Resource:  "skill/" + req.SkillID,
		Outcome:   "started",
		Timestamp: start,
	})

	e.mu.RLock()
	s, exists := e.skills[req.SkillID]
	wasmBytes := e.wasm[req.SkillID]
	e.mu.RUnlock()

	if !exists {
		loadedIDs := make([]string, 0)
		e.mu.RLock()
		for id := range e.skills {
			loadedIDs = append(loadedIDs, id)
		}
		e.mu.RUnlock()
		slog.Warn("skill not found in executor", "requested", req.SkillID, "loaded", loadedIDs)
	}

	mode := skills.GetExecutionMode(s)

	if !exists {
		elapsed := time.Since(start)
		e.emitAudit(ctx, al, audit.AuditEvent{
			ID:        uuid.NewString(),
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			RequestID: req.RequestID,
			Source:    audit.SourceExecutor,
			Action:    "skills.execute",
			Resource:  "skill/" + req.SkillID,
			Outcome:   "failure",
			Duration:  elapsed,
			Metadata:  map[string]string{"error": "skill not loaded"},
			Timestamp: time.Now(),
		})
		execStatus = "failed"
		return &execution.ExecutionResult{
			Status:   execution.StatusFailed,
			Error:    "skill not loaded",
			Duration: elapsed,
		}, execution.ErrSkillNotLoaded
	}

	// Validate input against skill schema
	if err := validation.ValidateInput(req.Input, s.InputSchema()); err != nil {
		elapsed := time.Since(start)
		execStatus = "rejected"
		e.emitAudit(ctx, al, audit.AuditEvent{
			ID:        uuid.NewString(),
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			RequestID: req.RequestID,
			Source:    audit.SourceExecutor,
			Action:    "skills.execute",
			Resource:  "skill/" + req.SkillID,
			Outcome:   "rejected",
			Duration:  elapsed,
			Metadata:  map[string]string{"error": err.Error(), "execution_mode": mode},
			Timestamp: time.Now(),
		})
		return &execution.ExecutionResult{
			Status:   execution.StatusRejected,
			Error:    "input validation failed: " + err.Error(),
			Duration: elapsed,
		}, err
	}

	// Apply timeout
	timeout := req.Timeout
	if timeout == 0 {
		timeout = s.Timeout()
	}
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute via Wasm runtime if available
	if e.runtime != nil && len(wasmBytes) > 0 {
		limits := wasm.Limits{
			MaxExecutionTime: timeout,
			MaxMemoryBytes:   128 * 1024 * 1024,
		}

		output, err := e.runtime.Execute(ctx, wasmBytes, req.Input, limits)
		elapsed := time.Since(start)

		if err != nil {
			// Wasm execution failed — try LLM fallback if available
			e.mu.RLock()
			tg := e.textGenerator
			e.mu.RUnlock()

			if ctx.Err() != context.DeadlineExceeded && tg != nil {
				// Only fallback to LLM for skills that declare LLM permission
				hasLLMPerm := false
				for _, p := range s.RequiredPermissions() {
					if p == "llm:generate" {
						hasLLMPerm = true
						break
					}
				}
				if !hasLLMPerm {
					goto wasmError
				}
				slog.WarnContext(ctx, "wasm execution failed, falling back to LLM",
					"skill_id", req.SkillID, "error", err)
				var fallbackPrompt string
				if p := skills.GetPrompt(s); p != "" {
					fallbackPrompt = strings.ReplaceAll(p, "{{.Input}}", string(req.Input))
				} else {
					fallbackPrompt = fmt.Sprintf("You are performing the skill: %s.\n\n%s\n\nUser input:\n%s",
						s.Name(), s.Description(), string(req.Input))
				}
				llmCtx, llmCancel := context.WithTimeout(ctx, timeout)
				llmOutput, llmErr := tg.GenerateText(llmCtx, fallbackPrompt)
				llmCancel()
				if llmErr == nil {
					e.emitAudit(ctx, al, audit.AuditEvent{
						ID:        uuid.NewString(),
						TenantID:  req.TenantID,
						UserID:    req.UserID,
						RequestID: req.RequestID,
						Source:    audit.SourceExecutor,
						Action:    "skills.execute",
						Resource:  "skill/" + req.SkillID,
						Outcome:   "success",
						Duration:  time.Since(start),
						Metadata:  map[string]string{"fallback": "llm", "execution_mode": mode},
						Timestamp: time.Now(),
					})
					return &execution.ExecutionResult{
						Status:   execution.StatusSuccess,
						Output:   []byte(llmOutput),
						Duration: time.Since(start),
					}, nil
				}
				// LLM also failed — return original Wasm error
				slog.WarnContext(ctx, "LLM fallback also failed", "error", llmErr)
			}

		wasmError:
			outcome := "failure"
			execStatus = "failed"
			if ctx.Err() == context.DeadlineExceeded {
				outcome = "timeout"
				execStatus = "timeout"
				e.emitAudit(ctx, al, audit.AuditEvent{
					ID:        uuid.NewString(),
					TenantID:  req.TenantID,
					UserID:    req.UserID,
					RequestID: req.RequestID,
					Source:    audit.SourceExecutor,
					Action:    "skills.execute",
					Resource:  "skill/" + req.SkillID,
					Outcome:   outcome,
					Duration:  elapsed,
					Metadata:  map[string]string{"error": "execution timeout", "execution_mode": mode},
					Timestamp: time.Now(),
				})
				return &execution.ExecutionResult{
					Status:   execution.StatusTimeout,
					Error:    "execution timeout",
					Duration: elapsed,
				}, execution.ErrExecutionTimeout
			}
			e.emitAudit(ctx, al, audit.AuditEvent{
				ID:        uuid.NewString(),
				TenantID:  req.TenantID,
				UserID:    req.UserID,
				RequestID: req.RequestID,
				Source:    audit.SourceExecutor,
				Action:    "skills.execute",
				Resource:  "skill/" + req.SkillID,
				Outcome:   outcome,
				Duration:  elapsed,
				Metadata:  map[string]string{"error": err.Error(), "execution_mode": mode},
				Timestamp: time.Now(),
			})
			return &execution.ExecutionResult{
				Status:   execution.StatusFailed,
				Error:    err.Error(),
				Duration: elapsed,
			}, err
		}

		e.emitAudit(ctx, al, audit.AuditEvent{
			ID:        uuid.NewString(),
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			RequestID: req.RequestID,
			Source:    audit.SourceExecutor,
			Action:    "skills.execute",
			Resource:  "skill/" + req.SkillID,
			Outcome:   "success",
			Duration:  elapsed,
			Metadata:  map[string]string{"execution_mode": mode},
			Timestamp: time.Now(),
		})
		return &execution.ExecutionResult{
			Status:   execution.StatusSuccess,
			Output:   output,
			Duration: elapsed,
		}, nil
	}

	// Non-Wasm path: only allowed for declarative skills
	if mode != "declarative" {
		elapsed := time.Since(start)
		execStatus = "failed"
		e.emitAudit(ctx, al, audit.AuditEvent{
			ID:        uuid.NewString(),
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			RequestID: req.RequestID,
			Source:    audit.SourceExecutor,
			Action:    "skills.execute",
			Resource:  "skill/" + req.SkillID,
			Outcome:   "failure",
			Duration:  elapsed,
			Metadata:  map[string]string{"error": fmt.Sprintf("skill requires %s execution but no binary available", mode), "execution_mode": mode},
			Timestamp: time.Now(),
		})
		return &execution.ExecutionResult{
			Status:   execution.StatusFailed,
			Error:    fmt.Sprintf("skill requires %s execution but no binary available", mode),
			Duration: elapsed,
		}, fmt.Errorf("skill %s requires %s execution but no binary available", req.SkillID, mode)
	}

	// Declarative skill execution path (LLM-based)
	result := &execution.ExecutionResult{
		Status:   execution.StatusSuccess,
		Duration: time.Since(start),
	}

	// Try LLM-based execution for declarative skills
	e.mu.RLock()
	tg := e.textGenerator
	e.mu.RUnlock()

	if tg != nil {
		// Build prompt: use SKILL.md content if available, else generic
		var prompt string
		if p := skills.GetPrompt(s); p != "" {
			prompt = strings.ReplaceAll(p, "{{.Input}}", string(req.Input))
		} else {
			slog.WarnContext(ctx, "declarative skill has no SKILL.md; using generic fallback",
				"skill_id", req.SkillID)
			prompt = fmt.Sprintf("You are performing the skill: %s.\n\n%s\n\nUser input:\n%s",
				s.Name(), s.Description(), string(req.Input))
		}
		var output string
		var err error
		if req.TokenFn != nil {
			if stg, ok := tg.(StreamingTextGenerator); ok {
				output, err = stg.GenerateStreamText(ctx, prompt, req.TokenFn)
			} else {
				output, err = tg.GenerateText(ctx, prompt)
				if err == nil && output != "" {
					req.TokenFn(output)
				}
			}
		} else {
			output, err = tg.GenerateText(ctx, prompt)
		}
		if err != nil {
			result.Status = execution.StatusFailed
			execStatus = "failed"
			result.Error = "LLM generation failed: " + err.Error()
			e.emitAudit(ctx, al, audit.AuditEvent{
				ID:        uuid.NewString(),
				TenantID:  req.TenantID,
				UserID:    req.UserID,
				RequestID: req.RequestID,
				Source:    audit.SourceExecutor,
				Action:    "skills.execute",
				Resource:  "skill/" + req.SkillID,
				Outcome:   "failure",
				Duration:  time.Since(start),
				Metadata:  map[string]string{"error": err.Error(), "execution_mode": mode},
				Timestamp: time.Now(),
			})
			return result, err
		}
		result.Output = []byte(output)
		result.Duration = time.Since(start)
	} else {
		result.Status = execution.StatusFailed
		result.Error = "declarative skill requires LLM but no text generator configured"
		e.emitAudit(ctx, al, audit.AuditEvent{
			ID:        uuid.NewString(),
			TenantID:  req.TenantID,
			UserID:    req.UserID,
			RequestID: req.RequestID,
			Source:    audit.SourceExecutor,
			Action:    "skills.execute",
			Resource:  "skill/" + req.SkillID,
			Outcome:   "failure",
			Duration:  time.Since(start),
			Metadata:  map[string]string{"error": "no LLM configured", "execution_mode": mode},
			Timestamp: time.Now(),
		})
		return result, fmt.Errorf("declarative skill %q requires LLM but no text generator is configured", req.SkillID)
	}

	e.emitAudit(ctx, al, audit.AuditEvent{
		ID:        uuid.NewString(),
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		RequestID: req.RequestID,
		Source:    audit.SourceExecutor,
		Action:    "skills.execute",
		Resource:  "skill/" + req.SkillID,
		Outcome:   "success",
		Duration:  result.Duration,
		Metadata:  map[string]string{"execution_mode": mode},
		Timestamp: time.Now(),
	})
	return result, nil
}

// SkillCount returns the number of loaded skills.
func (e *DefaultExecutor) SkillCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.skills)
}

// ErrNilExecutionPlan is returned when execution plan is nil.
var ErrNilExecutionPlan = errors.New("executor: nil execution plan")

// ExecuteFromPlan executes a validated execution plan.
//
// This is the PREFERRED entry point for skill execution. It:
//  1. Validates the plan is not nil and has at least one step
//  2. Extracts the skill ID and arguments from the first step
//  3. Calls the underlying Execute method
//
// Direct calls to Execute with raw bytes are discouraged.
func (e *DefaultExecutor) ExecuteFromPlan(ctx context.Context, plan *execution.ExecutionPlan, meta agent.ExecutionMeta) (*execution.ExecutionResult, error) {
	if plan == nil {
		return nil, ErrNilExecutionPlan
	}

	// Plan must be validated and frozen by the caller (agent or harness).
	if plan.IsFrozen() {
		if len(plan.Steps) == 0 {
			return &execution.ExecutionResult{
				Status: execution.StatusRejected,
				Error:  "empty execution plan",
			}, fmt.Errorf("executor: empty execution plan")
		}
	} else {
		// Not frozen yet — validate and freeze (backward compat for direct callers)
		if err := plan.Validate(); err != nil {
			return &execution.ExecutionResult{
				Status: execution.StatusRejected,
				Error:  err.Error(),
			}, err
		}
	}

	// Extract first step's skill ID and arguments
	step := plan.Steps[0]

	// Serialize arguments to JSON
	inputBytes, err := step.ArgumentsJSON()
	if err != nil {
		return &execution.ExecutionResult{
			Status: execution.StatusFailed,
			Error:  "failed to serialize arguments: " + err.Error(),
		}, err
	}

	// Build ExecutionRequest from plan
	req := execution.ExecutionRequest{
		SkillID:   step.Name,
		Input:     inputBytes,
		TenantID:  meta.TenantID,
		UserID:    meta.UserID,
		RequestID: meta.RequestID,
	}

	return e.Execute(ctx, req)
}

// ExecutePlan runs a multi-step execution plan using a StepRunner.
func (e *DefaultExecutor) ExecutePlan(ctx context.Context, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) error {
	if plan == nil {
		return ErrNilExecutionPlan
	}

	runner := NewStepRunner(e, e.tools)
	for _, step := range plan.Steps {
		// Check for context cancellation
		if err := ctx.Err(); err != nil {
			return err
		}

		_, err := runner.RunStep(ctx, ec, step)
		if err != nil {
			// In request-scoped ephemeral execution, we stop on the first error
			return fmt.Errorf("step %s failed: %w", step.Name, err)
		}
	}

	return nil
}

// List returns all loaded skill IDs.
func (e *DefaultExecutor) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.skills))
	for id := range e.skills {
		ids = append(ids, id)
	}
	return ids
}

// Get retrieves a skill by ID.
func (e *DefaultExecutor) Get(id string) (skills.Skill, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, exists := e.skills[id]
	if !exists {
		return nil, ErrSkillNotFound
	}
	return s, nil
}

// emitAudit logs an audit event if the logger is non-nil.
// Errors are silently ignored to avoid disrupting execution flow.
func (e *DefaultExecutor) emitAudit(ctx context.Context, al execution_logs.AuditLogger, event audit.AuditEvent) {
	if al == nil {
		return
	}
	_ = al.Log(ctx, event) //nolint:errcheck // audit failures must not disrupt execution
}
