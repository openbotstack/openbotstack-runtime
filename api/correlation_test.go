package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCorrelationMiddleware_GeneratesIDWhenMissing(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := CorrelationIDFromContext(r.Context())
		if correlationID == "" {
			t.Error("expected correlation ID in context, got empty string")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := CorrelationMiddleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	responseID := rec.Header().Get(CorrelationIDHeader)
	if responseID == "" {
		t.Error("expected X-Correlation-ID in response headers")
	}
}

func TestCorrelationMiddleware_ReusesIncomingID(t *testing.T) {
	const expectedID = "test-correlation-id-123"

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := CorrelationIDFromContext(r.Context())
		if correlationID != expectedID {
			t.Errorf("expected %q, got %q", expectedID, correlationID)
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := CorrelationMiddleware(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(CorrelationIDHeader, expectedID)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	responseID := rec.Header().Get(CorrelationIDHeader)
	if responseID != expectedID {
		t.Errorf("response header: expected %q, got %q", expectedID, responseID)
	}
}

func TestCorrelationIDFromContext_EmptyWhenNotSet(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	id := CorrelationIDFromContext(req.Context())
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}
