package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/config"
)

func TestInitMetrics(t *testing.T) {
	cleanup, err := Setup(context.Background(), config.ObservabilityConfig{}, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	if err := InitMetrics(); err != nil {
		t.Fatalf("InitMetrics: %v", err)
	}
}

func TestMetricsMiddleware(t *testing.T) {
	cleanup, err := Setup(context.Background(), config.ObservabilityConfig{}, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	if err := InitMetrics(); err != nil {
		t.Fatalf("InitMetrics: %v", err)
	}

	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	// Verify metrics were recorded by checking the Prometheus handler output
	promHandler := PrometheusHandler()
	promReq := httptest.NewRequest("GET", "/metrics", nil)
	promRec := httptest.NewRecorder()
	promHandler.ServeHTTP(promRec, promReq)

	body := promRec.Body.String()
	if !strings.Contains(body, "http_server_requests") {
		t.Errorf("expected http_server_requests metric, got: %s", body)
	}
	if !strings.Contains(body, "http_server_duration") {
		t.Errorf("expected http_server_duration metric, got: %s", body)
	}
}

func TestMetricsMiddlewareCapturesStatusCodes(t *testing.T) {
	cleanup, err := Setup(context.Background(), config.ObservabilityConfig{}, "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer cleanup()

	if err := InitMetrics(); err != nil {
		t.Fatalf("InitMetrics: %v", err)
	}

	handler := MetricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest("GET", "/v1/skills", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}
