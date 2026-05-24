package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
}

// SSEStream buffers events for streaming.
type SSEStream struct {
	mu     sync.Mutex
	events []SSEEvent
	closed bool
}

// NewSSEStream creates a new SSE stream.
func NewSSEStream() *SSEStream {
	return &SSEStream{
		events: make([]SSEEvent, 0),
	}
}

// Send adds an event to the stream.
func (s *SSEStream) Send(event SSEEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("stream closed")
	}

	s.events = append(s.events, event)
	return nil
}

// Count returns the number of events.
func (s *SSEStream) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

// Close marks the stream as closed.
func (s *SSEStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// SSEHandler writes SSE events to an HTTP response.
type SSEHandler struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEHandler creates a new SSE handler and immediately sends the response
// headers plus an SSE comment to force the TCP connection to flush.
// This ensures the client's ReadableStream is ready to receive events
// before any server-side processing begins.
func NewSSEHandler(w http.ResponseWriter) *SSEHandler {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Write response headers
	w.WriteHeader(http.StatusOK)

	// Send an SSE comment to force the HTTP response to flush to the network.
	// Go's net/http may buffer the headers until the first Write call.
	// SSE comments (lines starting with ':') are ignored by clients per spec.
	_, _ = fmt.Fprint(w, ":ok\n\n")

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	return &SSEHandler{
		w:       w,
		flusher: flusher,
	}
}

// WriteEvent writes a single SSE event.
// Multi-line data is split into multiple "data:" lines per the SSE specification.
func (h *SSEHandler) WriteEvent(event SSEEvent) error {
	if event.ID != "" {
		_, _ = fmt.Fprintf(h.w, "id: %s\n", event.ID)
	}
	if event.Event != "" {
		_, _ = fmt.Fprintf(h.w, "event: %s\n", event.Event)
	}
	for _, line := range strings.Split(event.Data, "\n") {
		_, _ = fmt.Fprintf(h.w, "data: %s\n", line)
	}
	_, _ = fmt.Fprintln(h.w)

	if h.flusher != nil {
		h.flusher.Flush()
	}

	return nil
}
