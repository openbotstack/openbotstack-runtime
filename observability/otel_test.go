package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/openbotstack/openbotstack-runtime/config"
)

func TestSetupMetricsOnly(t *testing.T) {
	cfg := config.ObservabilityConfig{
		LogLevel:    "info",
		OtelEnabled: false,
	}
	cleanup, err := Setup(context.Background(), cfg, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	handler := PrometheusHandler()
	if handler == nil {
		t.Fatal("PrometheusHandler returned nil")
	}

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "target_info") {
		t.Errorf("expected target_info in Prometheus output, got: %s", body)
	}
}

func TestSetupWithTracing(t *testing.T) {
	cfg := config.ObservabilityConfig{
		LogLevel:    "info",
		OtelEnabled: true,
	}
	cleanup, err := Setup(context.Background(), cfg, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	// Verify the global tracer provider is actually our sdktrace.TracerProvider
	tp := otel.GetTracerProvider()
	if _, ok := tp.(*sdktrace.TracerProvider); !ok {
		t.Errorf("expected *sdktrace.TracerProvider, got %T", tp)
	}
}

func TestPrometheusHandlerBeforeSetup(t *testing.T) {
	// Save and restore state
	mu.Lock()
	prev := promExporter
	promExporter = nil
	mu.Unlock()
	defer func() {
		mu.Lock()
		promExporter = prev
		mu.Unlock()
	}()

	handler := PrometheusHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "OTel not initialized") {
		t.Errorf("expected 'OTel not initialized' message, got: %s", body)
	}
}

func TestCleanup(t *testing.T) {
	cfg := config.ObservabilityConfig{
		LogLevel:    "info",
		OtelEnabled: true,
	}
	cleanup, err := Setup(context.Background(), cfg, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// Cleanup should not panic
	cleanup()

	// Verify state is nil'd after cleanup
	mu.RLock()
	mp := meterProvider
	tp := tracerProvider
	exp := promExporter
	mu.RUnlock()

	if mp != nil {
		t.Error("meterProvider should be nil after cleanup")
	}
	if tp != nil {
		t.Error("tracerProvider should be nil after cleanup")
	}
	if exp != nil {
		t.Error("promExporter should be nil after cleanup")
	}
}
