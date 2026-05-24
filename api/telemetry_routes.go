package api

import (
	"net/http"
)

func (r *Router) handleTelemetryHealth(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_configured"})
		return
	}
	r.telemetryHandler.handleHealth(w, req)
}

func (r *Router) handleTelemetrySpans(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	r.telemetryHandler.handleSpans(w, req)
}

func (r *Router) handleTelemetryEvents(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}
	r.telemetryHandler.handleEvents(w, req)
}

func (r *Router) handleTelemetryMetrics(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	r.telemetryHandler.handleMetrics(w, req)
}

func (r *Router) handleTelemetryFailures(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, map[string]int{})
		return
	}
	r.telemetryHandler.handleFailures(w, req)
}

func (r *Router) handleTelemetrySummary(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "")
		return
	}
	if r.telemetryHandler == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	r.telemetryHandler.handleSummary(w, req)
}
