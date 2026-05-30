package api

import (
	"net/http"

	"github.com/openbotstack/openbotstack-runtime/observability"
)

// MetricsHandler returns an http.Handler that serves Prometheus-format
// metrics via the OpenTelemetry SDK. Only GET is allowed.
func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
			return
		}
		observability.PrometheusHandler().ServeHTTP(w, r)
	})
}
