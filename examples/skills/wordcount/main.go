// Package main implements a word count skill.
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

	// Handle empty text
	if strings.TrimSpace(input.Text) == "" {
		output := Output{Count: 0, Words: []string{}}
		data, _ := json.Marshal(output)
		return data
	}

	words := strings.Fields(input.Text)
	output := Output{Count: len(words), Words: words}
	data, _ := json.Marshal(output)
	return data
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	os.Stdout.Write(run(input))
}
