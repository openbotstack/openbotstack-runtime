package builtin

import (
	"context"
	"errors"
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// TestRuntimeLLMAccess_NilRouterReturnsError pins the guard: a runtime LLM
// access with no router must fail fast rather than nil-deref. This characterizes
// the production adapter as it is hoisted out of package main.
func TestRuntimeLLMAccess_NilRouterReturnsError(t *testing.T) {
	access := NewRuntimeLLMAccess(nil, 2048, 60*time.Second)
	_, err := access.Generate(context.Background(), aitypes.LLMRequest{
		Contents: []aitypes.ContentBlock{aitypes.NewTextBlock("hi")},
	})
	if err == nil {
		t.Fatal("expected error when router is nil")
	}
}

// TestIsRetryableError pins the transient-error classification that drives
// the retry/backoff loop in Generate.
func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"HTTP 429 Too Many Requests", true},
		{"rate limit exceeded", true},
		{"too many requests", true},
		{"RateLimit: quota exceeded", true},
		{"500 Internal Server Error", true},
		{"502 Bad Gateway", true},
		{"503 Service Unavailable", true},
		{"ServiceUnavailable", true},
		{"Internal Server Error", true},
		{"400 Bad Request", false},
		{"not found", false},
		{"validation failed", false},
		{"unauthorized", false},
	}
	for _, c := range cases {
		if got := isRetryableError(errors.New(c.msg)); got != c.want {
			t.Errorf("isRetryableError(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

// TestExtractModelName pins the model-name extraction used for telemetry.
func TestExtractModelName(t *testing.T) {
	if got := extractModelName(stubProvider{}); got != "unknown" {
		t.Errorf("extractModelName(stubProvider) = %q, want unknown", got)
	}
	if got := extractModelName(namedStub{name: "gpt-4o"}); got != "gpt-4o" {
		t.Errorf("extractModelName(namedStub) = %q, want gpt-4o", got)
	}
}

// stubProvider satisfies providers.ModelProvider without exposing ModelName().
type stubProvider struct{}

func (stubProvider) ID() string                                           { return "stub" }
func (stubProvider) Capabilities() []aitypes.CapabilityType               { return nil }
func (stubProvider) Generate(context.Context, aitypes.GenerateRequest) (*aitypes.GenerateResponse, error) {
	return nil, nil
}
func (stubProvider) Embed(context.Context, []string) ([][]float32, error) { return nil, nil }

type namedStub struct{ name string }

func (n namedStub) ModelName() string                                      { return n.name }
func (namedStub) ID() string                                               { return "named" }
func (namedStub) Capabilities() []aitypes.CapabilityType                   { return nil }
func (namedStub) Generate(context.Context, aitypes.GenerateRequest) (*aitypes.GenerateResponse, error) {
	return nil, nil
}
func (namedStub) Embed(context.Context, []string) ([][]float32, error)     { return nil, nil }
