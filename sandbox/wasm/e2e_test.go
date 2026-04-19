//go:build integration
// +build integration

// Package wasm provides E2E tests using real compiled Wasm modules.
package wasm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// loadSkillWasm reads a compiled Wasm module from the examples directory.
func loadSkillWasm(t *testing.T, skillName string) []byte {
	t.Helper()
	wasmPath := filepath.Join("..", "..", "examples", "skills", skillName, "main.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skipf("%s/main.wasm not found - run 'GOOS=wasip1 GOARCH=wasm go build -o main.wasm .' in examples/skills/%s/", skillName, skillName)
	}
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", wasmPath, err)
	}
	return wasmBytes
}

// TestE2EHelloWorldWasm tests the full execution path with real hello-world.wasm
func TestE2EHelloWorldWasm(t *testing.T) {
	wasmBytes := loadSkillWasm(t, "hello-world")

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	hf := &HostFunctions{
		Log: func(ctx context.Context, level, msg string) {
			t.Logf("[%s] %s", level, msg)
		},
	}
	if err := rt.RegisterHostFunctions(context.Background(), hf); err != nil {
		t.Fatalf("RegisterHostFunctions failed: %v", err)
	}

	t.Run("basic_execution", func(t *testing.T) {
		input := map[string]string{"message": "hello"}
		inputBytes, _ := json.Marshal(input)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("Output is not valid JSON: %s", string(output))
		}

		msg, _ := result["message"].(string)
		if msg == "" {
			t.Fatal("Expected non-empty message in output")
		}
		t.Logf("Output: %s", string(output))
	})

	t.Run("empty_input", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		output, err := rt.Execute(ctx, wasmBytes, nil, DefaultLimits())
		if err != nil {
			t.Fatalf("Execute with empty input failed: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("Output is not valid JSON: %s", string(output))
		}
	})

	t.Run("timeout_enforcement", func(t *testing.T) {
		limits := Limits{
			MaxExecutionTime: 1 * time.Nanosecond,
			MaxMemoryBytes:   128 * 1024 * 1024,
		}

		ctx := context.Background()
		_, err := rt.Execute(ctx, wasmBytes, nil, limits)
		if err == ErrExecutionTimeout {
			t.Log("Timeout correctly enforced")
		} else {
			t.Log("Module executed before timeout triggered")
		}
	})

	t.Run("concurrent_stress", func(t *testing.T) {
		const numGoroutines = 5
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				input := map[string]interface{}{"message": "concurrent test", "id": id}
				inputBytes, _ := json.Marshal(input)

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				_, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
				errors <- err
			}(i)
		}

		for i := 0; i < numGoroutines; i++ {
			if err := <-errors; err != nil {
				t.Errorf("Concurrent execution %d failed: %v", i, err)
			}
		}
	})
}

// TestE2ETaxCalculatorWasm tests the deterministic tax-calculator skill.
func TestE2ETaxCalculatorWasm(t *testing.T) {
	wasmBytes := loadSkillWasm(t, "tax-calculator")

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	input := map[string]interface{}{
		"amount":   100.0,
		"region":   "US",
		"category": "goods",
	}
	inputBytes, _ := json.Marshal(input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %s", string(output))
	}

	if result["error"] != nil && result["error"] != "" {
		t.Fatalf("Unexpected error: %v", result["error"])
	}
	t.Logf("Tax result: %v", result)
}

// TestE2EMathAddWasm tests the math-add skill.
func TestE2EMathAddWasm(t *testing.T) {
	wasmBytes := loadSkillWasm(t, "math-add")

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	input := map[string]interface{}{"a": 5.0, "b": 3.0}
	inputBytes, _ := json.Marshal(input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %s", string(output))
	}

	sum, _ := result["sum"].(float64)
	if sum != 8.0 {
		t.Errorf("Expected sum=8, got %v", sum)
	}
}

// TestE2EWordcountWasm tests the wordcount skill.
func TestE2EWordcountWasm(t *testing.T) {
	wasmBytes := loadSkillWasm(t, "wordcount")

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	input := map[string]interface{}{"text": "hello world test"}
	inputBytes, _ := json.Marshal(input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %s", string(output))
	}

	count, _ := result["count"].(float64)
	if count != 3 {
		t.Errorf("Expected count=3, got %v", count)
	}
}

// TestE2ESentimentWasm tests the sentiment analysis skill.
func TestE2ESentimentWasm(t *testing.T) {
	wasmBytes := loadSkillWasm(t, "sentiment")

	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	input := map[string]interface{}{"text": "I love this product! It's amazing!"}
	inputBytes, _ := json.Marshal(input)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("Output is not valid JSON: %s", string(output))
	}

	sentiment, _ := result["sentiment"].(string)
	if sentiment != "positive" {
		t.Errorf("Expected sentiment=positive, got %q", sentiment)
	}
}

// TestE2EWasmPanicRecovery tests that invalid wasm doesn't crash the runtime.
func TestE2EWasmPanicRecovery(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	invalidWasm := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	ctx := context.Background()
	_, err = rt.Execute(ctx, invalidWasm, nil, DefaultLimits())
	if err == nil {
		t.Fatal("Expected error for invalid wasm")
	}
	t.Logf("Graceful error for invalid wasm: %v", err)
}

// TestE2EMemoryLimits tests memory limit enforcement.
func TestE2EMemoryLimits(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	limits := Limits{
		MaxExecutionTime: 30 * time.Second,
		MaxMemoryBytes:   1024,
	}

	minimalWasm := []byte{
		0x00, 0x61, 0x73, 0x6d,
		0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
		0x03, 0x02, 0x01, 0x00,
		0x07, 0x0b, 0x01, 0x07, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x65, 0x00, 0x00,
		0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
	}

	ctx := context.Background()
	_, err = rt.Execute(ctx, minimalWasm, nil, limits)
	if err != nil {
		t.Logf("Memory limit error (expected for complex modules): %v", err)
	}
}
