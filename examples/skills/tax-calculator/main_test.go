package main

import (
	"encoding/json"
	"math"
	"testing"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.001
}

func TestUSGoods(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 100.00, Region: "US", Category: "goods"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if !almostEqual(output.TaxRate, 0.08) {
		t.Errorf("Expected tax rate 0.08, got %f", output.TaxRate)
	}
	if !almostEqual(output.TaxAmount, 8.00) {
		t.Errorf("Expected tax 8.00, got %f", output.TaxAmount)
	}
	if !almostEqual(output.TotalAmount, 108.00) {
		t.Errorf("Expected total 108.00, got %f", output.TotalAmount)
	}
}

func TestEUDigital(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 50.00, Region: "EU", Category: "digital"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if !almostEqual(output.TaxRate, 0.23) {
		t.Errorf("Expected tax rate 0.23, got %f", output.TaxRate)
	}
	if !almostEqual(output.TaxAmount, 11.50) {
		t.Errorf("Expected tax 11.50, got %f", output.TaxAmount)
	}
}

func TestCNServices(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 1000.00, Region: "CN", Category: "services"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if !almostEqual(output.TaxRate, 0.06) {
		t.Errorf("Expected tax rate 0.06, got %f", output.TaxRate)
	}
	if !almostEqual(output.TaxAmount, 60.00) {
		t.Errorf("Expected tax 60.00, got %f", output.TaxAmount)
	}
}

func TestNegativeAmount(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: -100, Region: "US", Category: "goods"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for negative amount")
	}
}

func TestZeroAmount(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 0, Region: "US", Category: "goods"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for zero amount")
	}
}

func TestInvalidRegion(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 100, Region: "XX", Category: "goods"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for invalid region")
	}
}

func TestInvalidCategory(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 100, Region: "US", Category: "invalid"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for invalid category")
	}
}

func TestExcessiveAmount(t *testing.T) {
	ResetBuffers()
	input := TaxInput{Amount: 2000000000, Region: "US", Category: "goods"}
	data, _ := json.Marshal(input)
	SetInput(data)

	Execute()

	var output TaxOutput
	json.Unmarshal(GetOutput(), &output)

	if output.Error == "" {
		t.Error("Expected error for excessive amount")
	}
}

// Determinism test - same input MUST produce same output
func TestDeterminism(t *testing.T) {
	for i := 0; i < 100; i++ {
		ResetBuffers()
		input := TaxInput{Amount: 123.45, Region: "EU", Category: "goods"}
		data, _ := json.Marshal(input)
		SetInput(data)

		Execute()

		var output TaxOutput
		json.Unmarshal(GetOutput(), &output)

		// Must be exactly the same every time
		if !almostEqual(output.TaxAmount, 24.69) {
			t.Errorf("Iteration %d: non-deterministic result: %f", i, output.TaxAmount)
		}
	}
}
