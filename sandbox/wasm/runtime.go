// Package wasm provides a sandboxed Wasm runtime for skill execution.
package wasm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

var (
	// ErrNoEntrypoint is returned when module has no callable entrypoint.
	ErrNoEntrypoint = errors.New("wasm: no entrypoint found")

	// ErrExecutionTimeout is returned when execution exceeds time limit.
	ErrExecutionTimeout = errors.New("wasm: execution timeout")
)

// Limits defines resource constraints for Wasm execution.
type Limits struct {
	MaxMemoryBytes   int64
	MaxExecutionTime time.Duration
}

// DefaultLimits returns sensible defaults.
func DefaultLimits() Limits {
	return Limits{
		MaxMemoryBytes:   128 * 1024 * 1024, // 128MB
		MaxExecutionTime: 30 * time.Second,
	}
}

// Runtime wraps wazero for sandboxed Wasm execution.
type Runtime struct {
	engine wazero.Runtime
	mu     sync.Mutex
	hf     *HostFunctions
}

// NewRuntime creates a new Wasm runtime.
func NewRuntime() (*Runtime, error) {
	ctx := context.Background()
	engine := wazero.NewRuntime(ctx)

	// Instantiate WASI
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, engine); err != nil {
		_ = engine.Close(ctx)
		return nil, err
	}

	return &Runtime{
		engine: engine,
	}, nil
}

// RegisterHostFunctions registers the Host API functions in the runtime and stores the reference.
func (r *Runtime) RegisterHostFunctions(ctx context.Context, hf *HostFunctions) error {
	if err := RegisterHostFunctions(ctx, r.engine, hf); err != nil {
		return err
	}
	r.mu.Lock()
	r.hf = hf
	r.mu.Unlock()
	return nil
}

// GetEngine returns the underlying wazero runtime.
func (r *Runtime) GetEngine() wazero.Runtime {
	return r.engine
}

// LoadModule compiles and caches a Wasm module.
func (r *Runtime) LoadModule(ctx context.Context, name string, wasmBytes []byte) (api.Module, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	compiled, err := r.engine.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	mod, err := r.engine.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithName(name).WithSysWalltime().WithSysNanotime())
	if err != nil {
		return nil, err
	}

	return mod, nil
}

// Execute runs a Wasm module with the given input and limits.
//
// Supports two execution patterns:
//   - Command pattern (Go wasip1): module reads input from stdin, writes output to stdout.
//   - Reactor pattern (TinyGo): module exports "execute", uses host API get_input/set_output.
//
// The runtime auto-detects the pattern by checking for exported functions.
func (r *Runtime) Execute(ctx context.Context, wasmBytes []byte, input []byte, limits Limits) ([]byte, error) {
	// Apply timeout
	if limits.MaxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxExecutionTime)
		defer cancel()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Set execution-specific input in the shared Host API state (Reactor pattern).
	if r.hf != nil {
		r.hf.ClearBuffers()
		r.hf.SetInput(input)
	}

	// Compile
	compiled, err := r.engine.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	// Capture stdout for Command-pattern modules (stdin/stdout I/O).
	var stdoutBuf bytes.Buffer

	// Create stdin reader from input.
	var stdinReader io.Reader = bytes.NewReader(input)

	// Configure module with stdin/stdout + prevent auto _start.
	// WithStartFunctions() disables the default auto-call of _start during
	// InstantiateModule, which prevents clock_time_get nil pointer crashes
	// with Go wasip1 runtime initialization.
	config := wazero.NewModuleConfig().
		WithName("skill").
		WithStdin(stdinReader).
		WithStdout(&stdoutBuf).
		WithStderr(os.Stderr).
		WithSysWalltime().
		WithSysNanotime().
		WithStartFunctions() // disable auto _start

	// Instantiate module (no auto-start).
	mod, err := r.engine.InstantiateModule(ctx, compiled, config)
	if err != nil {
		return nil, fmt.Errorf("wasm: instantiation failed: %w", err)
	}
	defer mod.Close(ctx) //nolint:errcheck // cleanup

	// Find entrypoint.
	// Priority: "execute" (Reactor/TinyGo) > "_start" (Command/Go wasip1)
	fn := mod.ExportedFunction("execute")
	if fn == nil {
		fn = mod.ExportedFunction("_start")
	}
	if fn == nil {
		return nil, ErrNoEntrypoint
	}

	// Call the entrypoint.
	_, err = fn.Call(ctx)
	if err != nil {
		// Go wasip1 Command modules call proc_exit(0) after main() returns.
		// This is normal successful completion, not an error.
		var exitErr *sys.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 0 {
			// Clean exit — collect output from stdout.
			if stdoutBuf.Len() > 0 {
				return stdoutBuf.Bytes(), nil
			}
			if r.hf != nil && len(r.hf.GetOutput()) > 0 {
				return r.hf.GetOutput(), nil
			}
			return nil, nil
		}

		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrExecutionTimeout
		}
		return nil, fmt.Errorf("wasm: execution failed: %w", err)
	}

	// Collect output:
	// 1. If host functions were used (Reactor: set_output), prefer that.
	// 2. Otherwise use stdout capture (Command: stdin/stdout).
	if r.hf != nil && len(r.hf.GetOutput()) > 0 {
		return r.hf.GetOutput(), nil
	}

	if stdoutBuf.Len() > 0 {
		return stdoutBuf.Bytes(), nil
	}

	return nil, nil
}

// Close releases all resources.
func (r *Runtime) Close() error {
	return r.engine.Close(context.Background())
}

// ModuleCache provides LRU caching for compiled modules.
type ModuleCache struct {
	mu       sync.RWMutex
	cache    map[string][]byte
	order    []string
	capacity int
}

// NewModuleCache creates a cache with given capacity.
func NewModuleCache(capacity int) *ModuleCache {
	return &ModuleCache{
		cache:    make(map[string][]byte),
		order:    make([]string, 0, capacity),
		capacity: capacity,
	}
}

// Get retrieves a cached module.
func (c *ModuleCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, ok := c.cache[key]
	return data, ok
}

// Put stores a module, evicting oldest if at capacity.
func (c *ModuleCache) Put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.order) >= c.capacity {
		oldest := c.order[0]
		delete(c.cache, oldest)
		c.order = c.order[1:]
	}

	c.cache[key] = data
	c.order = append(c.order, key)
}
