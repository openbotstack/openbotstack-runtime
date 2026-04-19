package webui_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/web/webui"
)

func TestUserHandlerIndex(t *testing.T) {
	handler := webui.UserHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestUserHandlerSPAFallback(t *testing.T) {
	handler := webui.UserHandler()
	req := httptest.NewRequest("GET", "/chat", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 for SPA fallback, got %d", rr.Code)
	}
}

func TestAdminHandlerIndex(t *testing.T) {
	handler := webui.AdminHandler()
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}
}

func TestAdminHandlerSPAFallback(t *testing.T) {
	handler := webui.AdminHandler()
	req := httptest.NewRequest("GET", "/tenants", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 for SPA fallback, got %d", rr.Code)
	}
}
