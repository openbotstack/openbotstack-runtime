// Package main implements an LLM-assisted sentiment analysis skill.
//
// In Wasm execution, LLM calls are handled by the executor's TextGenerator
// fallback. The skill itself performs deterministic pre/post-processing.
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

// SkillInput is the input from the runtime.
type SkillInput struct {
	Text   string `json:"text"`
	UserID string `json:"user_id,omitempty"`
}

// SkillOutput is the output to the runtime.
type SkillOutput struct {
	Sentiment  string  `json:"sentiment"`  // positive, negative, neutral
	Confidence float64 `json:"confidence"` // 0.0 - 1.0
	Reasoning  string  `json:"reasoning"`
	Error      string  `json:"error,omitempty"`
}

// run is the core logic, separated from I/O for testability.
// Performs deterministic sentiment analysis (no LLM in Wasm).
func run(inputData []byte) []byte {
	var input SkillInput
	if len(inputData) > 0 {
		if err := json.Unmarshal(inputData, &input); err != nil {
			return marshalError("invalid input: " + err.Error())
		}
	}

	if strings.TrimSpace(input.Text) == "" {
		return marshalError("text is required")
	}

	if len(input.Text) > 5000 {
		return marshalError("text exceeds maximum length of 5000 characters")
	}

	// Deterministic keyword-based sentiment analysis
	sentiment, confidence, reasoning := analyzeSentiment(input.Text)

	// Adjust confidence for short text
	if len(input.Text) < 50 {
		confidence *= 0.8
		reasoning += " (Note: Short text may reduce accuracy)"
	}

	output := SkillOutput{
		Sentiment:  sentiment,
		Confidence: confidence,
		Reasoning:  reasoning,
	}

	data, _ := json.Marshal(output)
	return data
}

func analyzeSentiment(text string) (string, float64, string) {
	lower := strings.ToLower(text)

	positiveWords := []string{"love", "great", "excellent", "happy", "thank", "amazing"}
	negativeWords := []string{"hate", "terrible", "awful", "angry", "frustrated", "disappointed"}

	for _, w := range positiveWords {
		if strings.Contains(lower, w) {
			return "positive", 0.92, "The text expresses satisfaction and gratitude."
		}
	}
	for _, w := range negativeWords {
		if strings.Contains(lower, w) {
			return "negative", 0.88, "The text expresses frustration and disappointment."
		}
	}

	return "neutral", 0.75, "The text is factual without emotional indicators."
}

func marshalError(msg string) []byte {
	output := SkillOutput{Error: msg}
	data, _ := json.Marshal(output)
	return data
}

func main() {
	input, _ := io.ReadAll(os.Stdin)
	os.Stdout.Write(run(input))
}
