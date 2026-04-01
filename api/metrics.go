package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

// Metrics holds simple counters for Prometheus-style exposition.
// This is a lightweight stub — a full implementation would use
// prometheus/client_golang, but keeping zero external dependencies
// for the initial stub.
type Metrics struct {
	requestsTotal   atomic.Int64
	requestsErrored atomic.Int64
}

// NewMetrics returns a new Metrics instance.
func NewMetrics() *Metrics {
	return &Metrics{}
}

// IncRequests increments the total request counter.
func (m *Metrics) IncRequests() {
	m.requestsTotal.Add(1)
}

// IncErrors increments the error counter.
func (m *Metrics) IncErrors() {
	m.requestsErrored.Add(1)
}

// Handler returns an http.HandlerFunc that exposes metrics in
// Prometheus text exposition format.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		fmt.Fprintf(w, "# HELP openbotstack_requests_total Total number of HTTP requests received.\n")
		fmt.Fprintf(w, "# TYPE openbotstack_requests_total counter\n")
		fmt.Fprintf(w, "openbotstack_requests_total %d\n", m.requestsTotal.Load())
		fmt.Fprintf(w, "# HELP openbotstack_requests_errored_total Total number of HTTP requests that resulted in errors.\n")
		fmt.Fprintf(w, "# TYPE openbotstack_requests_errored_total counter\n")
		fmt.Fprintf(w, "openbotstack_requests_errored_total %d\n", m.requestsErrored.Load())
	}
}
