package tool_invocation

import (
	"context"

	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// WireHTTPFetch connects the ToolInvocationPipeline to Wasm HostFunctions.HTTPFetch.
// After calling this, Wasm skills can make sandboxed HTTP requests via the host API.
func WireHTTPFetch(hf *wasm.HostFunctions, pipeline *ToolInvocationPipeline) {
	hf.HTTPFetch = func(ctx context.Context, url, method string, body []byte) ([]byte, int, error) {
		result, err := pipeline.Invoke(ctx, ToolInvocation{
			Name: url,
			Type: "http",
			Arguments: map[string]any{
				"url":    url,
				"method": method,
				"body":   body,
			},
		})
		if err != nil {
			return nil, 0, err
		}
		return result.Output, result.StatusCode, nil
	}
}
