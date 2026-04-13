// Package observability provides OpenTelemetry setup for the OpenBotStack runtime.
//
// It initializes tracing and metrics providers. The Prometheus metrics exporter
// is always enabled for the /metrics endpoint. Tracing is only enabled when
// otel_enabled is set to true in configuration.
package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openbotstack/openbotstack-runtime/config"
)

var (
	mu             sync.RWMutex
	meterProvider  *sdkmetric.MeterProvider
	tracerProvider *sdktrace.TracerProvider
	promExporter   *prometheus.Exporter
)

// Setup initializes OpenTelemetry tracing and metrics.
// When otelEnabled is false, only the Prometheus metrics exporter is set up
// (tracing is a no-op). Returns a cleanup function for graceful shutdown.
func Setup(ctx context.Context, cfg config.ObservabilityConfig, version string) (func(), error) {
	// Create resource with service identity
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("openbotstack"),
			semconv.ServiceVersionKey.String(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTel resource: %w", err)
	}

	// Set up Prometheus exporter (always enabled for /metrics).
	promExp, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("create Prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExp),
	)
	otel.SetMeterProvider(mp)

	var tp *sdktrace.TracerProvider

	// Set up tracing only when enabled
	if cfg.OtelEnabled {
		// Use stdout trace exporter (OTLP exporter can be added later when endpoint is configured)
		stdoutExp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, fmt.Errorf("create stdout trace exporter: %w", err)
		}

		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(stdoutExp),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		)
		otel.SetTracerProvider(tp)

		// Set up W3C Trace Context propagator
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

		slog.Info("OpenTelemetry tracing enabled (stdout exporter)")
	}

	// Store in package-level vars under lock
	mu.Lock()
	promExporter = promExp
	meterProvider = mp
	tracerProvider = tp
	mu.Unlock()

	slog.Info("OpenTelemetry metrics initialized",
		"prometheus_enabled", true,
		"tracing_enabled", cfg.OtelEnabled)

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		mu.Lock()
		defer mu.Unlock()

		if tracerProvider != nil {
			if err := tracerProvider.Shutdown(shutdownCtx); err != nil {
				slog.Error("tracer shutdown error", "error", err)
			}
			tracerProvider = nil
		}
		if meterProvider != nil {
			if err := meterProvider.Shutdown(shutdownCtx); err != nil {
				slog.Error("meter shutdown error", "error", err)
			}
			meterProvider = nil
		}
		promExporter = nil
	}, nil
}

// PrometheusHandler returns an http.Handler that serves Prometheus-format metrics.
func PrometheusHandler() http.Handler {
	mu.RLock()
	exp := promExporter
	mu.RUnlock()

	if exp == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("# OTel not initialized\n"))
		})
	}
	return promhttp.Handler()
}
