package jsonrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSETransport communicates with an MCP server via HTTP POST + SSE.
type SSETransport struct {
	endpoint string
	client   *http.Client
	headers  map[string]string
}

// NewSSETransport creates an HTTP/SSE transport for the given endpoint.
func NewSSETransport(endpoint string, headers map[string]string) *SSETransport {
	return &SSETransport{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		headers: headers,
	}
}

// Send posts a JSON-RPC request and returns the response.
func (t *SSETransport) Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(request))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Limit response body to 10MB to prevent memory exhaustion
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("HTTP %d from MCP server: %s", resp.StatusCode, bodyStr)
	}

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return parseSSEEvent(body)
	}

	return json.RawMessage(body), nil
}

// SendNotification sends a notification via HTTP POST without reading a response.
func (t *SSETransport) SendNotification(request json.RawMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(request))
	if err != nil {
		return fmt.Errorf("create notification request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
	}
	_ = resp.Body.Close()
	return nil
}

// Close shuts down idle connections.
func (t *SSETransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

func parseSSEEvent(data []byte) (json.RawMessage, error) {
	lines := strings.Split(string(data), "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimPrefix(line, "data:")
			payload = strings.TrimPrefix(payload, " ")
			if payload != "" {
				parts = append(parts, payload)
			}
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no data found in SSE response")
	}
	return json.RawMessage(strings.Join(parts, "\n")), nil
}
