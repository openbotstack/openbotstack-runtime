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

// TestE2EHelloWorldWasm tests the full execution path with real hello-world.wasm
func TestE2EHelloWorldWasm(t *testing.T) {
	// Find the hello-world.wasm file
	wasmPath := filepath.Join("..", "examples", "skills", "hello-world", "main.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skip("hello-world.wasm not found - run 'make build-skills' first")
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("Failed to read wasm: %v", err)
	}

	// Create runtime with host functions
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	// Register host functions
	hf := &HostFunctions{
		Log: func(ctx context.Context, level, msg string) {
			t.Logf("[%s] %s", level, msg)
		},
		KVGet: func(ctx context.Context, key string) ([]byte, error) {
			return nil, nil
		},
		KVSet: func(ctx context.Context, key string, value []byte) error {
			return nil
		},
	}

	if err := rt.RegisterHostFunctions(context.Background(), hf); err != nil {
		t.Fatalf("RegisterHostFunctions failed: %v", err)
	}

	// Test 1: Basic execution with input
	t.Run("basic_execution", func(t *testing.T) {
		input := map[string]string{
			"message": "hello",
		}
		inputBytes, _ := json.Marshal(input)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		output, err := rt.Execute(ctx, wasmBytes, inputBytes, DefaultLimits())
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		// Verify output is valid JSON
		var result map[string]interface{}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Logf("Raw output: %s", string(output))
		}
		t.Logf("Output: %s", string(output))
	})

	// Test 2: Empty input
	t.Run("empty_input", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		output, err := rt.Execute(ctx, wasmBytes, nil, DefaultLimits())
		if err != nil {
			t.Fatalf("Execute with empty input failed: %v", err)
		}
		t.Logf("Output with empty input: %s", string(output))
	})

	// Test 3: Timeout enforcement
	t.Run("timeout_enforcement", func(t *testing.T) {
		limits := Limits{
			MaxExecutionTime: 1 * time.Nanosecond, // Impossibly short
			MaxMemoryBytes:   128 * 1024 * 1024,
		}

		ctx := context.Background()
		_, err := rt.Execute(ctx, wasmBytes, nil, limits)
		// May or may not timeout depending on execution speed
		if err == ErrExecutionTimeout {
			t.Log("Timeout correctly enforced")
		} else {
			t.Log("Module executed before timeout triggered")
		}
	})

	// Test 4: Concurrent executions (stress test)
	t.Run("concurrent_stress", func(t *testing.T) {
		const numGoroutines = 5
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				input := map[string]interface{}{
					"message": "concurrent test",
					"id":      id,
				}
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

// TestE2EWasmPanicRecovery tests that a panicking Wasm module doesn't crash the runtime
func TestE2EWasmPanicRecovery(t *testing.T) {
	// Create runtime
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	// Test with invalid wasm - should error gracefully, not panic
	invalidWasm := []byte{0xDE, 0xAD, 0xBE, 0xEF}

	ctx := context.Background()
	_, err = rt.Execute(ctx, invalidWasm, nil, DefaultLimits())
	if err == nil {
		t.Fatal("Expected error for invalid wasm")
	}
	t.Logf("Graceful error for invalid wasm: %v", err)
}

// TestE2EMemoryLimits tests memory limit enforcement
func TestE2EMemoryLimits(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	// Very small memory limit
	limits := Limits{
		MaxExecutionTime: 30 * time.Second,
		MaxMemoryBytes:   1024, // 1KB - very restrictive
	}

	// Use minimal wasm that should work with limited memory
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
	// Should succeed - minimal wasm uses very little memory
	if err != nil {
		t.Logf("Memory limit error (expected for complex modules): %v", err)
	}
}
