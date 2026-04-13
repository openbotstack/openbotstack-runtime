package observability

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "openbotstack"

var (
	httpRequestsTotal  metric.Int64Counter
	httpRequestDuration metric.Float64Histogram
)

// InitMetrics creates OTel metric instruments. Must be called after Setup().
func InitMetrics() error {
	meter := otel.GetMeterProvider().Meter(meterName)

	var err error
	httpRequestsTotal, err = meter.Int64Counter("http.server.requests",
		metric.WithDescription("Total number of HTTP requests"),
	)
	if err != nil {
		return fmt.Errorf("create http_requests counter: %w", err)
	}

	httpRequestDuration, err = meter.Float64Histogram("http.server.duration",
		metric.WithDescription("HTTP request duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return fmt.Errorf("create http_duration histogram: %w", err)
	}

	return nil
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// MetricsMiddleware records HTTP request metrics for all requests.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := newResponseWriter(w)

		next.ServeHTTP(ww, r)

		duration := float64(time.Since(start).Milliseconds())
		attrs := []attribute.KeyValue{
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
			attribute.Int("http.status_code", ww.statusCode),
		}

		if httpRequestsTotal != nil {
			httpRequestsTotal.Add(r.Context(), 1, metric.WithAttributes(attrs...))
		}
		if httpRequestDuration != nil {
			httpRequestDuration.Record(r.Context(), duration, metric.WithAttributes(attrs...))
		}
	})
}
