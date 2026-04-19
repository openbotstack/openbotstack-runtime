// Package main implements a deterministic math-add skill.
//
// Build for wasm:
//
//	GOOS=wasip1 GOARCH=wasm go build -o main.wasm .
package main

import (
	"encoding/json"
	"io"
	"os"
)

// Input represents the skill input.
type Input struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

// Output represents the skill output.
type Output struct {
	Sum   float64 `json:"sum"`
	Error string  `json:"error,omitempty"`
}

// run is the core logic, separated from I/O for testability.
func run(inputData []byte) []byte {
	var input Input
	if err := json.Unmarshal(inputData, &input); err != nil {
		output := Output{Error: "invalid input: " + err.Error()}
		data, _ := json.Marshal(output)
		return data
	}

	output := Output{Sum: input.A + input.B}
	data, _ := json.Marshal(output)
	return data
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	os.Stdout.Write(run(input))
}
