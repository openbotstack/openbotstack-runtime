// Package wasm provides a sandboxed Wasm runtime for skill execution.
package wasm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
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

// requestBuffers holds per-request input/output buffers, stored in context.
type requestBuffers struct {
	input  []byte
	output []byte
}

// contextKey is an unexported type for context keys defined in this package.
type contextKey int

const buffersKey contextKey = iota

// withBuffers returns a new context carrying per-request buffers.
func withBuffers(ctx context.Context, buf *requestBuffers) context.Context {
	return context.WithValue(ctx, buffersKey, buf)
}

// buffersFromContext retrieves per-request buffers from context.
func buffersFromContext(ctx context.Context) *requestBuffers {
	if buf, ok := ctx.Value(buffersKey).(*requestBuffers); ok {
		return buf
	}
	return nil
}

// Runtime wraps wazero for sandboxed Wasm execution.
type Runtime struct {
	engine          wazero.Runtime
	compileMu       sync.Mutex // protects module compilation
	compiledModules sync.Map   // map[[sha256.Size]byte]wazero.CompiledModule
	hf              *HostFunctions
	limits          Limits
	moduleCounter   uint64 // atomically incremented for unique module names
}

// NewRuntime creates a new Wasm runtime.
func NewRuntime() (*Runtime, error) {
	return NewRuntimeWithLimits(DefaultLimits())
}

// NewRuntimeWithLimits creates a new Wasm runtime with custom resource limits.
func NewRuntimeWithLimits(limits Limits) (*Runtime, error) {
	ctx := context.Background()

	// Enforce memory limit at the runtime level (pages = bytes / 64KB).
	var rtConfig wazero.RuntimeConfig
	if limits.MaxMemoryBytes > 0 {
		pages := uint32(limits.MaxMemoryBytes / 65536)
		if pages < 1 {
			pages = 1
		}
		rtConfig = wazero.NewRuntimeConfig().WithMemoryLimitPages(pages)
	} else {
		rtConfig = wazero.NewRuntimeConfig()
	}

	engine := wazero.NewRuntimeWithConfig(ctx, rtConfig)

	// Instantiate WASI
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, engine); err != nil {
		_ = engine.Close(ctx)
		return nil, err
	}

	return &Runtime{
		engine: engine,
		limits: limits,
	}, nil
}

// RegisterHostFunctions registers the Host API functions in the runtime and stores the reference.
func (r *Runtime) RegisterHostFunctions(ctx context.Context, hf *HostFunctions) error {
	if err := RegisterHostFunctions(ctx, r.engine, hf); err != nil {
		return err
	}
	r.hf = hf
	return nil
}

// GetEngine returns the underlying wazero runtime.
func (r *Runtime) GetEngine() wazero.Runtime {
	return r.engine
}

// moduleHash computes a SHA-256 hash of wasm bytes for cache key.
func moduleHash(wasmBytes []byte) [sha256.Size]byte {
	return sha256.Sum256(wasmBytes)
}

// getOrCompileModule returns a compiled module, using the cache if available.
// Compilation is serialized via compileMu; cache lookups are lock-free.
func (r *Runtime) getOrCompileModule(ctx context.Context, wasmBytes []byte) (wazero.CompiledModule, error) {
	hash := moduleHash(wasmBytes)

	// Fast path: check cache without lock.
	if cached, ok := r.compiledModules.Load(hash); ok {
		return cached.(wazero.CompiledModule), nil
	}

	// Slow path: compile under lock.
	r.compileMu.Lock()
	defer r.compileMu.Unlock()

	// Double-check after acquiring lock (another goroutine may have compiled).
	if cached, ok := r.compiledModules.Load(hash); ok {
		return cached.(wazero.CompiledModule), nil
	}

	compiled, err := r.engine.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	r.compiledModules.Store(hash, compiled)
	return compiled, nil
}

// LoadModule compiles and caches a Wasm module.
func (r *Runtime) LoadModule(ctx context.Context, name string, wasmBytes []byte) (api.Module, error) {
	compiled, err := r.getOrCompileModule(ctx, wasmBytes)
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
//
// Execute is safe for concurrent use. Module compilation is cached and shared;
// per-request state (input/output buffers) is isolated per call.
func (r *Runtime) Execute(ctx context.Context, wasmBytes []byte, input []byte, limits Limits) ([]byte, error) {
	// Apply timeout
	if limits.MaxExecutionTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.MaxExecutionTime)
		defer cancel()
	}

	// Get or compile module (cached after first call; no global lock on hot path).
	compiled, err := r.getOrCompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, err
	}

	// Allocate per-request buffers for Host API isolation.
	// Host functions (get_input, set_output) read/write via context,
	// so concurrent executions do not interfere with each other.
	reqBuf := &requestBuffers{input: input}
	ctx = withBuffers(ctx, reqBuf)

	// Capture stdout for Command-pattern modules (stdin/stdout I/O).
	var stdoutBuf bytes.Buffer

	// Create stdin reader from input.
	var stdinReader io.Reader = bytes.NewReader(input)

	// Configure module with stdin/stdout + prevent auto _start.
	// WithStartFunctions() disables the default auto-call of _start during
	// InstantiateModule, which prevents clock_time_get nil pointer crashes
	// with Go wasip1 runtime initialization.
	// Memory limit is enforced at the Runtime level via WithMemoryLimitPages.
	// Each instantiation gets a unique name to allow concurrent execution.
	instanceName := fmt.Sprintf("skill-%d", atomic.AddUint64(&r.moduleCounter, 1))
	config := wazero.NewModuleConfig().
		WithName(instanceName).
		WithStdin(stdinReader).
		WithStdout(&stdoutBuf).
		WithStderr(os.Stderr).
		WithSysWalltime().
		WithSysNanotime().
		WithStartFunctions() // disable auto _start

	// Instantiate module (no auto-start). No global lock held here.
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
			// Check per-request buffer first, then shared HostFunctions.
			if reqBuf.output != nil {
				return reqBuf.output, nil
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
	// 1. Per-request buffer (context-isolated, takes priority).
	// 2. Shared host functions buffer (backward compat).
	// 3. Stdout capture (Command: stdin/stdout).
	if reqBuf.output != nil {
		return reqBuf.output, nil
	}

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
	// Clean up cached compiled modules.
	r.compiledModules.Range(func(key, value any) bool {
		if cm, ok := value.(wazero.CompiledModule); ok {
			_ = cm.Close(context.Background()) //nolint:errcheck // cleanup
		}
		return true
	})
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
