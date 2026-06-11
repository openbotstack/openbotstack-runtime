package harness

import (
	"context"
	"errors"
	"strings"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestLLMStepRunner_RespondWithGenerator(t *testing.T) {
	gen := func(ctx context.Context, systemPrompt, userMessage string, history []aitypes.Message) (string, error) {
		return "Hello!", nil
	}
	runner := NewLLMStepRunner(
		func(ctx context.Context, sp, um string, h []aitypes.Message) (string, error) {
			return gen(ctx, sp, um, h)
		},
		nil, nil,
	)

	step := execution.ExecutionStep{
		StepID:        "s1",
		Name:          "respond",
		Type:          execution.StepTypeLLM,
		ExpectedOutput: "hi",
	}
	ec := &execution.ExecutionContext{}

	sr, metrics, turnData, err := runner.Run(context.Background(), step, ec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sr.Output != "Hello!" {
		t.Errorf("Output = %q, want %q", sr.Output, "Hello!")
	}
	if metrics != nil {
		t.Errorf("expected nil metrics for respond step, got %+v", metrics)
	}
	if turnData != nil {
		t.Errorf("expected nil turnData for respond step, got %+v", turnData)
	}
}

func TestLLMStepRunner_RespondWithStreamingGenerator(t *testing.T) {
	var tokens []string
	streamGen := func(ctx context.Context, sp, um string, h []aitypes.Message, fn func(string)) (string, error) {
		fn("Hel")
		fn("lo!")
		return "Hello!", nil
	}
	runner := NewLLMStepRunner(nil, streamGen, nil)

	if !runner.HasGenerator() {
		t.Error("HasGenerator should be true with stream generator")
	}

	step := execution.ExecutionStep{
		StepID:         "s1",
		Name:           "respond",
		Type:           execution.StepTypeLLM,
		ExpectedOutput: "hi",
	}
	ec := &execution.ExecutionContext{
		ProgressFn: func(eventType, data string, progress int, message string) {
			tokens = append(tokens, data)
		},
	}

	sr, _, _, err := runner.Run(context.Background(), step, ec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sr.Output != "Hello!" {
		t.Errorf("Output = %q", sr.Output)
	}
	if len(tokens) != 2 || tokens[0] != "Hel" || tokens[1] != "lo!" {
		t.Errorf("tokens = %v, want [Hel lo!]", tokens)
	}
}

func TestLLMStepRunner_GeneratorError(t *testing.T) {
	runner := NewLLMStepRunner(
		func(ctx context.Context, sp, um string, h []aitypes.Message) (string, error) {
			return "", errors.New("LLM unavailable")
		},
		nil, nil,
	)

	step := execution.ExecutionStep{
		StepID:         "s1",
		Name:           "respond",
		Type:           execution.StepTypeLLM,
		ExpectedOutput: "hi",
	}
	ec := &execution.ExecutionContext{}

	sr, _, _, err := runner.Run(context.Background(), step, ec, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if sr == nil {
		t.Fatal("expected non-nil StepResult on error")
	}
	if sr.Error == nil {
		t.Error("StepResult.Error should be set")
	}
}

func TestLLMStepRunner_NoGeneratorNoLoop(t *testing.T) {
	runner := NewLLMStepRunner(nil, nil, nil)

	if runner.HasGenerator() {
		t.Error("HasGenerator should be false with no generators")
	}

	step := execution.ExecutionStep{
		StepID:         "s1",
		Name:           "reason",
		Type:           execution.StepTypeLLM,
		ExpectedOutput: "think hard",
	}
	ec := &execution.ExecutionContext{}

	_, _, _, err := runner.Run(context.Background(), step, ec, nil)
	if err == nil {
		t.Fatal("expected error when no reasoning loop configured")
	}
}

func TestLLMStepRunner_ResolveArguments(t *testing.T) {
	runner := NewLLMStepRunner(
		func(ctx context.Context, sp, um string, h []aitypes.Message) (string, error) {
			return um, nil
		},
		nil, nil,
	)

	step := execution.ExecutionStep{
		StepID:   "s1",
		Name:     "respond",
		Type:     execution.StepTypeLLM,
		Arguments: map[string]any{"prompt": "original"},
	}
	ec := &execution.ExecutionContext{}
	prevResults := map[string]any{"prev_step": "resolved_value"}

	sr, _, _, err := runner.Run(context.Background(), step, ec, prevResults)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Should prefer ExpectedOutput over arguments.prompt when both are empty
	if sr.Output == "" {
		t.Error("Output should not be empty")
	}
}

func TestLLMStepRunner_WithPlannerContext(t *testing.T) {
	var capturedPrompt string
	runner := NewLLMStepRunner(
		func(ctx context.Context, sp, um string, h []aitypes.Message) (string, error) {
			capturedPrompt = sp
			return "ok", nil
		},
		nil, nil,
	)

	step := execution.ExecutionStep{
		StepID:         "s1",
		Name:           "respond",
		Type:           execution.StepTypeLLM,
		ExpectedOutput: "hi",
	}
	pCtx := &planner.PlannerContext{
		Soul: planner.AssistantSoul{SystemPrompt: "You are helpful."},
	}
	ec := &execution.ExecutionContext{}
	ec.SetPlannerContext(pCtx)

	_, _, _, err := runner.Run(context.Background(), step, ec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if capturedPrompt != "You are helpful." {
		t.Errorf("systemPrompt = %q, want %q", capturedPrompt, "You are helpful.")
	}
}

func TestInjectPrevResults_AddsResultsWhenMissing(t *testing.T) {
	prev := map[string]any{
		"vision_analyze": map[string]any{
			"description": "Image shows a wound with blood.",
			"tokens_used": float64(1464),
		},
	}
	result := injectPrevResults("Summarize the results.", prev)
	if !strings.Contains(result, "Previous step results") {
		t.Error("should inject previous results section")
	}
	if !strings.Contains(result, "Image shows a wound with blood") {
		t.Error("should contain vision analysis description")
	}
}

func TestHasTemplateMarkers_ReturnsTrue(t *testing.T) {
	step := &execution.ExecutionStep{
		Arguments: map[string]any{"prompt": "Here: {{data_tool}}"},
	}
	if !hasTemplateMarkers(step) {
		t.Error("should detect {{...}} in prompt argument")
	}
}

func TestHasTemplateMarkers_ReturnsFalse(t *testing.T) {
	step := &execution.ExecutionStep{
		Arguments: map[string]any{"prompt": "No templates here."},
	}
	if hasTemplateMarkers(step) {
		t.Error("should not detect templates when absent")
	}
}

func TestHasTemplateMarkers_NilArgs(t *testing.T) {
	step := &execution.ExecutionStep{}
	if hasTemplateMarkers(step) {
		t.Error("should return false for nil arguments")
	}
}

func TestInjectPrevResults_EmptyPrevResults(t *testing.T) {
	result := injectPrevResults("Hello", map[string]any{})
	if result != "Hello" {
		t.Error("should return unchanged with no prev results")
	}
}

func TestFormatPrevResult_ExtractsDescription(t *testing.T) {
	result := formatPrevResult(map[string]any{
		"description": "test desc",
		"tokens_used": float64(100),
	})
	if result != "test desc" {
		t.Errorf("should extract description, got %q", result)
	}
}
