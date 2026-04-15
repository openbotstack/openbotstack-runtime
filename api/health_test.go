package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockHealthChecker is a test HealthChecker.
type mockHealthChecker struct {
	name   string
	status string
	err    string
	delay  time.Duration
}

func (m *mockHealthChecker) Name() string { return m.name }

func (m *mockHealthChecker) Check(ctx context.Context) ComponentHealth {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ComponentHealth{Status: "unhealthy", Error: "timeout"}
		}
	}
	return ComponentHealth{
		Status: m.status,
		Error:  m.err,
	}
}

func TestHealthzEndpoint(t *testing.T) {
	router := NewRouter(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "healthy" {
		t.Errorf("Expected 'healthy', got '%s'", body["status"])
	}
}

func TestReadyzAllHealthy(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{Version: "1.0", Commit: "abc", Branch: "main", BuildTime: "2026-01-01"})
	router.SetHealthCheckers(
		&mockHealthChecker{name: "redis", status: "healthy"},
		&mockHealthChecker{name: "llm", status: "healthy"},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	var report HealthReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.Status != "healthy" {
		t.Errorf("Expected 'healthy', got '%s'", report.Status)
	}
	if len(report.Components) != 2 {
		t.Errorf("Expected 2 components, got %d", len(report.Components))
	}
}

func TestReadyzUnhealthyComponent(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{Version: "1.0"})
	router.SetHealthCheckers(
		&mockHealthChecker{name: "redis", status: "healthy"},
		&mockHealthChecker{name: "llm", status: "unhealthy", err: "connection refused"},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
	var report HealthReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.Status != "unhealthy" {
		t.Errorf("Expected 'unhealthy', got '%s'", report.Status)
	}
}

func TestReadyzNoCheckers(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{Version: "dev"})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with no checkers, got %d", w.Code)
	}
}

func TestVersionEndpoint(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{
		Version:   "1.0.0",
		Commit:    "abc123",
		Branch:    "main",
		BuildTime: "2026-04-11T12:00:00Z",
	})

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	var info BuildInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got '%s'", info.Version)
	}
	if info.Commit != "abc123" {
		t.Errorf("Expected commit 'abc123', got '%s'", info.Commit)
	}
}

func TestVersionEndpointDevDefaults(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{})

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", w.Code)
	}
	var info BuildInfo
	if err := json.NewDecoder(w.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Version != "" {
		t.Errorf("Expected empty version for dev, got '%s'", info.Version)
	}
}

func TestReadyzCheckerTimeout(t *testing.T) {
	router := NewRouter(nil)
	router.SetBuildInfo(BuildInfo{Version: "1.0"})
	router.SetHealthCheckers(
		&mockHealthChecker{name: "slow", status: "healthy", delay: 10 * time.Second},
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 for timeout, got %d", w.Code)
	}
}
