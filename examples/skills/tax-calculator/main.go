// Package main implements a deterministic tax calculator skill.
//
// This skill demonstrates:
//   - Pure code logic with NO LLM calls
//   - Fully deterministic, testable behavior
//   - Governance-ready calculations
//
// Build for wasm:
//
//	tinygo build -o tax.wasm -target wasi -scheduler=none main.go
package main

import (
	"encoding/json"
	"fmt"
)

// TaxInput is the input from the runtime.
type TaxInput struct {
	Amount   float64 `json:"amount"`
	Region   string  `json:"region"`   // US, EU, CN
	Category string  `json:"category"` // goods, services, digital
}

// TaxOutput is the output to the runtime.
type TaxOutput struct {
	Amount      float64 `json:"amount"`
	TaxRate     float64 `json:"tax_rate"`
	TaxAmount   float64 `json:"tax_amount"`
	TotalAmount float64 `json:"total_amount"`
	Region      string  `json:"region"`
	Category    string  `json:"category"`
	Error       string  `json:"error,omitempty"`
}

// Tax rates by region and category (deterministic, auditable)
var taxRates = map[string]map[string]float64{
	"US": {
		"goods":    0.08,
		"services": 0.06,
		"digital":  0.00,
	},
	"EU": {
		"goods":    0.20,
		"services": 0.20,
		"digital":  0.23,
	},
	"CN": {
		"goods":    0.13,
		"services": 0.06,
		"digital":  0.06,
	},
}

// calculateTax runs the core tax calculation logic.
func calculateTax(inputData []byte) []byte {
	if len(inputData) == 0 {
		return nil
	}

	var input TaxInput
	if err := json.Unmarshal(inputData, &input); err != nil {
		return marshalError("invalid input: " + err.Error())
	}

	// Validate input
	if input.Amount <= 0 {
		return marshalError("amount must be positive")
	}
	if input.Amount > 1000000000 {
		return marshalError("amount exceeds maximum (1 billion)")
	}

	regionRates, ok := taxRates[input.Region]
	if !ok {
		return marshalError(fmt.Sprintf("unsupported region: %s (supported: US, EU, CN)", input.Region))
	}

	rate, ok := regionRates[input.Category]
	if !ok {
		return marshalError(fmt.Sprintf("unsupported category: %s (supported: goods, services, digital)", input.Category))
	}

	// Deterministic calculation
	taxAmount := input.Amount * rate
	totalAmount := input.Amount + taxAmount

	output := TaxOutput{
		Amount:      input.Amount,
		TaxRate:     rate,
		TaxAmount:   taxAmount,
		TotalAmount: totalAmount,
		Region:      input.Region,
		Category:    input.Category,
	}

	data, _ := json.Marshal(output)
	return data
}

func marshalError(msg string) []byte {
	output := TaxOutput{Error: msg}
	data, _ := json.Marshal(output)
	return data
}

// main is the library initialization entry point.
func main() {}

// Test helpers
var (
	inputBuffer  []byte
	outputBuffer []byte
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

// Execute runs the calculation using internal buffers (for testing).
func Execute() error {
	outputBuffer = calculateTax(inputBuffer)
	return nil
}
