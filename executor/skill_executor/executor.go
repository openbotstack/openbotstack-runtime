// Package executor implements skill execution with sandboxing.
package skill_executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-core/validation"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/observability"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

var (
	ErrSkillAlreadyLoaded = errors.New("executor: skill already loaded")
	ErrSkillNotFound      = errors.New("executor: skill not found")
	ErrNilSkill           = errors.New("executor: nil skill")
	ErrEmptySkillID       = errors.New("executor: empty skill ID")
	ErrNoWasmBytes        = errors.New("executor: skill has no wasm bytes")
)

// WasmSkill extends skills.Skill with Wasm bytes.
type WasmSkill interface {
	skills.Skill
	WasmBytes() []byte
}

// TextGenerator generates text from a prompt using an LLM.
type TextGenerator interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
}

// StreamingTextGenerator extends TextGenerator with token-level streaming.
type StreamingTextGenerator interface {
	TextGenerator
	GenerateStreamText(ctx context.Context, prompt string, tokenFn func(string)) (string, error)
}

// VisionTextGenerator extends TextGenerator with multimodal (image + text) generation.
// Used by declarative skills when input contains image_url.
type VisionTextGenerator interface {
	TextGenerator
	GenerateWithImage(ctx context.Context, prompt string, imageURL string) (string, error)
	GenerateStreamWithImage(ctx context.Context, prompt string, imageURL string, tokenFn func(string)) (string, error)
}

// DefaultExecutor implements SkillExecutor with real Wasm execution.
type DefaultExecutor struct {
	mu             sync.RWMutex
	skills         map[string]skills.Skill
	wasm           map[string][]byte
	runtime        *wasm.Runtime
	tools          toolrunner.ToolRunner
	auditLogger    execution_logs.AuditLogger
	textGenerator  TextGenerator
	stepDispatcher toolrunner.StepDispatcher
}

func NewDefaultExecutor() *DefaultExecutor {
	return &DefaultExecutor{
		skills: make(map[string]skills.Skill),
		wasm:   make(map[string][]byte),
	}
}

func NewDefaultExecutorWithRuntime(rt *wasm.Runtime, tools toolrunner.ToolRunner) *DefaultExecutor {
	return &DefaultExecutor{
		skills:  make(map[string]skills.Skill),
		wasm:    make(map[string][]byte),
		runtime: rt,
		tools:   tools,
	}
}

func (e *DefaultExecutor) SetToolRunner(tools toolrunner.ToolRunner) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tools = tools
}

// SetStepDispatcher sets the dispatcher used by ExecutePlan.
// When set, ExecutePlan delegates step execution to the dispatcher
// instead of creating a harness.StepExecutor directly.
func (e *DefaultExecutor) SetStepDispatcher(d toolrunner.StepDispatcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stepDispatcher = d
}

func (e *DefaultExecutor) SetAuditLogger(l execution_logs.AuditLogger) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.auditLogger = l
}

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
	if ws, ok := s.(WasmSkill); ok {
		e.wasm[s.ID()] = ws.WasmBytes()
	}
	return nil
}

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

func (e *DefaultExecutor) ListSkills(ctx context.Context) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.skills))
	for id := range e.skills {
		ids = append(ids, id)
	}
	return ids
}

func (e *DefaultExecutor) CanExecute(ctx context.Context, skillID string) (bool, error) {
	if skillID == "" {
		return false, ErrEmptySkillID
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.skills[skillID]
	return exists, nil
}

// execDeps holds a snapshot of executor state for a single Execute call.
type execDeps struct {
	al execution_logs.AuditLogger
	tg TextGenerator
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

	execStatus := "success"
	defer func() {
		durationMs := float64(time.Since(start).Milliseconds())
		observability.RecordSkillExecution(ctx, req.SkillID, execStatus, durationMs)
	}()

	// Snapshot dependencies under read lock
	e.mu.RLock()
	deps := execDeps{
		al: e.auditLogger,
		tg: e.textGenerator,
	}
	s, exists := e.skills[req.SkillID]
	wasmBytes := e.wasm[req.SkillID]
	e.mu.RUnlock()

	mode := skills.GetExecutionMode(s)

	// Emit audit: started
	e.emitAudit(ctx, deps.al, audit.AuditEvent{
		ID: uuid.NewString(), TenantID: req.TenantID, UserID: req.UserID,
		RequestID: req.RequestID, Source: audit.SourceExecutor,
		Action: "skills.execute", Resource: "skill/" + req.SkillID,
		Outcome: "started", Timestamp: start,
	})

	// Skill not found
	if !exists {
		e.mu.RLock()
		loadedIDs := make([]string, 0, len(e.skills))
		for id := range e.skills {
			loadedIDs = append(loadedIDs, id)
		}
		e.mu.RUnlock()
		slog.Warn("skill not found in executor", "requested", req.SkillID, "loaded", loadedIDs)
		execStatus = "failed"
		return e.auditResult(ctx, deps.al, req, "failure", time.Since(start), map[string]string{"error": "skill not loaded"}),
			execution.ErrSkillNotLoaded
	}

	// Validate input against skill schema
	if err := validation.ValidateInput(req.Input, s.InputSchema()); err != nil {
		execStatus = "rejected"
		result := e.auditResult(ctx, deps.al, req, "rejected", time.Since(start), map[string]string{"error": err.Error(), "execution_mode": mode})
		result.Error = "input validation failed: " + err.Error()
		return result, err
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

	// Dispatch to execution path
	if e.runtime != nil && len(wasmBytes) > 0 {
		result, err := e.executeWasm(ctx, req, s, wasmBytes, timeout, start, deps, mode)
		if result != nil {
			execStatus = statusToMetric(result.Status)
		}
		return result, err
	}

	if mode != "declarative" {
		execStatus = "failed"
		return e.auditResult(ctx, deps.al, req, "failure", time.Since(start),
			map[string]string{"error": fmt.Sprintf("skill requires %s execution but no binary available", mode), "execution_mode": mode}),
			fmt.Errorf("skill %s requires %s execution but no binary available", req.SkillID, mode)
	}

	result, err := e.executeDeclarative(ctx, req, s, start, deps, mode)
	if result != nil {
		execStatus = statusToMetric(result.Status)
	}
	return result, err
}

// executeWasm handles the Wasm execution path, including LLM fallback.
func (e *DefaultExecutor) executeWasm(
	ctx context.Context,
	req execution.ExecutionRequest,
	s skills.Skill,
	wasmBytes []byte,
	timeout time.Duration,
	start time.Time,
	deps execDeps,
	mode string,
) (*execution.ExecutionResult, error) {
	limits := wasm.Limits{
		MaxExecutionTime: timeout,
		MaxMemoryBytes:   128 * 1024 * 1024,
	}

	output, wasmErr := e.runtime.Execute(ctx, wasmBytes, req.Input, limits)
	elapsed := time.Since(start)

	if wasmErr == nil {
		e.emitAudit(ctx, deps.al, audit.AuditEvent{
			ID: uuid.NewString(), TenantID: req.TenantID, UserID: req.UserID,
			RequestID: req.RequestID, Source: audit.SourceExecutor,
			Action: "skills.execute", Resource: "skill/" + req.SkillID,
			Outcome: "success", Duration: elapsed,
			Metadata: map[string]string{"execution_mode": mode}, Timestamp: time.Now(),
		})
		return &execution.ExecutionResult{
			Status: execution.StatusSuccess, Output: output, Duration: elapsed,
		}, nil
	}

	// Wasm failed — try LLM fallback
	if result, ok := e.tryLLMFallback(ctx, req, s, timeout, start, deps, mode, wasmErr); ok {
		return result, nil
	}

	// No fallback — return Wasm error
	if ctx.Err() == context.DeadlineExceeded {
		return e.auditResult(ctx, deps.al, req, "timeout", elapsed,
			map[string]string{"error": "execution timeout", "execution_mode": mode}),
			execution.ErrExecutionTimeout
	}

	return e.auditResult(ctx, deps.al, req, "failure", elapsed,
		map[string]string{"error": wasmErr.Error(), "execution_mode": mode}), wasmErr
}

// tryLLMFallback attempts LLM fallback after Wasm failure. Returns (result, true) on success.
func (e *DefaultExecutor) tryLLMFallback(
	ctx context.Context,
	req execution.ExecutionRequest,
	s skills.Skill,
	timeout time.Duration,
	start time.Time,
	deps execDeps,
	mode string,
	wasmErr error,
) (*execution.ExecutionResult, bool) {
	if ctx.Err() == context.DeadlineExceeded || deps.tg == nil {
		return nil, false
	}

	hasLLMPerm := false
	for _, p := range s.RequiredPermissions() {
		if p == "llm:generate" {
			hasLLMPerm = true
			break
		}
	}
	if !hasLLMPerm {
		return nil, false
	}

	slog.WarnContext(ctx, "wasm execution failed, falling back to LLM",
		"skill_id", req.SkillID, "error", wasmErr)

	prompt := buildSkillPrompt(s, req.Input)
	llmCtx, llmCancel := context.WithTimeout(ctx, timeout)
	defer llmCancel()

	llmOutput, llmErr := deps.tg.GenerateText(llmCtx, prompt)
	if llmErr != nil {
		slog.WarnContext(ctx, "LLM fallback also failed", "error", llmErr)
		return nil, false
	}

	e.emitAudit(ctx, deps.al, audit.AuditEvent{
		ID: uuid.NewString(), TenantID: req.TenantID, UserID: req.UserID,
		RequestID: req.RequestID, Source: audit.SourceExecutor,
		Action: "skills.execute", Resource: "skill/" + req.SkillID,
		Outcome: "success", Duration: time.Since(start),
		Metadata: map[string]string{"fallback": "llm", "execution_mode": mode}, Timestamp: time.Now(),
	})
	return &execution.ExecutionResult{
		Status: execution.StatusSuccess, Output: []byte(llmOutput), Duration: time.Since(start),
	}, true
}

// executeDeclarative handles the declarative (LLM-based) skill execution path.
// When input contains image_url, it routes through vision-capable LLM with
// multimodal content blocks instead of plain text.
func (e *DefaultExecutor) executeDeclarative(
	ctx context.Context,
	req execution.ExecutionRequest,
	s skills.Skill,
	start time.Time,
	deps execDeps,
	mode string,
) (*execution.ExecutionResult, error) {
	if deps.tg == nil {
		return e.auditResult(ctx, deps.al, req, "failure", time.Since(start),
			map[string]string{"error": "no LLM configured", "execution_mode": mode}),
			fmt.Errorf("declarative skill %q requires LLM but no text generator is configured", req.SkillID)
	}

	prompt := buildSkillPrompt(s, req.Input)
	if skills.GetPrompt(s) == "" {
		slog.WarnContext(ctx, "declarative skill has no SKILL.md; using generic fallback",
			"skill_id", req.SkillID)
	}

	// Detect image_url in input and route to vision-capable path.
	imageURL := extractImageURL(req.Input)
	useVision := imageURL != ""
	imageURLLog := imageURL
	if len(imageURLLog) > 80 {
		imageURLLog = imageURLLog[:80]
	}
	slog.InfoContext(ctx, "declarative skill execution",
		"skill_id", req.SkillID, "useVision", useVision, "imageURL", imageURLLog)

	var output string
	var err error

	if useVision {
		output, err = e.generateWithVision(ctx, deps.tg, prompt, imageURL, req.TokenFn)
		if err != nil {
			slog.WarnContext(ctx, "vision generation failed, falling back to text-only",
				"skill_id", req.SkillID, "error", err)
			output, err = e.generateText(ctx, deps.tg, prompt, req.TokenFn)
		}
	} else {
		output, err = e.generateText(ctx, deps.tg, prompt, req.TokenFn)
	}

	if err != nil {
		result := e.auditResult(ctx, deps.al, req, "failure", time.Since(start),
			map[string]string{"error": err.Error(), "execution_mode": mode})
		result.Error = "LLM generation failed: " + err.Error()
		return result, err
	}

	execMode := mode
	if useVision {
		execMode = mode + "+vision"
	}

	duration := time.Since(start)
	e.emitAudit(ctx, deps.al, audit.AuditEvent{
		ID: uuid.NewString(), TenantID: req.TenantID, UserID: req.UserID,
		RequestID: req.RequestID, Source: audit.SourceExecutor,
		Action: "skills.execute", Resource: "skill/" + req.SkillID,
		Outcome: "success", Duration: duration,
		Metadata: map[string]string{"execution_mode": execMode}, Timestamp: time.Now(),
	})
	return &execution.ExecutionResult{
		Status: execution.StatusSuccess, Output: []byte(output), Duration: duration,
	}, nil
}

// generateText performs text-only LLM generation, with streaming if available.
func (e *DefaultExecutor) generateText(ctx context.Context, tg TextGenerator, prompt string, tokenFn func(string)) (string, error) {
	if tokenFn != nil {
		if stg, ok := tg.(StreamingTextGenerator); ok {
			return stg.GenerateStreamText(ctx, prompt, tokenFn)
		}
		output, err := tg.GenerateText(ctx, prompt)
		if err == nil && output != "" {
			tokenFn(output)
		}
		return output, err
	}
	return tg.GenerateText(ctx, prompt)
}

// generateWithVision performs multimodal LLM generation with image content.
func (e *DefaultExecutor) generateWithVision(ctx context.Context, tg TextGenerator, prompt string, imageURL string, tokenFn func(string)) (string, error) {
	vtg, ok := tg.(VisionTextGenerator)
	if !ok {
		return "", fmt.Errorf("text generator does not support vision")
	}
	if tokenFn != nil {
		return vtg.GenerateStreamWithImage(ctx, prompt, imageURL, tokenFn)
	}
	return vtg.GenerateWithImage(ctx, prompt, imageURL)
}

// extractImageURL checks if the input JSON contains an image_url field.
func extractImageURL(input []byte) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	url, _ := m["image_url"].(string)
	if url == "" {
		return ""
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url
	}
	return ""
}

// buildSkillPrompt constructs the LLM prompt for a skill, using SKILL.md if available.
func buildSkillPrompt(s skills.Skill, input []byte) string {
	if p := skills.GetPrompt(s); p != "" {
		return strings.ReplaceAll(p, "{{.Input}}", string(input))
	}
	return fmt.Sprintf("You are performing the skill: %s.\n\n%s\n\nUser input:\n%s",
		s.Name(), s.Description(), string(input))
}

// auditResult creates an ExecutionResult with an audit event.
func (e *DefaultExecutor) auditResult(
	ctx context.Context,
	al execution_logs.AuditLogger,
	req execution.ExecutionRequest,
	outcome string,
	duration time.Duration,
	metadata map[string]string,
) *execution.ExecutionResult {
	e.emitAudit(ctx, al, audit.AuditEvent{
		ID: uuid.NewString(), TenantID: req.TenantID, UserID: req.UserID,
		RequestID: req.RequestID, Source: audit.SourceExecutor,
		Action: "skills.execute", Resource: "skill/" + req.SkillID,
		Outcome: outcome, Duration: duration,
		Metadata: metadata, Timestamp: time.Now(),
	})

	status := execution.StatusFailed
	switch outcome {
	case "timeout":
		status = execution.StatusTimeout
	case "rejected":
		status = execution.StatusRejected
	case "success":
		status = execution.StatusSuccess
	}

	return &execution.ExecutionResult{
		Status:   status,
		Error:    metadata["error"],
		Duration: duration,
	}
}

func statusToMetric(status execution.ExecutionStatus) string {
	switch status {
	case execution.StatusSuccess:
		return "success"
	case execution.StatusFailed:
		return "failed"
	case execution.StatusTimeout:
		return "timeout"
	case execution.StatusRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

func (e *DefaultExecutor) SkillCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.skills)
}

var ErrNilExecutionPlan = errors.New("executor: nil execution plan")

func (e *DefaultExecutor) ExecutePlan(ctx context.Context, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) error {
	if plan == nil {
		return ErrNilExecutionPlan
	}
	// Use the injected StepDispatcher. This decouples the executor from
	// the harness package -- the dispatcher is injected by the caller
	// (typically server setup in cmd/openbotstack).
	e.mu.RLock()
	dispatcher := e.stepDispatcher
	e.mu.RUnlock()

	if dispatcher == nil {
		return fmt.Errorf("executor: StepDispatcher not configured -- call SetStepDispatcher before ExecutePlan")
	}
	for _, step := range plan.Steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		s := step // capture for pointer
		result, err := dispatcher.Dispatch(ctx, &s, ec, nil, 0)
		if result != nil {
			ec.AddResult(*result)
		}
		if err != nil {
			return fmt.Errorf("step %s failed: %w", step.Name, err)
		}
	}
	return nil
}

func (e *DefaultExecutor) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ids := make([]string, 0, len(e.skills))
	for id := range e.skills {
		ids = append(ids, id)
	}
	return ids
}

func (e *DefaultExecutor) Get(id string) (skills.Skill, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, exists := e.skills[id]
	if !exists {
		return nil, ErrSkillNotFound
	}
	return s, nil
}

func (e *DefaultExecutor) emitAudit(ctx context.Context, al execution_logs.AuditLogger, event audit.AuditEvent) {
	if al == nil {
		return
	}
	_ = al.Log(ctx, event) //nolint:errcheck // audit failures must not disrupt execution
}
