package api

import (
	"net/http"

	"github.com/openbotstack/openbotstack-runtime/observability"
)

// MetricsHandler returns an http.Handler that serves Prometheus-format
// metrics via the OpenTelemetry SDK.
func MetricsHandler() http.Handler {
	return observability.PrometheusHandler()
}
