package webui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/web/webui"
)

func TestUIHandlerIndex(t *testing.T) {
	handler := webui.Handler()

	req := httptest.NewRequest("GET", "/ui/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestUIHandlerAsset(t *testing.T) {
	handler := webui.Handler()

	req := httptest.NewRequest("GET", "/ui/assets/index.js", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should return 200 for existing assets or 404 for non-existent
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Errorf("Expected 200 or 404, got %d", rr.Code)
	}
}

func TestUIHandlerSPAFallback(t *testing.T) {
	handler := webui.Handler()

	// Request for a route that should fallback to index.html
	req := httptest.NewRequest("GET", "/ui/chat", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 for SPA fallback, got %d", rr.Code)
	}
}
