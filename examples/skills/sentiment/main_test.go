package main

import (
	"encoding/json"
	"testing"
)

func TestSentimentPositive(t *testing.T) {
	input := SkillInput{Text: "I absolutely love this product! It's amazing and I'm so happy with my purchase. Thank you!"}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Sentiment != "positive" {
		t.Errorf("Expected positive, got %s", result.Sentiment)
	}
	if result.Confidence < 0.8 {
		t.Errorf("Expected high confidence, got %f", result.Confidence)
	}
}

func TestSentimentNegative(t *testing.T) {
	input := SkillInput{Text: "This is terrible, I'm so frustrated!"}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Sentiment != "negative" {
		t.Errorf("Expected negative, got %s", result.Sentiment)
	}
}

func TestSentimentNeutral(t *testing.T) {
	input := SkillInput{Text: "The package arrived on Tuesday."}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Sentiment != "neutral" {
		t.Errorf("Expected neutral, got %s", result.Sentiment)
	}
}

func TestEmptyInput(t *testing.T) {
	input := SkillInput{Text: ""}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Error == "" {
		t.Error("Expected error for empty text")
	}
}

func TestInputTooLong(t *testing.T) {
	longText := make([]byte, 6000)
	for i := range longText {
		longText[i] = 'a'
	}
	input := SkillInput{Text: string(longText)}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	if result.Error == "" {
		t.Error("Expected error for long text")
	}
}

func TestShortTextConfidence(t *testing.T) {
	input := SkillInput{Text: "love it"}
	data, _ := json.Marshal(input)
	output := run(data)

	var result SkillOutput
	_ = json.Unmarshal(output, &result)

	// Short text should have reduced confidence (92% * 0.8 = ~73.6%)
	if result.Confidence > 0.8 {
		t.Errorf("Expected reduced confidence for short text, got %f", result.Confidence)
	}
}
