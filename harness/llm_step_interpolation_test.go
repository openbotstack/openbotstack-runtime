package harness

import (
	"context"
	"encoding/json"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/execution"
)

// ========================================================================
// TDD: Step Result Interpolation in LLM Steps
// ========================================================================
// Bug: executeLLMStep did not call ResolveArguments(prevResults), so
// {{step_name}} templates in LLM "respond" step arguments were never resolved.
// This test verifies the fix: LLM steps must resolve templates from prior tool outputs.

// TestLLMStep_ResolvesTemplateArguments verifies that an LLM "respond" step
// receives interpolated arguments where {{tool_step}} is replaced with actual tool output.
func TestLLMStep_ResolvesTemplateArguments(t *testing.T) {
	cfg := DefaultHarnessConfig()

	// Tool step returns structured data
	tr := newMockToolRunner()
	tr.result["builtin.vision_analyze"] = map[string]any{
		"description": "A severe laceration on the lateral ankle",
		"severity":    "high",
	}

	// Capture what the LLM generator actually receives
	var capturedPrompt string
	llmGen := func(ctx context.Context, systemPrompt, userMessage string, _ []aitypes.Message) (string, error) {
		capturedPrompt = userMessage
		return "Based on the analysis: severe laceration detected.", nil
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		LLMGenerator: llmGen,
	})

	// Plan: tool step → LLM respond step with {{builtin.vision_analyze}} template
	plan := makeFrozenPlan(
		execution.ExecutionStep{
			Name:      "builtin.vision_analyze",
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{"image_url": "http://example.com/img.jpg"},
		},
		execution.ExecutionStep{
			Name: "respond",
			Type: execution.StepTypeLLM,
			Arguments: map[string]any{
				"prompt": "Analyze this injury based on the vision analysis: {{builtin.vision_analyze}}. Provide classification and recommendations.",
			},
		},
	)

	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify: the LLM generator received the resolved template, NOT the literal {{builtin.vision_analyze}}
	if capturedPrompt == "" {
		t.Fatal("LLM generator was never called")
	}

	// The template must be resolved — no literal {{...}} should remain
	if containsTemplate(capturedPrompt, "builtin.vision_analyze") {
		t.Errorf("Template {{builtin.vision_analyze}} was NOT resolved in LLM prompt.\nGot: %s", capturedPrompt)
	}

	// The prompt should contain the actual tool output data
	if !containsSubstring(capturedPrompt, "laceration") {
		t.Errorf("LLM prompt does not contain tool output data.\nGot: %s", capturedPrompt)
	}

	// Verify both steps completed
	if len(result.StepResults) != 2 {
		t.Fatalf("Expected 2 step results, got %d", len(result.StepResults))
	}
	if result.StepResults[0].Error != nil {
		t.Errorf("Tool step failed: %v", result.StepResults[0].Error)
	}
	if result.StepResults[1].Error != nil {
		t.Errorf("LLM step failed: %v", result.StepResults[1].Error)
	}
}

// TestLLMStep_ResolvesStructuredOutputAsJSON verifies that structured tool output
// (map[string]any) is JSON-serialized when interpolated into LLM step arguments,
// not Go's fmt.Sprintf("%v") map[...] format.
func TestLLMStep_ResolvesStructuredOutputAsJSON(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["data_tool"] = map[string]any{
		"key": "value",
		"nested": map[string]any{
			"field": 42,
		},
	}

	var capturedPrompt string
	llmGen := func(ctx context.Context, _, userMessage string, _ []aitypes.Message) (string, error) {
		capturedPrompt = userMessage
		return "done", nil
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		LLMGenerator: llmGen,
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{
			Name:      "data_tool",
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{"query": "test"},
		},
		execution.ExecutionStep{
			Name: "respond",
			Type: execution.StepTypeLLM,
			Arguments: map[string]any{
				"prompt": "Here is the data: {{data_tool}}",
			},
		},
	)

	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// The interpolated output must be valid JSON, not Go map[...] format
	if capturedPrompt == "" {
		t.Fatal("LLM generator was never called")
	}

	// Extract the JSON part after "Here is the data: "
	dataStart := len("Here is the data: ")
	if dataStart >= len(capturedPrompt) {
		t.Fatalf("Prompt too short: %s", capturedPrompt)
	}
	jsonPart := capturedPrompt[dataStart:]

	// Must be parseable as JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
		t.Errorf("Interpolated tool output is not valid JSON.\nGot: %s\nParse error: %v", jsonPart, err)
	}

	// Must NOT contain Go format indicators like "map["
	if containsSubstring(capturedPrompt, "map[") {
		t.Errorf("Output uses Go map format instead of JSON.\nGot: %s", capturedPrompt)
	}
}

// TestLLMStep_ResolvesShortAlias verifies that short aliases (e.g., "vision_analyze"
// for "builtin.vision_analyze") work in template resolution for LLM steps.
func TestLLMStep_ResolvesShortAlias(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["builtin.vision_analyze"] = map[string]any{"description": "test result"}

	var capturedPrompt string
	llmGen := func(ctx context.Context, _, userMessage string, _ []aitypes.Message) (string, error) {
		capturedPrompt = userMessage
		return "done", nil
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		LLMGenerator: llmGen,
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{
			Name:      "builtin.vision_analyze",
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{"image_url": "http://example.com/img.jpg"},
		},
		execution.ExecutionStep{
			Name: "respond",
			Type: execution.StepTypeLLM,
			Arguments: map[string]any{
				"prompt": "Result: {{vision_analyze}}", // short alias (without builtin. prefix)
			},
		},
	)

	ec := execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
	_, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if containsTemplate(capturedPrompt, "vision_analyze") {
		t.Errorf("Short alias {{vision_analyze}} was NOT resolved.\nGot: %s", capturedPrompt)
	}
}

// --- helpers ---

func containsTemplate(s, name string) bool {
	return containsSubstring(s, "{{"+name+"}}")
}

func containsSubstring(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
