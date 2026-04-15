// Package tool_invocation provides a pipeline for routing tool invocations
// to appropriate backends (HTTP, registry) with sandboxing and timeout enforcement.
package tool_invocation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

var (
	// ErrUnsupportedToolType is returned for unknown tool types.
	ErrUnsupportedToolType = errors.New("tool_invocation: unsupported tool type")

	// ErrMissingToolName is returned when a tool name/URL is empty.
	ErrMissingToolName = errors.New("tool_invocation: tool name is required")

	// ErrNilHTTPClient is returned when HTTP tool is invoked without a configured client.
	ErrNilHTTPClient = errors.New("tool_invocation: HTTP client not configured")

	// ErrNilRegistryClient is returned when registry tool is invoked without a configured client.
	ErrNilRegistryClient = errors.New("tool_invocation: registry client not configured")
)

// ToolInvocation represents a request to invoke a tool.
type ToolInvocation struct {
	// Name is the tool name or URL.
	Name string

	// Type is the tool backend: "http", "registry".
	Type string

	// Arguments contains tool-specific parameters.
	Arguments map[string]any

	// Meta contains execution metadata for auditing.
	Meta ToolInvocationMeta
}

// ToolInvocationMeta contains metadata for execution tracking.
type ToolInvocationMeta struct {
	TenantID  string
	UserID    string
	RequestID string
	SessionID string
}

// ToolResult contains the output of a tool invocation.
type ToolResult struct {
	Output     []byte
	StatusCode int
	Error      error
	Duration   time.Duration
}

// ToolInvocationPipeline routes tool invocations to the appropriate backend.
// Supported tool types: "http" (sandboxed HTTP via allowlist), "registry" (external registry).
type ToolInvocationPipeline struct {
	httpClient     *wasm.SandboxedHTTPClient
	registryClient *toolrunner.RegistryClient
	timeout        time.Duration
}

// NewToolInvocationPipeline creates a new tool invocation pipeline.
// httpClient and registryClient may be nil; invoking a tool type with a nil client
// returns a descriptive error.
func NewToolInvocationPipeline(
	httpClient *wasm.SandboxedHTTPClient,
	registryClient *toolrunner.RegistryClient,
	timeout time.Duration,
) *ToolInvocationPipeline {
	return &ToolInvocationPipeline{
		httpClient:     httpClient,
		registryClient: registryClient,
		timeout:        timeout,
	}
}

// Invoke executes a tool invocation and returns the result.
func (p *ToolInvocationPipeline) Invoke(ctx context.Context, inv ToolInvocation) (*ToolResult, error) {
	if inv.Name == "" {
		return nil, ErrMissingToolName
	}

	// Apply pipeline-level timeout
	if p.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.timeout)
		defer cancel()
	}

	start := time.Now()

	switch inv.Type {
	case "http":
		return p.invokeHTTP(ctx, inv, start)
	case "registry":
		return p.invokeRegistry(ctx, inv, start)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedToolType, inv.Type)
	}
}

func (p *ToolInvocationPipeline) invokeHTTP(ctx context.Context, inv ToolInvocation, start time.Time) (*ToolResult, error) {
	if p.httpClient == nil {
		return nil, ErrNilHTTPClient
	}

	url, _ := inv.Arguments["url"].(string)
	method, _ := inv.Arguments["method"].(string)

	var body []byte
	if b, ok := inv.Arguments["body"]; ok {
		switch v := b.(type) {
		case []byte:
			body = v
		case string:
			body = []byte(v)
		}
	}

	if url == "" {
		url = inv.Name // fallback to invocation name as URL
	}

	respBody, statusCode, err := p.httpClient.Fetch(ctx, url, method, body)
	duration := time.Since(start)

	result := &ToolResult{
		Output:     respBody,
		StatusCode: statusCode,
		Duration:   duration,
	}
	if err != nil {
		result.Error = err
		return result, err
	}

	return result, nil
}

func (p *ToolInvocationPipeline) invokeRegistry(ctx context.Context, inv ToolInvocation, start time.Time) (*ToolResult, error) {
	if p.registryClient == nil {
		return nil, ErrNilRegistryClient
	}

	tc := &toolrunner.ToolContext{
		Context:   ctx,
		TenantID:  inv.Meta.TenantID,
		UserID:    inv.Meta.UserID,
		RequestID: inv.Meta.RequestID,
		StartTime: start,
	}

	output, err := p.registryClient.Invoke(tc, inv.Name, inv.Arguments)
	duration := time.Since(start)

	result := &ToolResult{
		Output:   output,
		Duration: duration,
	}
	if err != nil {
		result.Error = err
		return result, err
	}

	return result, nil
}
