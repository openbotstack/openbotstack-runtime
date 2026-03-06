// Package wasm provides HTTP sandboxing for Wasm skills.
package wasm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrURLNotAllowed is returned when a URL is not in the allowlist.
	ErrURLNotAllowed = errors.New("wasm: URL not in allowlist")
)

// HTTPAllowlist validates URLs against an allowed domains list.
type HTTPAllowlist struct {
	patterns []string
}

// NewHTTPAllowlist creates a new allowlist from patterns.
// Patterns can be:
//   - Exact domain: "https://api.example.com"
//   - Wildcard subdomain: "*.example.com"
//   - Allow all: "*"
func NewHTTPAllowlist(patterns []string) *HTTPAllowlist {
	return &HTTPAllowlist{patterns: patterns}
}

// IsAllowed checks if a URL is permitted by the allowlist.
func (a *HTTPAllowlist) IsAllowed(rawURL string) bool {
	if len(a.patterns) == 0 {
		return false
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	for _, pattern := range a.patterns {
		if pattern == "*" {
			return true
		}

		// Wildcard subdomain pattern: *.example.com
		if strings.HasPrefix(pattern, "*.") {
			domain := strings.TrimPrefix(pattern, "*.")
			// Must have subdomain (host contains a dot before domain)
			if strings.HasSuffix(parsed.Host, "."+domain) {
				return true
			}
			continue
		}

		// Exact domain match (with path prefix)
		patternURL, err := url.Parse(pattern)
		if err != nil {
			continue
		}

		// Check scheme and host match
		if patternURL.Scheme != "" && patternURL.Scheme != parsed.Scheme {
			continue
		}

		// Prevent subdomain bypass (api.example.com.evil.com)
		if parsed.Host == patternURL.Host {
			return true
		}

		// Check if path starts with pattern path
		if parsed.Host == patternURL.Host && strings.HasPrefix(parsed.Path, patternURL.Path) {
			return true
		}
	}

	return false
}

// SandboxedHTTPClient performs HTTP requests with allowlist validation.
type SandboxedHTTPClient struct {
	allowlist *HTTPAllowlist
	client    *http.Client
}

// NewSandboxedHTTPClient creates a new sandboxed HTTP client.
// If httpClient is nil, uses default with 30s timeout.
func NewSandboxedHTTPClient(allowlist *HTTPAllowlist, httpClient *http.Client) *SandboxedHTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &SandboxedHTTPClient{
		allowlist: allowlist,
		client:    httpClient,
	}
}

// Fetch performs an HTTP request if the URL is allowed.
func (c *SandboxedHTTPClient) Fetch(ctx context.Context, urlStr, method string, body []byte) ([]byte, int, error) {
	// Validate URL is in allowlist
	if !c.allowlist.IsAllowed(urlStr) {
		return nil, 0, ErrURLNotAllowed
	}

	// Default to GET
	if method == "" {
		method = "GET"
	}

	// Create request
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bodyReader)
	if err != nil {
		return nil, 0, err
	}

	// Set content-type for POST/PUT
	if len(body) > 0 && (method == "POST" || method == "PUT" || method == "PATCH") {
		req.Header.Set("Content-Type", "application/json")
	}

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}
