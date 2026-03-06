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
		{
			name:      "simple sentence",
			input:     `{"text": "hello world"}`,
			wantCount: 2,
		},
		{
			name:      "multiple spaces",
			input:     `{"text": "hello    world   test"}`,
			wantCount: 3,
		},
		{
			name:      "empty",
			input:     `{"text": ""}`,
			wantCount: 0,
		},
		{
			name:      "whitespace only",
			input:     `{"text": "   "}`,
			wantCount: 0,
		},
		{
			name:      "newlines and tabs",
			input:     `{"text": "hello\nworld\ttab"}`,
			wantCount: 3,
		},
		{
			name:      "single word",
			input:     `{"text": "word"}`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetInput([]byte(tt.input))
			_ = Execute()

			var output Output
			if err := json.Unmarshal(GetOutput(), &output); err != nil {
				t.Fatalf("Failed to parse output: %v", err)
			}

			if output.Error != "" {
				t.Errorf("Unexpected error: %s", output.Error)
			}

			if output.Count != tt.wantCount {
				t.Errorf("Count = %d, want %d", output.Count, tt.wantCount)
			}
		})
	}
}

func TestWordCountWords(t *testing.T) {
	SetInput([]byte(`{"text": "the quick brown fox"}`))
	_ = Execute()

	var output Output
	_ = json.Unmarshal(GetOutput(), &output)

	expectedWords := []string{"the", "quick", "brown", "fox"}
	if len(output.Words) != len(expectedWords) {
		t.Fatalf("Words length = %d, want %d", len(output.Words), len(expectedWords))
	}

	for i, word := range expectedWords {
		if output.Words[i] != word {
			t.Errorf("Words[%d] = %s, want %s", i, output.Words[i], word)
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
			SetInput([]byte(tt.input))
			_ = Execute()

			var output Output
			_ = json.Unmarshal(GetOutput(), &output)

			// Either error or empty result is acceptable
			if output.Error == "" && output.Count != 0 {
				t.Error("Expected error or zero count for invalid input")
			}
		})
	}
}

func TestWordCountBoundary(t *testing.T) {
	// Very long text
	longText := ""
	for i := 0; i < 1000; i++ {
		longText += "word "
	}
	input := `{"text": "` + longText + `"}`
	SetInput([]byte(input))
	_ = Execute()

	var output Output
	_ = json.Unmarshal(GetOutput(), &output)

	if output.Count != 1000 {
		t.Errorf("Count = %d, want 1000", output.Count)
	}
}
