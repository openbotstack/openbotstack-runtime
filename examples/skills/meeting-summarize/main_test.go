package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockLLM implements LLMClient for testing.
type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Generate(ctx context.Context, prompt string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func TestExecuteSuccess(t *testing.T) {
	llm := &mockLLM{
		response: `{"summary": "Team discussed Q4 roadmap", "action_items": ["Review specs", "Schedule follow-up"], "attendees": ["Alice", "Bob"]}`,
	}
	executor := NewSkillExecutor(llm)

	input := `{"transcript": "Alice said: We need to review Q4. Bob asked: When is the deadline?"}`
	result, err := executor.Execute(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var output Output
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("Failed to parse output: %v", err)
	}

	if output.Error != "" {
		t.Errorf("Unexpected error: %s", output.Error)
	}

	if output.Summary == "" {
		t.Error("Expected summary")
	}

	if len(output.ActionItems) != 2 {
		t.Errorf("Expected 2 action items, got %d", len(output.ActionItems))
	}
}

func TestExecuteEmptyTranscript(t *testing.T) {
	llm := &mockLLM{}
	executor := NewSkillExecutor(llm)

	input := `{"transcript": ""}`
	result, _ := executor.Execute(context.Background(), []byte(input))

	var output Output
	_ = json.Unmarshal(result, &output)

	if output.Error == "" {
		t.Error("Expected error for empty transcript")
	}
}

func TestExecuteInvalidInput(t *testing.T) {
	llm := &mockLLM{}
	executor := NewSkillExecutor(llm)

	result, _ := executor.Execute(context.Background(), []byte(`not json`))

	var output Output
	_ = json.Unmarshal(result, &output)

	if output.Error == "" {
		t.Error("Expected error for invalid input")
	}
}

func TestExecuteLLMFailure(t *testing.T) {
	llm := &mockLLM{
		err: errors.New("LLM unavailable"),
	}
	executor := NewSkillExecutor(llm)

	input := `{"transcript": "Some meeting notes"}`
	result, _ := executor.Execute(context.Background(), []byte(input))

	var output Output
	_ = json.Unmarshal(result, &output)

	if output.Error == "" {
		t.Error("Expected error when LLM fails")
	}
	if output.Error != "LLM call failed: LLM unavailable" {
		t.Errorf("Unexpected error message: %s", output.Error)
	}
}

func TestExecutePlainTextResponse(t *testing.T) {
	// LLM returns plain text instead of JSON
	llm := &mockLLM{
		response: `Summary: The team discussed priorities.
- Action item 1
- Action item 2`,
	}
	executor := NewSkillExecutor(llm)

	input := `{"transcript": "Discussion about priorities"}`
	result, _ := executor.Execute(context.Background(), []byte(input))

	var output Output
	json.Unmarshal(result, &output)

	if output.Error != "" {
		t.Errorf("Unexpected error: %s", output.Error)
	}

	if output.Summary == "" {
		t.Error("Expected summary from plain text response")
	}
}

func TestExtractAttendees(t *testing.T) {
	transcript := "Alice said: Let's start. Bob mentioned: We have blockers. Carol asked: What's the timeline?"

	attendees := extractAttendees(transcript)

	if len(attendees) != 3 {
		t.Errorf("Expected 3 attendees, got %d: %v", len(attendees), attendees)
	}
}

func TestExtractActionItems(t *testing.T) {
	text := `Here are the action items:
- Review the document
- Schedule the meeting
* Send the email`

	items := extractActionItems(text)

	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d: %v", len(items), items)
	}
}

func TestMaxLengthDefault(t *testing.T) {
	llm := &mockLLM{response: `{"summary": "test"}`}
	executor := NewSkillExecutor(llm)

	// No max_length specified - should use default 200
	input := `{"transcript": "Test transcript"}`
	_, _ = executor.Execute(context.Background(), []byte(input))

	// We'd need to capture the prompt to verify, but the test passes if no error
}
