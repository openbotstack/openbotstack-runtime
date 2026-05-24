package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"

	coretelemetry "github.com/openbotstack/openbotstack-core/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
)

// ActiveExecutionTracker provides active execution counts for telemetry.
type ActiveExecutionTracker interface {
	ActiveExecutions() map[string]int
}

// TelemetryHandler serves telemetry data via admin API.
type TelemetryHandler struct {
	spanStore    *store.RingBufferSpanStore
	eventStore   *store.RingBufferEventStore
	meter        *coretelemetry.MemoryMeter
	activeTracker ActiveExecutionTracker
}

// NewTelemetryHandler creates a handler for telemetry endpoints.
func NewTelemetryHandler(spanStore *store.RingBufferSpanStore, eventStore *store.RingBufferEventStore, meter *coretelemetry.MemoryMeter, tracker ActiveExecutionTracker) *TelemetryHandler {
	return &TelemetryHandler{spanStore: spanStore, eventStore: eventStore, meter: meter, activeTracker: tracker}
}

func (h *TelemetryHandler) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"status": "healthy",
	}
	if h.meter != nil {
		snap := h.meter.Snapshot()
		resp["metrics_count"] = len(snap.Counters) + len(snap.Gauges) + len(snap.Histograms)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *TelemetryHandler) handleSpans(w http.ResponseWriter, r *http.Request) {
	if h.spanStore == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	var q store.SpanQuery
	if tid := r.URL.Query().Get("trace_id"); tid != "" {
		id := coretelemetry.TraceID(tid)
		q.TraceID = &id
	}
	if eid := r.URL.Query().Get("execution_id"); eid != "" {
		q.ExecutionID = &eid
	}
	q.Limit = 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 1000 {
			q.Limit = n
		}
	}

	spans, err := h.spanStore.QuerySpans(context.Background(), q)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query spans")
		return
	}
	if spans == nil {
		spans = []coretelemetry.Span{}
	}
	writeJSON(w, http.StatusOK, spans)
}

func (h *TelemetryHandler) handleEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventStore == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	var q store.EventQuery
	if tid := r.URL.Query().Get("trace_id"); tid != "" {
		id := coretelemetry.TraceID(tid)
		q.TraceID = &id
	}
	if comp := r.URL.Query().Get("component"); comp != "" {
		q.Component = stringPtr(comp)
	}
	if lvl := r.URL.Query().Get("level"); lvl != "" {
		l := coretelemetry.EventLevel(lvl)
		q.Level = &l
	}
	q.Limit = 100
	q.Offset = 0

	events, err := h.eventStore.QueryEvents(context.Background(), q)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query events")
		return
	}
	if events == nil {
		events = []coretelemetry.TelemetryEvent{}
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *TelemetryHandler) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	if h.meter == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{})
		return
	}
	writeJSON(w, http.StatusOK, h.meter.Snapshot())
}

func (h *TelemetryHandler) handleFailures(w http.ResponseWriter, r *http.Request) {
	if h.spanStore == nil {
		writeJSON(w, http.StatusOK, map[string]int{})
		return
	}

	q := store.SpanQuery{Limit: 1000}
	if eid := r.URL.Query().Get("execution_id"); eid != "" {
		q.ExecutionID = &eid
	}

	spans, err := h.spanStore.QuerySpans(context.Background(), q)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to query spans")
		return
	}
	grouped := make(map[string]int)
	for _, s := range spans {
		if s.Status != coretelemetry.SpanStatusOK {
			key := string(s.Kind) + "." + string(s.Status)
			grouped[key]++
		}
	}
	writeJSON(w, http.StatusOK, grouped)
}

func (h *TelemetryHandler) handleSummary(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{}

	// Active executions
	if h.activeTracker != nil {
		active := h.activeTracker.ActiveExecutions()
		resp["active_executions"] = len(active)
		resp["active_steps"] = active
	}

	// Compute rates and latency from spans
	if h.spanStore != nil {
		spans, _ := h.spanStore.QuerySpans(context.Background(), store.SpanQuery{Limit: 1000})
		total := len(spans)
		if total == 0 {
			resp["total_spans"] = 0
			resp["avg_step_latency_ms"] = 0
			resp["error_rate"] = 0
			resp["timeout_rate"] = 0
		} else {
			errorCount := 0
			timeoutCount := 0
			var totalDur float64
			timedCount := 0
			for _, s := range spans {
				switch s.Status {
				case coretelemetry.SpanStatusError:
					errorCount++
				case coretelemetry.SpanStatusTimeout:
					timeoutCount++
				}
				if d := s.Duration(); d > 0 {
					totalDur += float64(d.Milliseconds())
					timedCount++
				}
			}
			resp["total_spans"] = total
			resp["error_rate"] = float64(errorCount) / float64(total)
			resp["timeout_rate"] = float64(timeoutCount) / float64(total)
			if timedCount > 0 {
				resp["avg_step_latency_ms"] = totalDur / float64(timedCount)
			} else {
				resp["avg_step_latency_ms"] = 0
			}
		}
	}

	// Histogram percentiles
	if h.meter != nil {
		snap := h.meter.Snapshot()
		if entries, ok := snap.Histograms["step_duration_ms"]; ok {
			var vals []float64
			for _, e := range entries {
				vals = append(vals, e.Values...)
			}
			if len(vals) > 0 {
				sort.Float64s(vals)
				resp["percentiles"] = computePercentiles(vals, []float64{0.5, 0.9, 0.99})
			}
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

func computePercentiles(sorted []float64, ps []float64) map[string]float64 {
	result := make(map[string]float64)
	n := len(sorted)
	for _, p := range ps {
		idx := int(math.Ceil(float64(n)*p)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		key := "p" + strconv.Itoa(int(p*100))
		result[key] = sorted[idx]
	}
	return result
}

func stringPtr(s string) *string { return &s }

// Helper to ensure we satisfy writeJSON
var _ = json.Marshal
