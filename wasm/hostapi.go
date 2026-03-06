package wasm

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// HostFunctions provides the Host API for Wasm skills.
type HostFunctions struct {
	// LLMGenerate is called when skill needs text generation.
	LLMGenerate func(ctx context.Context, prompt string) (string, error)

	// KVGet retrieves a value from key-value store.
	KVGet func(ctx context.Context, key string) ([]byte, error)

	// KVSet stores a value in key-value store.
	KVSet func(ctx context.Context, key string, value []byte) error

	// HTTPFetch performs an HTTP request (sandboxed).
	HTTPFetch func(ctx context.Context, url string, method string, body []byte) ([]byte, int, error)

	// Log writes to structured log.
	Log func(ctx context.Context, level string, msg string)

	// inputBuffer holds input data for the skill
	inputBuffer []byte

	// outputBuffer holds output data from the skill
	outputBuffer []byte
}

// SetInput sets the input buffer for the skill.
func (hf *HostFunctions) SetInput(input []byte) {
	hf.inputBuffer = input
}

// GetOutput returns the output buffer from the skill.
func (hf *HostFunctions) GetOutput() []byte {
	return hf.outputBuffer
}

// ClearBuffers resets input and output buffers.
func (hf *HostFunctions) ClearBuffers() {
	hf.inputBuffer = nil
	hf.outputBuffer = nil
}

// RegisterHostFunctions binds host functions to the Wasm runtime.
func RegisterHostFunctions(ctx context.Context, r wazero.Runtime, hf *HostFunctions) error {
	_, err := r.NewHostModuleBuilder("env").
		// get_input_len returns the length of input data
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context) uint32 {
			return uint32(len(hf.inputBuffer))
		}).
		Export("get_input_len").
		// get_input copies input data to guest memory
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr uint32) uint32 {
			if len(hf.inputBuffer) == 0 {
				return 0
			}
			m.Memory().Write(ptr, hf.inputBuffer)
			return uint32(len(hf.inputBuffer))
		}).
		Export("get_input").
		// set_output copies output data from guest memory
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, ptr, len uint32) {
			data, ok := m.Memory().Read(ptr, len)
			if ok {
				hf.outputBuffer = make([]byte, len)
				copy(hf.outputBuffer, data)
			}
		}).
		Export("set_output").
		// llm_generate calls the LLM provider
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, promptPtr, promptLen, resultPtr uint32) uint32 {
			prompt, ok := m.Memory().Read(promptPtr, promptLen)
			if !ok || hf.LLMGenerate == nil {
				return 0
			}

			result, err := hf.LLMGenerate(ctx, string(prompt))
			if err != nil {
				return 0
			}

			m.Memory().Write(resultPtr, []byte(result))
			return uint32(len(result))
		}).
		Export("llm_generate").
		// kv_get retrieves a value
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, keyPtr, keyLen, valuePtr uint32) uint32 {
			key, ok := m.Memory().Read(keyPtr, keyLen)
			if !ok || hf.KVGet == nil {
				return 0
			}

			value, err := hf.KVGet(ctx, string(key))
			if err != nil {
				return 0
			}

			m.Memory().Write(valuePtr, value)
			return uint32(len(value))
		}).
		Export("kv_get").
		// kv_set stores a value
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, keyPtr, keyLen, valuePtr, valueLen uint32) uint32 {
			key, ok := m.Memory().Read(keyPtr, keyLen)
			if !ok {
				return 0
			}
			value, ok := m.Memory().Read(valuePtr, valueLen)
			if !ok || hf.KVSet == nil {
				return 0
			}

			err := hf.KVSet(ctx, string(key), value)
			if err != nil {
				return 0
			}
			return 1
		}).
		Export("kv_set").
		// log writes a log message
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, m api.Module, levelPtr, levelLen, msgPtr, msgLen uint32) {
			level, _ := m.Memory().Read(levelPtr, levelLen)
			msg, _ := m.Memory().Read(msgPtr, msgLen)
			if hf.Log != nil {
				hf.Log(ctx, string(level), string(msg))
			}
		}).
		Export("log").
		Instantiate(ctx)

	return err
}
