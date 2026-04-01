// Package executor implements skill execution with sandboxing.
package skill_executor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-core/control/agent"
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

// DefaultExecutor implements SkillExecutor with real Wasm execution.
type DefaultExecutor struct {
	mu      sync.RWMutex
	skills  map[string]skills.Skill
	wasm    map[string][]byte // Wasm bytes per skill
	runtime *wasm.Runtime
	tools   toolrunner.ToolRunner
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

	e.mu.RLock()
	s, exists := e.skills[req.SkillID]
	wasmBytes := e.wasm[req.SkillID]
	e.mu.RUnlock()

	if !exists {
		return &execution.ExecutionResult{
			Status:   execution.StatusFailed,
			Error:    "skill not loaded",
			Duration: time.Since(start),
		}, execution.ErrSkillNotLoaded
	}

	// Apply timeout
	timeout := req.Timeout
	if timeout == 0 {
		timeout = s.Timeout()
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
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return &execution.ExecutionResult{
					Status:   execution.StatusTimeout,
					Error:    "execution timeout",
					Duration: time.Since(start),
				}, execution.ErrExecutionTimeout
			}
			return &execution.ExecutionResult{
				Status:   execution.StatusFailed,
				Error:    err.Error(),
				Duration: time.Since(start),
			}, err
		}

		return &execution.ExecutionResult{
			Status:   execution.StatusSuccess,
			Output:   output,
			Duration: time.Since(start),
		}, nil
	}

	// Fallback for skills without Wasm (declarative skills)
	result := &execution.ExecutionResult{
		Status:   execution.StatusSuccess,
		Duration: time.Since(start),
	}

	select {
	case <-ctx.Done():
		result.Status = execution.StatusTimeout
		result.Error = "execution timeout"
		return result, execution.ErrExecutionTimeout
	default:
		// For declarative skills, return empty output (handled by agent)
		result.Output = []byte(`{"type": "declarative"}`)
	}

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
//  1. Validates the plan is not nil and has a skill ID
//  2. Serializes plan.Arguments to JSON
//  3. Calls the underlying Execute method
//
// Direct calls to Execute with raw bytes are discouraged.
func (e *DefaultExecutor) ExecuteFromPlan(ctx context.Context, plan *agent.ExecutionPlan, meta agent.ExecutionMeta) (*execution.ExecutionResult, error) {
	if plan == nil {
		return nil, ErrNilExecutionPlan
	}

	if err := plan.Validate(); err != nil {
		return &execution.ExecutionResult{
			Status: execution.StatusRejected,
			Error:  err.Error(),
		}, err
	}

	// Serialize arguments to JSON
	inputBytes, err := plan.ArgumentsJSON()
	if err != nil {
		return &execution.ExecutionResult{
			Status: execution.StatusFailed,
			Error:  "failed to serialize arguments: " + err.Error(),
		}, err
	}

	// Build ExecutionRequest from plan
	req := execution.ExecutionRequest{
		SkillID:   plan.SkillID,
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
