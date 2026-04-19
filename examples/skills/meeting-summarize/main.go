// Package main implements an LLM-orchestrated meeting summarization skill.
//
// In Wasm execution, LLM calls are handled by the executor's TextGenerator
// fallback. The skill extracts key information from the transcript.
//
// Build for wasm:
//
//	GOOS=wasip1 GOARCH=wasm go build -o main.wasm .
package main

import (
	"encoding/json"
	"io"
	"os"
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

// run is the core logic, separated from I/O for testability.
func run(inputData []byte) []byte {
	var input Input
	if len(inputData) > 0 {
		if err := json.Unmarshal(inputData, &input); err != nil {
			output := Output{Error: "invalid input: " + err.Error()}
			data, _ := json.Marshal(output)
			return data
		}
	}

	if strings.TrimSpace(input.Transcript) == "" {
		output := Output{Error: "transcript is required"}
		data, _ := json.Marshal(output)
		return data
	}

	if input.MaxLength <= 0 {
		input.MaxLength = 200
	}

	output := Output{
		Summary:     extractSummary(input.Transcript),
		ActionItems: extractActionItems(input.Transcript),
		Attendees:   extractAttendees(input.Transcript),
	}

	data, _ := json.Marshal(output)
	return data
}

func extractSummary(text string) string {
	// Take first meaningful sentence(s) as summary
	lines := strings.Split(text, "\n")
	var parts []string
	totalLen := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*") {
			continue
		}
		parts = append(parts, line)
		totalLen += len(line)
		if totalLen > 200 {
			break
		}
	}
	if len(parts) == 0 {
		return "No summary available."
	}
	return strings.Join(parts, " ")
}

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

func extractAttendees(transcript string) []string {
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
	input, _ := io.ReadAll(os.Stdin)
	os.Stdout.Write(run(input))
}
