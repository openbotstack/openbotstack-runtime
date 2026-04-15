// Package wasm provides HTTP sandboxing for Wasm skills.
package wasm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxHTTPResponseBody = 1 << 20 // 1MB max response body for Wasm skills

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
	allowlist       *HTTPAllowlist
	client          *http.Client
	blockPrivateIPs bool // when true, blocks private/loopback/link-local addresses
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
		allowlist:       allowlist,
		client:          httpClient,
		blockPrivateIPs: false, // opt-in for backward compatibility
	}
}

// NewSandboxedHTTPClientWithSSRF creates a sandboxed HTTP client with SSRF protection.
// Blocks private/loopback/link-local IP addresses at the DNS resolution level
// using a custom DialContext, preventing DNS rebinding attacks.
func NewSandboxedHTTPClientWithSSRF(allowlist *HTTPAllowlist, httpClient *http.Client) *SandboxedHTTPClient {
	// Use a custom Transport that validates resolved IPs at connection time
	// to prevent DNS rebinding (TOCTOU) attacks.
	ssrfTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, ErrURLNotAllowed
			}
			// Resolve and check each IP at connection time
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if isBlockedIP(ip.IP) {
					return nil, ErrURLNotAllowed
				}
			}
				if len(ips) == 0 {
					return nil, ErrURLNotAllowed
				}

			// Dial the resolved IP directly to prevent a second DNS lookup
			// that could be poisoned (DNS rebinding TOCTOU).
			d := &net.Dialer{Timeout: 30 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout:   30 * time.Second,
			Transport: ssrfTransport,
		}
	} else {
		httpClient.Transport = ssrfTransport
	}

	return &SandboxedHTTPClient{
		allowlist:       allowlist,
		client:          httpClient,
		blockPrivateIPs: true,
	}
}

// isBlockedIP returns true if the IP should be blocked for SSRF protection.
func isBlockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() ||
		ip.Equal(net.ParseIP("169.254.169.254"))
}

// Fetch performs an HTTP request if the URL is allowed.
func (c *SandboxedHTTPClient) Fetch(ctx context.Context, urlStr, method string, body []byte) ([]byte, int, error) {
	// Validate URL is in allowlist
	if !c.allowlist.IsAllowed(urlStr) {
		return nil, 0, ErrURLNotAllowed
	}

	// Block private/loopback/link-local addresses to prevent SSRF (opt-in)
	if c.blockPrivateIPs && isPrivateOrReserved(urlStr) {
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
	defer resp.Body.Close() //nolint:errcheck // closed after io.ReadAll

	// Read response body with size limit to prevent unbounded memory growth
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPResponseBody))
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return respBody, resp.StatusCode, nil
}

// isPrivateOrReserved checks if a URL resolves to a private, loopback, or
// link-local IP address. This prevents SSRF attacks from Wasm skills.
func isPrivateOrReserved(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true // fail closed
	}

	host := parsed.Hostname()
	if host == "" {
		return true
	}

	// Check for obvious localhost variants
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	// Resolve the host and check the IP
	ips, err := net.LookupIP(host)
	if err != nil {
		// If resolution fails, fail closed for non-IP hosts
		// Allow direct IP addresses to be checked
		ip := net.ParseIP(host)
		if ip == nil {
			return false // can't resolve, allow (DNS will fail at request time)
		}
		ips = []net.IP{ip}
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return true
		}
	}

	return false
}
