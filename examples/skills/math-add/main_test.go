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
	}{
		{"positive integers", `{"a": 5, "b": 3}`, 8},
		{"negative numbers", `{"a": -10, "b": 5}`, -5},
		{"floats", `{"a": 1.5, "b": 2.5}`, 4.0},
		{"zero", `{"a": 0, "b": 0}`, 0},
		{"large numbers", `{"a": 999999999, "b": 1}`, 1000000000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := run([]byte(tt.input))

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
			result := run([]byte(tt.input))

			var output Output
			_ = json.Unmarshal(result, &output)
			if output.Error == "" {
				t.Error("Expected error message in output")
			}
		})
	}
}

func TestExecuteMissingFields(t *testing.T) {
	result := run([]byte(`{}`))
	var output Output
	_ = json.Unmarshal(result, &output)
	if output.Sum != 0 {
		t.Errorf("Expected sum 0, got %v", output.Sum)
	}
}

func TestExecuteBoundary(t *testing.T) {
	result := run([]byte(`{"a": 0.0000001, "b": 0.0000002}`))
	var output Output
	_ = json.Unmarshal(result, &output)
	if output.Sum < 0.0000002 || output.Sum > 0.0000004 {
		t.Errorf("Precision issue: %v", output.Sum)
	}
}
