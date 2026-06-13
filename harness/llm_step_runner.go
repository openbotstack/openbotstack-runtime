package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// LLMStepRunner encapsulates LLM step execution — direct response generation
// and iterative reasoning via the ReasoningLoop. Extracted from ExecutionHarness
// to narrow the test surface: LLM routing can be tested without constructing
// the full HarnessDeps.
type LLMStepRunner struct {
	llmGenerator       LLMGenerator
	llmStreamGenerator LLMStreamGenerator
	reasoningLoop      ReasoningLoop
}

// NewLLMStepRunner creates a step runner with the given LLM capabilities.
func NewLLMStepRunner(gen LLMGenerator, streamGen LLMStreamGenerator, loop ReasoningLoop) *LLMStepRunner {
	return &LLMStepRunner{
		llmGenerator:       gen,
		llmStreamGenerator: streamGen,
		reasoningLoop:      loop,
	}
}

// HasGenerator reports whether any LLM generation capability is available.
func (r *LLMStepRunner) HasGenerator() bool {
	return r.llmGenerator != nil || r.llmStreamGenerator != nil
}

// Run executes an LLM-type step. For "respond" steps it calls the LLM directly;
// for complex reasoning it delegates to the ReasoningLoop.
func (r *LLMStepRunner) Run(ctx context.Context, step execution.ExecutionStep, ec *execution.ExecutionContext, prevResults map[string]any) (*execution.StepResult, *HarnessMetrics, map[string][]TurnResult, error) {
	startTime := time.Now()

	// Check if the planner used {{step_name}} templates BEFORE ResolveArguments
	// replaces them. We need the original text to know whether injection is needed.
	hadTemplates := hasTemplateMarkers(&step)

	// Resolve step result interpolation templates in arguments.
	if err := step.ResolveArguments(prevResults); err != nil {
		return nil, nil, nil, fmt.Errorf("step %q: %w", step.Name, err)
	}

	// Derive user request: prefer ExpectedOutput, fall back to arguments.prompt.
	userRequest := step.ExpectedOutput
	if userRequest == "" {
		if prompt, ok := step.Arguments["prompt"].(string); ok && prompt != "" {
			userRequest = prompt
		}
	}

	// If this is a respond step with previous results but no {{...}} templates
	// were used by the planner, explicitly inject the results so the LLM sees them.
	if step.Name == "respond" && len(prevResults) > 0 && !hadTemplates {
		userRequest = injectPrevResults(userRequest, prevResults)
	}

	origCtx := ec.PlannerContext()

	// "respond" steps: direct LLM generation.
	if step.Name == "respond" && (r.llmGenerator != nil || r.llmStreamGenerator != nil) {
		result, err := r.generateResponse(ctx, step, origCtx, userRequest, ec, startTime)
		return result, nil, nil, err
	}

	// Complex LLM steps: delegate to ReasoningLoop.
	if r.reasoningLoop == nil {
		err := fmt.Errorf("step %q is LLM type but no reasoning loop configured", step.Name)
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: 0,
		}, nil, nil, err
	}

	pCtx := &planner.PlannerContext{UserRequest: userRequest}
	if origCtx != nil {
		// WithRequest copies every field (incl. TurnResults, ProgressFn) and
		// replaces only UserRequest — avoids the fragile manual field-by-field
		// copy that previously dropped TurnResults and ProgressFn.
		pCtx = origCtx.WithRequest(userRequest)
	}

	rlResult, rlErr := r.reasoningLoop.Run(ctx, &step, pCtx, ec)
	if rlErr != nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: rlErr,
		}, nil, nil, rlErr
	}

	turnData := map[string][]TurnResult{}
	if len(rlResult.TurnResults) > 0 {
		turnData[step.StepID] = rlResult.TurnResults
	}

	return &execution.StepResult{
		StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
		Output: rlResult.Output, Duration: rlResult.Duration,
	}, &HarnessMetrics{TotalLLMTurns: rlResult.TurnCount}, turnData, nil
}

func (r *LLMStepRunner) generateResponse(
	ctx context.Context,
	step execution.ExecutionStep,
	origCtx *planner.PlannerContext,
	userRequest string,
	ec *execution.ExecutionContext,
	startTime time.Time,
) (*execution.StepResult, error) {
	systemPrompt := ""
	var history []aitypes.Message
	if origCtx != nil {
		systemPrompt = origCtx.Soul.SystemPrompt
		history = origCtx.ConversationHistory
	}

	tokenFn := func(token string) {
		if ec.ProgressFn != nil {
			ec.ProgressFn("token", token, 0, "")
		}
	}

	var response string
	var err error

	if r.llmStreamGenerator != nil {
		response, err = r.llmStreamGenerator(ctx, systemPrompt, userRequest, history, tokenFn)
	} else {
		response, err = r.llmGenerator(ctx, systemPrompt, userRequest, history)
	}

	if err != nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: time.Since(startTime),
		}, err
	}
	return &execution.StepResult{
		StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
		Output: response, Duration: time.Since(startTime),
	}, nil
}

// injectPrevResults ensures respond steps see the output of previous tool/skill steps.
// When the LLM planner forgets to use {{step_name}} template references, this explicit
// injection prevents the respond step from producing generic replies.
// hasTemplateMarkers checks if the step's arguments contain unresolved {{...}} template
// references — meaning the planner correctly used step result interpolation and we
// should not inject results redundantly.
func hasTemplateMarkers(step *execution.ExecutionStep) bool {
	if step.Arguments == nil {
		return false
	}
	for _, val := range step.Arguments {
		if s, ok := val.(string); ok && strings.Contains(s, "{{") {
			return true
		}
	}
	return false
}

func injectPrevResults(userRequest string, prevResults map[string]any) string {
	if len(prevResults) == 0 {
		return userRequest
	}

	var sb strings.Builder
	sb.WriteString(userRequest)
	sb.WriteString("\n\n--- Previous step results ---\n")
	for name, result := range prevResults {
		sb.WriteString(fmt.Sprintf("\n[%s]:\n%s\n", name, formatPrevResult(result)))
	}
	return sb.String()
}

func formatPrevResult(v any) string {
	switch r := v.(type) {
	case string:
		return r
	case map[string]any:
		if desc, ok := r["description"]; ok {
			if s, ok := desc.(string); ok && len(s) > 0 {
				return s
			}
		}
		b, _ := json.MarshalIndent(r, "", "  ")
		return string(b)
	default:
		return fmt.Sprintf("%v", r)
	}
}
