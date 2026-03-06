// Package main implements an LLM-orchestrated meeting summarization skill.
//
// This is an LLM skill - it uses an LLM provider internally but has
// a deterministic interface with structured input/output.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Input for the meeting summarize skill.
type Input struct {
	Transcript string `json:"transcript"`
	MaxLength  int    `json:"max_length,omitempty"` // Optional, default 200
}

// Output from the meeting summarize skill.
type Output struct {
	Summary     string   `json:"summary"`
	ActionItems []string `json:"action_items"`
	Attendees   []string `json:"attendees,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// LLMClient interface for text generation.
type LLMClient interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// SkillExecutor handles the meeting summarization logic.
type SkillExecutor struct {
	llm LLMClient
}

// NewSkillExecutor creates a new executor with the given LLM client.
func NewSkillExecutor(llm LLMClient) *SkillExecutor {
	return &SkillExecutor{llm: llm}
}

// Execute summarizes a meeting transcript.
func (s *SkillExecutor) Execute(ctx context.Context, inputJSON []byte) ([]byte, error) {
	var input Input
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return json.Marshal(Output{Error: "invalid input: " + err.Error()})
	}

	if strings.TrimSpace(input.Transcript) == "" {
		return json.Marshal(Output{Error: "transcript is required"})
	}

	// Default max length
	if input.MaxLength <= 0 {
		input.MaxLength = 200
	}

	// Build prompt for LLM
	prompt := s.buildPrompt(input)

	// Call LLM
	response, err := s.llm.Generate(ctx, prompt)
	if err != nil {
		return json.Marshal(Output{Error: "LLM call failed: " + err.Error()})
	}

	// Parse structured response
	output := s.parseResponse(response, input.Transcript)
	return json.Marshal(output)
}

func (s *SkillExecutor) buildPrompt(input Input) string {
	return fmt.Sprintf(`Analyze this meeting transcript and provide:
1. A summary (max %d words)
2. Action items as a list
3. Attendees mentioned

Transcript:
%s

Respond in JSON format:
{"summary": "...", "action_items": ["..."], "attendees": ["..."]}`, input.MaxLength, input.Transcript)
}

func (s *SkillExecutor) parseResponse(response, transcript string) Output {
	// Try to parse as JSON first
	var output Output
	if err := json.Unmarshal([]byte(response), &output); err == nil {
		return output
	}

	// Fallback: extract from plain text
	output = Output{
		Summary:     extractSummary(response),
		ActionItems: extractActionItems(response),
		Attendees:   extractAttendees(transcript),
	}

	if output.Summary == "" {
		output.Summary = response
	}

	return output
}

// extractSummary extracts summary from unstructured text.
func extractSummary(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 20 && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
			return line
		}
	}
	return text
}

// extractActionItems extracts bullet points as action items.
func extractActionItems(text string) []string {
	var items []string
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			item := strings.TrimPrefix(strings.TrimPrefix(line, "-"), "*")
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
	}
	return items
}

// extractAttendees finds names mentioned in transcript.
func extractAttendees(transcript string) []string {
	// Simple heuristic: find words after "said", "mentioned", "asked"
	var attendees []string
	seen := make(map[string]bool)

	words := strings.Fields(transcript)
	for i, word := range words {
		if word == "said:" || word == "mentioned:" || word == "asked:" {
			if i > 0 {
				name := strings.Trim(words[i-1], ",.:")
				if !seen[name] && len(name) > 1 {
					attendees = append(attendees, name)
					seen[name] = true
				}
			}
		}
	}

	return attendees
}

func main() {
	// Example usage with stub LLM
	fmt.Println("meeting-summarize skill loaded")
}
