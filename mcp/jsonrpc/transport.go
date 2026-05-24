package jsonrpc

import (
	"context"
	"encoding/json"
)

// Transport sends JSON-RPC requests and returns responses.
type Transport interface {
	// Send sends a JSON-RPC request and returns the raw response.
	Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error)
	// SendNotification sends a JSON-RPC notification (no response expected).
	SendNotification(request json.RawMessage) error
	// Close shuts down the transport.
	Close() error
}
