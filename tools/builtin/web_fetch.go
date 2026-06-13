package builtin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type WebFetchTool struct {
	Timeout  time.Duration
	MaxBytes int64
	// allowPrivateIPs disables SSRF protection for private network addresses.
	// Intended for testing with httptest servers that bind to 127.0.0.1.
	// Must NOT be set to true in production.
	allowPrivateIPs bool
}

func (t *WebFetchTool) Name() string        { return "web_fetch" }
func (t *WebFetchTool) Description() string { return "Performs an HTTP request and returns the response body." }
func (t *WebFetchTool) Parameters() map[string]string {
	return map[string]string{"method": "string", "url": "string", "headers": "object", "body": "string"}
}
func (t *WebFetchTool) Required() []string    { return []string{"url"} }
func (t *WebFetchTool) Permissions() []string { return []string{"http.fetch"} }


func (t *WebFetchTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	url, _ := input["url"].(string)
	if url == "" {
		return nil, fmt.Errorf("web_fetch: url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("web_fetch: url must start with http:// or https://")
	}
	method := "GET"
	if m, ok := input["method"].(string); ok && m != "" {
		method = strings.ToUpper(m)
	}
	allowedMethods := map[string]bool{"GET": true, "POST": true, "HEAD": true}
	if !allowedMethods[method] {
		return nil, fmt.Errorf("web_fetch: method %q not allowed (use GET, POST, or HEAD)", method)
	}
	timeout := t.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if b, ok := input["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: %w", err)
	}
	if headers, ok := input["headers"].(map[string]any); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}
	// SSRF protection + bounded redirects via the shared client (the same
	// transport web_fetch has always used, now factored out so resource_read
	// inherits identical policy).
	client := newSSRFClient(t.allowPrivateIPs)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	maxBytes := t.MaxBytes
	if maxBytes == 0 {
		maxBytes = 1024 * 1024
	}
	data, _, err := readLimited(resp.Body, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: read body: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("web_fetch: response too large (max %d bytes)", maxBytes)
	}
	return map[string]any{
		"status_code":  int64(resp.StatusCode),
		"body":         string(data),
		"content_type": resp.Header.Get("Content-Type"),
	}, nil
}
