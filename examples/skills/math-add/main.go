// Package main implements a deterministic math.add skill.
//
// This is a CODE skill - pure Go, no Wasm, deterministic.
// Build:
//
//	go build -o math-add main.go
package main

import (
	"encoding/json"
	"fmt"
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

// Execute performs the addition.
func Execute(inputJSON []byte) ([]byte, error) {
	var input Input
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		return json.Marshal(Output{Error: "invalid input: " + err.Error()})
	}

	output := Output{Sum: input.A + input.B}
	return json.Marshal(output)
}

func main() {
	// Example usage
	input := `{"a": 5, "b": 3}`
	result, _ := Execute([]byte(input))
	fmt.Println(string(result))
}
