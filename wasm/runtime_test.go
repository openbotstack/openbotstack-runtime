package wasm_test

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/wasm"
)

// Minimal valid Wasm module (empty module)
var minimalWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic: \0asm
	0x01, 0x00, 0x00, 0x00, // version: 1
}

func TestRuntimeCreate(t *testing.T) {
	rt, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("NewRuntime failed: %v", err)
	}
	defer rt.Close() //nolint:errcheck // test cleanup

	if rt == nil {
		t.Fatal("NewRuntime returned nil")
	}
}

func TestRuntimeLoadModule(t *testing.T) {
	rt, _ := wasm.NewRuntime()
	defer rt.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	mod, err := rt.LoadModule(ctx, "test", minimalWasm)
	if err != nil {
		t.Fatalf("LoadModule failed: %v", err)
	}

	if mod == nil {
		t.Fatal("LoadModule returned nil module")
	}
}

func TestRuntimeLoadInvalidModule(t *testing.T) {
	rt, _ := wasm.NewRuntime()
	defer rt.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	_, err := rt.LoadModule(ctx, "invalid", []byte{0x00, 0x00})
	if err == nil {
		t.Error("Expected error for invalid module")
	}
}

func TestRuntimeExecuteWithLimits(t *testing.T) {
	rt, _ := wasm.NewRuntime()
	defer rt.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()
	limits := wasm.Limits{
		MaxMemoryBytes:   128 * 1024 * 1024, // 128MB
		MaxExecutionTime: 30 * time.Second,
	}

	_, err := rt.Execute(ctx, minimalWasm, nil, limits)
	// Minimal module has no exports, so execution is a no-op
	if err != nil && err != wasm.ErrNoEntrypoint {
		t.Logf("Execute returned: %v (expected for minimal module)", err)
	}
}

func TestRuntimeClose(t *testing.T) {
	rt, _ := wasm.NewRuntime()
	err := rt.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestModuleCacheHit(t *testing.T) {
	cache := wasm.NewModuleCache(10)

	// First load
	cache.Put("skill-v1", minimalWasm)

	// Cache hit
	cached, ok := cache.Get("skill-v1")
	if !ok {
		t.Error("Expected cache hit")
	}
	if len(cached) != len(minimalWasm) {
		t.Error("Cached data mismatch")
	}
}

func TestModuleCacheMiss(t *testing.T) {
	cache := wasm.NewModuleCache(10)

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Error("Expected cache miss")
	}
}

func TestModuleCacheEviction(t *testing.T) {
	cache := wasm.NewModuleCache(2) // Only 2 entries

	cache.Put("a", minimalWasm)
	cache.Put("b", minimalWasm)
	cache.Put("c", minimalWasm) // Should evict "a"

	_, ok := cache.Get("a")
	if ok {
		t.Error("Expected 'a' to be evicted")
	}

	_, ok = cache.Get("c")
	if !ok {
		t.Error("Expected 'c' to be present")
	}
}

func TestDefaultLimits(t *testing.T) {
	limits := wasm.DefaultLimits()

	if limits.MaxMemoryBytes != 128*1024*1024 {
		t.Errorf("Expected 128MB, got %d", limits.MaxMemoryBytes)
	}

	if limits.MaxExecutionTime != 30*time.Second {
		t.Errorf("Expected 30s, got %v", limits.MaxExecutionTime)
	}
}

func TestRuntimeMultipleModules(t *testing.T) {
	rt, _ := wasm.NewRuntime()
	defer rt.Close() //nolint:errcheck // test cleanup

	ctx := context.Background()

	// Load two modules
	mod1, err := rt.LoadModule(ctx, "mod1", minimalWasm)
	if err != nil {
		t.Fatalf("LoadModule mod1 failed: %v", err)
	}
	if mod1 == nil {
		t.Error("mod1 is nil")
	}

	mod2, err := rt.LoadModule(ctx, "mod2", minimalWasm)
	if err != nil {
		t.Fatalf("LoadModule mod2 failed: %v", err)
	}
	if mod2 == nil {
		t.Error("mod2 is nil")
	}
}

func TestModuleCacheUpdate(t *testing.T) {
	cache := wasm.NewModuleCache(10)

	// Put same key twice
	cache.Put("key", []byte{1})
	cache.Put("key", []byte{2})

	data, ok := cache.Get("key")
	if !ok {
		t.Error("Key should exist")
	}
	if len(data) != 1 || data[0] != 2 {
		t.Errorf("Expected updated data, got %v", data)
	}
}
