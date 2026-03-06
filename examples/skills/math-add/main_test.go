package main

import (
	"encoding/json"
	"testing"
)

func TestExecuteSuccess(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSum float64
		wantErr bool
	}{
		{
			name:    "positive integers",
			input:   `{"a": 5, "b": 3}`,
			wantSum: 8,
		},
		{
			name:    "negative numbers",
			input:   `{"a": -10, "b": 5}`,
			wantSum: -5,
		},
		{
			name:    "floats",
			input:   `{"a": 1.5, "b": 2.5}`,
			wantSum: 4.0,
		},
		{
			name:    "zero",
			input:   `{"a": 0, "b": 0}`,
			wantSum: 0,
		},
		{
			name:    "large numbers",
			input:   `{"a": 999999999, "b": 1}`,
			wantSum: 1000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Execute([]byte(tt.input))
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			var output Output
			if err := json.Unmarshal(result, &output); err != nil {
				t.Fatalf("Failed to parse output: %v", err)
			}

			if output.Error != "" {
				t.Errorf("Unexpected error: %s", output.Error)
			}

			if output.Sum != tt.wantSum {
				t.Errorf("Sum = %v, want %v", output.Sum, tt.wantSum)
			}
		})
	}
}

func TestExecuteInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ``},
		{"invalid json", `not json`},
		{"wrong types", `{"a": "not a number", "b": 1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Execute([]byte(tt.input))
			// Should return result with error, not fail
			if err != nil {
				t.Fatalf("Execute should not return error: %v", err)
			}

			var output Output
			_ = json.Unmarshal(result, &output)
			if output.Error == "" {
				t.Error("Expected error message in output")
			}
		})
	}
}

// Note: {} is valid JSON and unmarshal produces {a:0, b:0} - this is expected behavior
func TestExecuteMissingFields(t *testing.T) {
	result, _ := Execute([]byte(`{}`))
	var output Output
	_ = json.Unmarshal(result, &output)
	// 0 + 0 = 0, no error expected
	if output.Sum != 0 {
		t.Errorf("Expected sum 0, got %v", output.Sum)
	}
}

func TestExecuteBoundary(t *testing.T) {
	// Edge case: very small numbers
	result, _ := Execute([]byte(`{"a": 0.0000001, "b": 0.0000002}`))
	var output Output
	_ = json.Unmarshal(result, &output)
	if output.Sum < 0.0000002 || output.Sum > 0.0000004 {
		t.Errorf("Precision issue: %v", output.Sum)
	}
}
