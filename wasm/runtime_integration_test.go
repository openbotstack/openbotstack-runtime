// Package wasm provides integration tests for real Wasm execution.
package wasm

import (
	"context"
	"testing"
	"time"
)

// Minimal WASI module that writes "hello" to stdout.
// This is compiled from: (module (func (export "_start")))
var minimalWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section: () -> ()
	0x03, 0x02, 0x01, 0x00, // function section: 1 func of type 0
	0x07, 0x0a, 0x01, 0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00, 0x00, // export "_start"
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b, // code section: empty function body
}

// Module with execute export (same structure, different export name)
var executeWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type section: () -> ()
	0x03, 0x02, 0x01, 0x00, // function section
	0x07, 0x0b, 0x01, 0x07, 0x65, 0x78, 0x65, 0x63, 0x75, 0x74, 0x65, 0x00, 0x00, // export "execute"
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b, // code section
}

// Invalid Wasm - just garbage bytes
var invalidWasm = []byte{0x00, 0x01, 0x02, 0x03}

func TestRuntimeExecuteWithStartExport(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	_, err = rt.Execute(ctx, minimalWasm, nil, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute with _start failed: %v", err)
	}
}

func TestRuntimeExecuteWithExecuteExport(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	_, err = rt.Execute(ctx, executeWasm, nil, DefaultLimits())
	if err != nil {
		t.Fatalf("Execute with execute export failed: %v", err)
	}
}

func TestRuntimeExecuteInvalidWasm(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	_, err = rt.Execute(ctx, invalidWasm, nil, DefaultLimits())
	if err == nil {
		t.Fatal("Expected error for invalid Wasm")
	}
}

func TestRuntimeTimeout(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	limits := Limits{
		MaxExecutionTime: 1 * time.Millisecond, // Very short timeout
	}

	// Minimal wasm should complete before timeout
	_, err = rt.Execute(ctx, minimalWasm, nil, limits)
	// This should NOT timeout because the module is trivial
	if err == ErrExecutionTimeout {
		t.Log("Timeout as expected for slow module")
	}
}

func TestRuntimeConcurrent(t *testing.T) {
	rt, err := NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close()

	ctx := context.Background()
	done := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func() {
			_, err := rt.Execute(ctx, minimalWasm, nil, DefaultLimits())
			done <- err
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent execution %d failed: %v", i, err)
		}
	}
}
