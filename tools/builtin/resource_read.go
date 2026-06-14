package builtin

import (
	"context"
	"fmt"
	"log/slog"
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
func (t *ResourceReadTool) Description() string {
	return "Fetches a resource (URL) and returns a structured Document. " +
		"Return fields: content (extracted text, alias for text), text, title, source, content_type, layout, images, metadata, truncated, note. " +
		"Use {{builtin.resource_read.content}} or {{resource_read.text}} to access extracted text."
}
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

	startTime := time.Now()
	timeout := t.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	slog.Debug("resource_read: fetching", "url", rawURL, "timeout", timeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("resource_read: %w", err)
	}

	// Reuse the shared SSRF-safe HTTP transport (identical policy as web_fetch).
	client := newSSRFClient(t.allowPrivateIPs)
	resp, err := client.Do(req)
	if err != nil {
		slog.Warn("resource_read: fetch failed", "url", rawURL, "error", err, "duration_ms", time.Since(startTime).Milliseconds())
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
	fetchDuration := time.Since(startTime)

	slog.Info("resource_read: fetched",
		"url", rawURL,
		"status", resp.StatusCode,
		"content_type", contentType,
		"content_length", resp.ContentLength,
		"bytes_read", len(data),
		"truncated", truncated,
		"fetch_ms", fetchDuration.Milliseconds(),
	)

	doc := resource.ReadResource(rawURL, data, contentType)
	doc.Truncated = truncated

	slog.Info("resource_read: document ready",
		"url", rawURL,
		"type", doc.ContentType,
		"text_len", len(doc.Text),
		"layout", string(doc.Layout),
		"truncated", doc.Truncated,
		"has_images", len(doc.Images) > 0,
		"total_ms", time.Since(startTime).Milliseconds(),
	)
	if doc.Note != "" {
		slog.Warn("resource_read: document note", "url", rawURL, "note", doc.Note)
	}

	// Document.ToMap owns the wire shape (including the content/text alias).
	// No JSON round-trip — preserves []ImageRef and map[string]string intent.
	return doc.ToMap(), nil
}
