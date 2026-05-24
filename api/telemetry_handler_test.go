package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	coretelemetry "github.com/openbotstack/openbotstack-core/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
)

func TestTelemetryHealthHandler(t *testing.T) {
	meter := coretelemetry.NewMemoryMeter()
	meter.Counter("execution_completed_total", 42, coretelemetry.Labels{})
	meter.Gauge("active_executions", 3.0, coretelemetry.Labels{})

	handler := &TelemetryHandler{meter: meter}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/telemetry/health", nil)
	w := httptest.NewRecorder()
	handler.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "healthy" {
		t.Fatalf("status = %v, want healthy", resp["status"])
	}
}

func TestTelemetrySpansHandler(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)

	handler := &TelemetryHandler{spanStore: spanStore}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/telemetry/spans", nil)
	w := httptest.NewRecorder()
	handler.handleSpans(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTelemetryMetricsHandler(t *testing.T) {
	meter := coretelemetry.NewMemoryMeter()
	meter.Counter("test_counter", 10, coretelemetry.Labels{"status": "ok"})

	handler := &TelemetryHandler{meter: meter}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/telemetry/metrics", nil)
	w := httptest.NewRecorder()
	handler.handleMetrics(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

func TestTelemetryEventsHandler(t *testing.T) {
	eventStore := store.NewRingBufferEventStore(100)

	handler := &TelemetryHandler{eventStore: eventStore}

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/telemetry/events", nil)
	w := httptest.NewRecorder()
	handler.handleEvents(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestTelemetryHTTPMethodEnforcement(t *testing.T) {
	r := &Router{telemetryHandler: &TelemetryHandler{}}
	routes := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"health", r.handleTelemetryHealth},
		{"spans", r.handleTelemetrySpans},
		{"events", r.handleTelemetryEvents},
		{"metrics", r.handleTelemetryMetrics},
		{"failures", r.handleTelemetryFailures},
	}

	for _, tt := range routes {
		t.Run(tt.name+" rejects POST", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/telemetry/"+tt.name, nil)
			w := httptest.NewRecorder()
			tt.handler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("POST %s: got %d, want %d", tt.name, w.Code, http.StatusMethodNotAllowed)
			}
		})
		t.Run(tt.name+" accepts GET", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/admin/telemetry/"+tt.name, nil)
			w := httptest.NewRecorder()
			tt.handler(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("GET %s: got %d, want %d", tt.name, w.Code, http.StatusOK)
			}
		})
	}
}
