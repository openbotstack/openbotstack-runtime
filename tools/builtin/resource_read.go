package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-runtime/resource"
)

// ResourceReadTool implements the resource_read builtin tool.
// It fetches content from a URL (https:// for v1), detects the content type,
// and normalises the raw bytes into a resource.Document.
//
// Design (per Part 1–7):
//   - Source can be https:// (file://, mcp://, obs:// in future versions).
//   - Return type is always Document — the canonical format-agnostic output.
//   - Reuses the shared SSRF-safe HTTP transport from httpclient.go.
//   - Does NOT invoke vision automatically — planner decides.
//   - Document → Chunk → Embedding → Knowledge is a future pipeline.
type ResourceReadTool struct {
	Timeout  time.Duration
	MaxBytes int64
	// allowPrivateIPs disables SSRF protection for private network addresses.
	// Intended for testing with httptest servers. Must NOT be true in production.
	allowPrivateIPs bool
}

func (t *ResourceReadTool) Name() string        { return "resource_read" }
func (t *ResourceReadTool) Description() string { return "Fetches a resource (URL) and returns a structured Document with extracted text, title, content type, and layout classification." }
func (t *ResourceReadTool) Parameters() map[string]string {
	return map[string]string{"url": "string"}
}
func (t *ResourceReadTool) Required() []string    { return []string{"url"} }
func (t *ResourceReadTool) Permissions() []string { return []string{"http.fetch"} }

func (t *ResourceReadTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	rawURL, _ := input["url"].(string)
	if rawURL == "" {
		return nil, fmt.Errorf("resource_read: url is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("resource_read: url must start with http:// or https://")
	}

	timeout := t.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("resource_read: %w", err)
	}

	// Reuse the shared SSRF-safe HTTP transport (identical policy as web_fetch).
	client := newSSRFClient(t.allowPrivateIPs)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("resource_read: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	maxBytes := t.MaxBytes
	if maxBytes == 0 {
		maxBytes = 10 * 1024 * 1024 // 10 MiB for documents (larger than web_fetch's 1 MiB)
	}
	data, truncated, err := readLimited(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("resource_read: read body: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")

	doc := resource.ReadResource(rawURL, data, contentType)
	doc.Truncated = truncated

	// Convert Document to map[string]any via JSON round-trip so the caller
	// always sees a consistent structure.
	return documentToMap(doc)
}

// documentToMap converts a resource.Document to map[string]any via JSON.
func documentToMap(doc resource.Document) (map[string]any, error) {
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("resource_read: marshal document: %w", err)
	}
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("resource_read: unmarshal document: %w", err)
	}
	return result, nil
}
