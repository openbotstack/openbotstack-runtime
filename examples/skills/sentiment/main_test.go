package main

import (
	"encoding/json"
	"testing"
)

func TestSentimentPositive(t *testing.T) {
	ResetBuffers()
	// Use longer text to avoid short-text confidence reduction
	input := SkillInput{Text: "I absolutely love this product! It's amazing and I'm so happy with my purchase. Thank you!"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Sentiment != "positive" {
		t.Errorf("Expected positive, got %s", output.Sentiment)
	}
	if output.Confidence < 0.8 {
		t.Errorf("Expected high confidence, got %f", output.Confidence)
	}
}

func TestSentimentNegative(t *testing.T) {
	ResetBuffers()
	input := SkillInput{Text: "This is terrible, I'm so frustrated!"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Sentiment != "negative" {
		t.Errorf("Expected negative, got %s", output.Sentiment)
	}
}

func TestSentimentNeutral(t *testing.T) {
	ResetBuffers()
	input := SkillInput{Text: "The package arrived on Tuesday."}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Sentiment != "neutral" {
		t.Errorf("Expected neutral, got %s", output.Sentiment)
	}
}

func TestEmptyInput(t *testing.T) {
	ResetBuffers()
	input := SkillInput{Text: ""}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for empty text")
	}
}

func TestInputTooLong(t *testing.T) {
	ResetBuffers()
	longText := make([]byte, 6000)
	for i := range longText {
		longText[i] = 'a'
	}
	input := SkillInput{Text: string(longText)}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for long text")
	}
}

func TestShortTextConfidence(t *testing.T) {
	ResetBuffers()
	input := SkillInput{Text: "love it"}
	data, _ := json.Marshal(input)
	SetInput(data)

	_ = Execute()

	var output SkillOutput
	_ = json.Unmarshal(GetOutput(), &output)

	// Short text should have reduced confidence (92% * 0.8 = ~73.6%)
	if output.Confidence > 0.8 {
		t.Errorf("Expected reduced confidence for short text, got %f", output.Confidence)
	}
}
