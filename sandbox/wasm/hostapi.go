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

// SetInput sets the input buffer for the skills.
func (hf *HostFunctions) SetInput(input []byte) {
	hf.inputBuffer = input
}

// GetOutput returns the output buffer from the skills.
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
			memSize := m.Memory().Size()
			n := uint32(len(hf.inputBuffer))
			if ptr >= memSize || ptr+n < ptr || ptr+n > memSize {
				return 0xFFFFFFFF
			}
			if ok := m.Memory().Write(ptr, hf.inputBuffer); !ok {
				return 0xFFFFFFFF
			}
			return n
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

			resultBytes := []byte(result)
			memSize := m.Memory().Size()
			n := uint32(len(resultBytes))
			if resultPtr >= memSize || resultPtr+n < resultPtr || resultPtr+n > memSize {
				return 0xFFFFFFFF
			}
			if ok := m.Memory().Write(resultPtr, resultBytes); !ok {
				return 0xFFFFFFFF
			}
			return n
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

			memSize := m.Memory().Size()
			n := uint32(len(value))
			if valuePtr >= memSize || valuePtr+n < valuePtr || valuePtr+n > memSize {
				return 0xFFFFFFFF
			}
			if ok := m.Memory().Write(valuePtr, value); !ok {
				return 0xFFFFFFFF
			}
			return n
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
			// http_fetch performs a sandboxed HTTP request.
			// Response layout at respPtr: [4 bytes status code (little-endian)] [body bytes].
			// Return value: upper 16 bits = HTTP status code, lower 16 bits = body length.
			NewFunctionBuilder().
			WithFunc(func(ctx context.Context, m api.Module, urlPtr, urlLen, methodPtr, methodLen, bodyPtr, bodyLen, respPtr uint32) uint32 {
				if hf.HTTPFetch == nil {
					return 0
				}

				urlBytes, ok := m.Memory().Read(urlPtr, urlLen)
				if !ok {
					return 0
				}
				methodBytes, ok := m.Memory().Read(methodPtr, methodLen)
				if !ok {
					return 0
				}
				var body []byte
				if bodyLen > 0 {
					bodyBytes, ok := m.Memory().Read(bodyPtr, bodyLen)
					if !ok {
						return 0
					}
					body = bodyBytes
				}

				respBody, statusCode, err := hf.HTTPFetch(ctx, string(urlBytes), string(methodBytes), body)
				if err != nil {
					return 0
				}

				// Cap response body to 1MB to prevent host panic on unbounded Memory.Write
				const maxRespSize = 1 << 20
				if len(respBody) > maxRespSize {
					respBody = respBody[:maxRespSize]
				}

				// Validate buffer bounds before writing to prevent silent data loss.
				// Memory.Write returns false on out-of-bounds but we check explicitly
				// to avoid ambiguity with the return value 0 used for other errors.
				memSize := m.Memory().Size()
				required := uint32(4 + len(respBody))
				if respPtr >= memSize || respPtr+required < respPtr || respPtr+required > memSize {
					return 0xFFFFFFFF // sentinel: buffer overflow
				}

				// Write status code as 4 bytes (little-endian) at respPtr
				if ok := m.Memory().Write(respPtr, []byte{
					byte(statusCode), byte(statusCode >> 8),
					byte(statusCode >> 16), byte(statusCode >> 24),
				}); !ok {
					return 0xFFFFFFFF
				}
				// Write body after status code
				if ok := m.Memory().Write(respPtr+4, respBody); !ok {
					return 0xFFFFFFFF
				}

				return (uint32(statusCode&0xFFFF) << 16) | uint32(len(respBody)&0xFFFF)
			}).
			Export("http_fetch").
			Instantiate(ctx)

	return err
}
