// Package main implements a hello-world skill for OpenBotStack.
//
// Command pattern: reads input JSON from stdin, writes output JSON to stdout.
// Compatible with GOOS=wasip1 GOARCH=wasm (Go 1.26+) and TinyGo.
//
// Build for wasm:
//
//	GOOS=wasip1 GOARCH=wasm go build -o main.wasm .
//
// Test locally:
//
//	go test -v ./...
package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// SkillInput represents the input passed from the runtime.
type SkillInput struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Message   string `json:"message"`
}

// SkillOutput represents the output returned to the runtime.
type SkillOutput struct {
	Message string            `json:"message"`
	Data    map[string]string `json:"data,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// processMessage handles different message types (pure logic, testable).
func processMessage(message string) string {
	msg := strings.ToLower(strings.TrimSpace(message))

	switch {
	case msg == "":
		return "Hello! I'm the hello-world skill. How can I help you?"

	case strings.Contains(msg, "hello") || strings.Contains(msg, "hi"):
		return "Hello! I'm the hello-world skill. How can I help you?"

	case strings.Contains(msg, "name"):
		return "I'm the OpenBotStack hello-world skill!"

	case strings.Contains(msg, "help"):
		return "I can respond to greetings. Try saying 'hello' or ask my name!"

	case strings.Contains(msg, "version"):
		return "hello-world skill v1.0.0"

	default:
		return "I heard you say: " + message
	}
}

// run is the core execution logic, separated from I/O for testability.
func run(inputData []byte) []byte {
	var input SkillInput
	if len(inputData) > 0 {
		if err := json.Unmarshal(inputData, &input); err != nil {
			output := SkillOutput{Error: "failed to parse input: " + err.Error()}
			data, _ := json.Marshal(output)
			return data
		}
	}

	response := processMessage(input.Message)

	output := SkillOutput{
		Message: response,
		Data: map[string]string{
			"skill":   "hello-world",
			"version": "1.0.0",
		},
	}

	data, _ := json.Marshal(output)
	return data
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	os.Stdout.Write(run(input))
}
