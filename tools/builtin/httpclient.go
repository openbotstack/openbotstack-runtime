package builtin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
)

// maxRedirects caps HTTP redirect chains to prevent loops. The Go default is
// 10; we tighten to 5 which covers legitimate CDN/signature-URL chains.
const maxRedirects = 5

// isPrivateIP reports whether the given IP address belongs to a private,
// loopback, or link-local network range. Used for SSRF protection by both
// web_fetch and resource_read via ssrfTransport.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// ssrfTransport returns an *http.Transport whose DialContext rejects private,
// loopback, and link-local destination IPs (SSRF protection via connection-time
// resolution, which also defeats DNS rebinding). When allowPrivateIPs is true
// the check is skipped (development/test opt-in).
//
// Shared by web_fetch and resource_read so both inherit identical SSRF policy.
func ssrfTransport(allowPrivateIPs bool) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address: %w", err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("DNS resolution failed: %w", err)
			}
			for _, ip := range ips {
				if !allowPrivateIPs && isPrivateIP(ip.IP) {
					return nil, fmt.Errorf("access to private network addresses is blocked (%s)", ip.IP)
				}
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
}

// newSSRFClient returns an *http.Client using ssrfTransport with a bounded
// redirect chain. Timeout is applied per-request by callers via context.
func newSSRFClient(allowPrivateIPs bool) *http.Client {
	return &http.Client{
		Transport: ssrfTransport(allowPrivateIPs),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects (>%d)", maxRedirects)
			}
			return nil
		},
	}
}

// readLimited reads at most maxBytes+1 bytes from r and reports whether the
// limit was exceeded (so callers can flag truncation / reject oversize bodies).
func readLimited(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	if maxBytes <= 0 {
		data, err := io.ReadAll(r)
		return data, false, err
	}
	limited := io.LimitReader(r, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) > maxBytes, nil
}
