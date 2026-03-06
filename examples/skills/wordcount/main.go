// Package main implements a word count skill as TinyGo Wasm.
//
// Build for wasm:
//
//	tinygo build -o main.wasm -target wasi -scheduler=none main.go
package main

import (
	"encoding/json"
	"strings"
)

// Input for the word count skill.
type Input struct {
	Text string `json:"text"`
}

// Output from the word count skill.
type Output struct {
	Count int      `json:"count"`
	Words []string `json:"words"`
	Error string   `json:"error,omitempty"`
}

// Shared buffers for host communication
var (
	inputBuffer  []byte
	outputBuffer []byte
)

// SetInput is called by tests.
func SetInput(data []byte) {
	inputBuffer = data
}

// GetOutput is called by tests.
func GetOutput() []byte {
	return outputBuffer
}

// Execute performs word counting.
func Execute() error {
	var input Input
	if len(inputBuffer) > 0 {
		if err := json.Unmarshal(inputBuffer, &input); err != nil {
			return setError("invalid input: " + err.Error())
		}
	}

	// Handle empty text
	if strings.TrimSpace(input.Text) == "" {
		output := Output{Count: 0, Words: []string{}}
		data, _ := json.Marshal(output)
		outputBuffer = data
		return nil
	}

	// Split into words
	words := strings.Fields(input.Text)

	output := Output{
		Count: len(words),
		Words: words,
	}

	data, err := json.Marshal(output)
	if err != nil {
		return setError("failed to marshal output: " + err.Error())
	}
	outputBuffer = data
	return nil
}

func setError(msg string) error {
	output := Output{Error: msg}
	data, _ := json.Marshal(output)
	outputBuffer = data
	return nil
}

func main() {}

//export execute
func execute() {
	Execute()
}
