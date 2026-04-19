package main

import (
	"encoding/json"
	"testing"
)

func TestWordCountBasic(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
	}{
		{"simple sentence", `{"text": "hello world"}`, 2},
		{"multiple spaces", `{"text": "hello    world   test"}`, 3},
		{"empty", `{"text": ""}`, 0},
		{"whitespace only", `{"text": "   "}`, 0},
		{"newlines and tabs", `{"text": "hello\nworld\ttab"}`, 3},
		{"single word", `{"text": "word"}`, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := run([]byte(tt.input))

			var result Output
			if err := json.Unmarshal(output, &result); err != nil {
				t.Fatalf("Failed to parse output: %v", err)
			}

			if result.Error != "" {
				t.Errorf("Unexpected error: %s", result.Error)
			}

			if result.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", result.Count, tt.wantCount)
			}
		})
	}
}

func TestWordCountWords(t *testing.T) {
	output := run([]byte(`{"text": "the quick brown fox"}`))

	var result Output
	_ = json.Unmarshal(output, &result)

	expectedWords := []string{"the", "quick", "brown", "fox"}
	if len(result.Words) != len(expectedWords) {
		t.Fatalf("Words length = %d, want %d", len(result.Words), len(expectedWords))
	}

	for i, word := range expectedWords {
		if result.Words[i] != word {
			t.Errorf("Words[%d] = %s, want %s", i, result.Words[i], word)
		}
	}
}

func TestWordCountInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid json", `not json at all`},
		{"empty", ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := run([]byte(tt.input))

			var result Output
			_ = json.Unmarshal(output, &result)

			if result.Error == "" && result.Count != 0 {
				t.Error("Expected error or zero count for invalid input")
			}
		})
	}
}

func TestWordCountBoundary(t *testing.T) {
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "word "
	}
	input := `{"text": "` + longText + `"}`
	output := run([]byte(input))

	var result Output
	_ = json.Unmarshal(output, &result)

	if result.Count != 1000 {
		t.Errorf("Count = %d, want 1000", result.Count)
	}
}
