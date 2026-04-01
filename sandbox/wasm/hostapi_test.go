package wasm

import (
	"context"
	"testing"
)

func TestHostFunctionsInputOutput(t *testing.T) {
	hf := &HostFunctions{}

	// Test SetInput
	input := []byte(`{"message": "hello"}`)
	hf.SetInput(input)

	if len(hf.inputBuffer) != len(input) {
		t.Errorf("Input buffer length: expected %d, got %d", len(input), len(hf.inputBuffer))
	}

	// Test GetOutput before setting
	if hf.GetOutput() != nil {
		t.Error("Output should be nil before setting")
	}

	// Simulate skill setting output
	hf.outputBuffer = []byte(`{"result": "world"}`)
	output := hf.GetOutput()
	if string(output) != `{"result": "world"}` {
		t.Errorf("Unexpected output: %s", output)
	}

	// Test ClearBuffers
	hf.ClearBuffers()
	if hf.inputBuffer != nil || hf.outputBuffer != nil {
		t.Error("Buffers should be nil after clear")
	}
}

func TestHostFunctionsLLMGenerate(t *testing.T) {
	called := false
	hf := &HostFunctions{
		LLMGenerate: func(ctx context.Context, prompt string) (string, error) {
			called = true
			if prompt != "test prompt" {
				t.Errorf("Unexpected prompt: %s", prompt)
			}
			return "test response", nil
		},
	}

	// Can't fully test without Wasm module, but verify function is set
	if hf.LLMGenerate == nil {
		t.Error("LLMGenerate should be set")
	}

	// Call directly to verify
	result, err := hf.LLMGenerate(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("LLMGenerate failed: %v", err)
	}
	if !called {
		t.Error("LLMGenerate was not called")
	}
	if result != "test response" {
		t.Errorf("Unexpected result: %s", result)
	}
}

func TestHostFunctionsKV(t *testing.T) {
	store := make(map[string][]byte)

	hf := &HostFunctions{
		KVGet: func(ctx context.Context, key string) ([]byte, error) {
			return store[key], nil
		},
		KVSet: func(ctx context.Context, key string, value []byte) error {
			store[key] = value
			return nil
		},
	}

	// Test KVSet
	err := hf.KVSet(context.Background(), "test-key", []byte("test-value"))
	if err != nil {
		t.Fatalf("KVSet failed: %v", err)
	}

	// Test KVGet
	value, err := hf.KVGet(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("KVGet failed: %v", err)
	}
	if string(value) != "test-value" {
		t.Errorf("Unexpected value: %s", value)
	}
}

func TestHostFunctionsLog(t *testing.T) {
	var loggedLevel, loggedMsg string

	hf := &HostFunctions{
		Log: func(ctx context.Context, level, msg string) {
			loggedLevel = level
			loggedMsg = msg
		},
	}

	hf.Log(context.Background(), "info", "test message")

	if loggedLevel != "info" {
		t.Errorf("Expected level 'info', got '%s'", loggedLevel)
	}
	if loggedMsg != "test message" {
		t.Errorf("Expected msg 'test message', got '%s'", loggedMsg)
	}
}

func TestHostFunctionsNil(t *testing.T) {
	hf := &HostFunctions{}

	// Should not panic with nil functions
	if hf.LLMGenerate != nil {
		t.Error("LLMGenerate should be nil by default")
	}
	if hf.KVGet != nil {
		t.Error("KVGet should be nil by default")
	}
	if hf.KVSet != nil {
		t.Error("KVSet should be nil by default")
	}
	if hf.Log != nil {
		t.Error("Log should be nil by default")
	}
}
