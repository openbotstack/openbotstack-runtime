// Package main implements an LLM-assisted skill for sentiment analysis.
//
// This skill demonstrates:
//   - Business logic in code
//   - Calling LLM via Host API for reasoning
//   - Structured output validation
//
// Build for wasm:
//
//	tinygo build -o sentiment.wasm -target wasi -scheduler=none main.go
package main

import (
	"encoding/json"
	"strings"
)

// SkillInput is the input from the runtime.
type SkillInput struct {
	Text   string `json:"text"`
	UserID string `json:"user_id,omitempty"`
}

// SkillOutput is the output to the runtime.
type SkillOutput struct {
	Sentiment  string  `json:"sentiment"`  // positive, negative, neutral
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	Reasoning  string  `json:"reasoning"`  // LLM explanation
	Error      string  `json:"error,omitempty"`
}

// Host API stubs - in real wasm these are provided by runtime
var (
	inputBuffer  []byte
	outputBuffer []byte
	llmResponses = map[string]string{
		"positive": `{"sentiment": "positive", "confidence": 0.92, "reasoning": "The text expresses satisfaction and gratitude."}`,
		"negative": `{"sentiment": "negative", "confidence": 0.88, "reasoning": "The text expresses frustration and disappointment."}`,
		"neutral":  `{"sentiment": "neutral", "confidence": 0.75, "reasoning": "The text is factual without emotional indicators."}`,
	}
)

// SetInput sets the input buffer (for testing).
func SetInput(data []byte) { inputBuffer = data }

// GetOutput gets the output buffer (for testing).
func GetOutput() []byte { return outputBuffer }

// ResetBuffers clears buffers (for testing).
func ResetBuffers() {
	inputBuffer = nil
	outputBuffer = nil
}

// Execute is the main skill logic.
func Execute() error {
	var input SkillInput
	if len(inputBuffer) > 0 {
		if err := json.Unmarshal(inputBuffer, &input); err != nil {
			return setError("invalid input: " + err.Error())
		}
	}

	// Business logic: validate input
	if strings.TrimSpace(input.Text) == "" {
		return setError("text is required")
	}

	if len(input.Text) > 5000 {
		return setError("text exceeds maximum length of 5000 characters")
	}

	// Determine sentiment category for LLM call (simulated)
	// In real implementation, this calls hostLLM.Generate()
	llmResponse := callLLM(input.Text)

	// Parse LLM response
	var result SkillOutput
	if err := json.Unmarshal([]byte(llmResponse), &result); err != nil {
		return setError("failed to parse LLM response")
	}

	// Business logic: adjust confidence for short text
	if len(input.Text) < 50 {
		result.Confidence *= 0.8 // Lower confidence for short text
		result.Reasoning += " (Note: Short text may reduce accuracy)"
	}

	data, _ := json.Marshal(result)
	outputBuffer = data
	return nil
}

// callLLM simulates LLM call - in real wasm this is a host function.
func callLLM(text string) string {
	text = strings.ToLower(text)

	// Simulated sentiment detection
	positiveWords := []string{"love", "great", "excellent", "happy", "thank", "amazing"}
	negativeWords := []string{"hate", "terrible", "awful", "angry", "frustrated", "disappointed"}

	for _, w := range positiveWords {
		if strings.Contains(text, w) {
			return llmResponses["positive"]
		}
	}
	for _, w := range negativeWords {
		if strings.Contains(text, w) {
			return llmResponses["negative"]
		}
	}
	return llmResponses["neutral"]
}

func setError(msg string) error {
	output := SkillOutput{Error: msg}
	data, _ := json.Marshal(output)
	outputBuffer = data
	return nil
}

func main() {}

//export execute
func execute() { _ = Execute() } //nolint:unused // exported for wasm
