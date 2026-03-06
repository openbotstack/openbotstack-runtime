// Package main implements a production-like hello-world skill for OpenBotStack.
//
// This is a standalone skill that can be compiled to WebAssembly and
// executed by the OpenBotStack runtime. It demonstrates:
//
//   - Reading input from the host runtime
//   - Processing user messages
//   - Writing output back to the runtime
//   - Using host APIs (logging, KV, LLM) - stubbed for local testing
//
// Build for wasm:
//
//	tinygo build -o hello.wasm -target wasi -scheduler=none main.go
//
// Test locally:
//
//	go test -v ./...
package main

import (
	"encoding/json"
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

// Shared memory buffers (populated by host, read by guest)
var (
	inputBuffer  []byte
	outputBuffer []byte
)

// SetInput is called by tests to set input data.
func SetInput(data []byte) {
	inputBuffer = data
}

// GetOutput is called by tests to read output data.
func GetOutput() []byte {
	return outputBuffer
}

// Execute is the main skill logic.
func Execute() error {
	// Parse input
	var input SkillInput
	if len(inputBuffer) > 0 {
		if err := json.Unmarshal(inputBuffer, &input); err != nil {
			return setError("failed to parse input: " + err.Error())
		}
	}

	// Process message
	response := processMessage(input.Message)

	// Set output
	output := SkillOutput{
		Message: response,
		Data: map[string]string{
			"skill":   "hello-world",
			"version": "1.0.0",
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		return setError("failed to marshal output: " + err.Error())
	}
	outputBuffer = data
	return nil
}

// processMessage handles different message types.
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

func setError(msg string) error {
	output := SkillOutput{Error: msg}
	data, _ := json.Marshal(output)
	outputBuffer = data
	return nil
}

// main is required but empty for wasm.
func main() {}

// execute is the wasm entrypoint called by the runtime.
//
//export execute
func execute() {
	Execute()
}
