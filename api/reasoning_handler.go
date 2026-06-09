package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/harness"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// ReasoningResponse is the API response for the reasoning endpoint.
type ReasoningResponse struct {
	ExecutionID   string                     `json:"execution_id"`
	PlanID        string                     `json:"plan_id,omitempty"`
	ReplanCount   int                        `json:"replan_count,omitempty"`
	PlanIDs       []string                   `json:"plan_ids,omitempty"`
	Tree          *reasoning.ReasoningEvent  `json:"tree"`
	Text          string                     `json:"text"`
	Metrics       *TraceMetricsJSON          `json:"metrics,omitempty"`
	StopCondition *StopConditionJSON          `json:"stop_condition,omitempty"`
	Debug         *ReasoningDebug            `json:"debug,omitempty"`
}

// TraceMetricsJSON holds execution metrics for the API response.
type TraceMetricsJSON struct {
	TotalSteps     int `json:"total_steps"`
	TotalToolCalls int `json:"total_tool_calls"`
	TotalLLMTurns  int `json:"total_llm_turns"`
	TotalRuntimeMs int `json:"total_runtime_ms"`
}

// StopConditionJSON holds stop condition info for the API response.
type StopConditionJSON struct {
	Stopped bool   `json:"stopped"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
}

// ReasoningDebug contains extra information when ?debug=true is requested.
type ReasoningDebug struct {
	AuditTrail []AuditEntryJSON `json:"audit_trail"`
}

// AuditEntryJSON is the JSON representation of an audit entry for the debug response.
type AuditEntryJSON struct {
	TraceID    string `json:"trace_id"`
	StepID     string `json:"step_id"`
	StepName   string `json:"step_name"`
	StepType   string `json:"step_type"`
	Timestamp  string `json:"timestamp"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMs int    `json:"duration_ms"`
}

// handleExecutionAction dispatches /v1/execution/{id}/* sub-paths.
func (r *Router) handleExecutionAction(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	switch {
	case strings.HasSuffix(path, "/reasoning"):
		r.handleExecutionReasoning(w, req)
	case strings.HasSuffix(path, "/lineage"):
		handleLineage(r.lineageBuilder, w, req)
	default:
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "not found")
	}
}

// handleExecutionReasoning handles GET /v1/execution/{id}/reasoning
func (r *Router) handleExecutionReasoning(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method, "path", req.URL.Path,
			"status", http.StatusMethodNotAllowed, "error", "method not allowed")
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	executionID := extractExecutionID(req.URL.Path)
	if executionID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "missing execution ID")
		return
	}

	if r.reasoningStore == nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "reasoning not available")
		return
	}

	user, hasUser := middleware.UserFromContext(req.Context())

	// Try enhanced trace path first.
	traceRaw, _ := r.reasoningStore.GetTraceData(req.Context(), executionID)
	if trace, ok := traceRaw.(*harness.ExecutionTraceData); ok && trace != nil {
		// Tenant isolation: verify trace belongs to requesting user's tenant.
		if hasUser && user.TenantID != "" && trace.TenantID != "" && trace.TenantID != user.TenantID {
			writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
			return
		}

		tree := harness.BuildExecutionTree(trace)
		text := reasoning.RenderReasoningText(tree)
		resp := ReasoningResponse{
			ExecutionID: executionID,
			PlanID:      trace.PlanID,
			ReplanCount: trace.ReplanCount,
			PlanIDs:     trace.PlanIDs,
			Tree:        tree,
			Text:        text,
			Metrics: &TraceMetricsJSON{
				TotalSteps:     trace.Metrics.TotalSteps,
				TotalToolCalls: trace.Metrics.TotalToolCalls,
				TotalLLMTurns:  trace.Metrics.TotalLLMTurns,
				TotalRuntimeMs: trace.Metrics.TotalRuntimeMs,
			},
			StopCondition: &StopConditionJSON{
				Stopped: trace.StopReason != "",
				Reason:  trace.StopReason,
				Detail:  trace.StopDetail,
			},
		}

		if req.URL.Query().Get("debug") == "true" {
			resp.Debug = r.buildDebugResponse(req.Context(), executionID)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// Fallback: build tree from audit trail (backward compatible).
	trail, err := r.reasoningStore.GetAuditTrail(req.Context(), executionID)
	if err != nil {
		slog.ErrorContext(req.Context(), "handler error",
			"method", req.Method, "path", req.URL.Path,
			"status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to retrieve reasoning")
		return
	}

	if trail == nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
		return
	}

	// Verify tenant ownership.
	if hasUser && user.TenantID != "" {
		if len(trail) > 0 && trail[0].TenantID != "" && trail[0].TenantID != user.TenantID {
			slog.WarnContext(req.Context(), "reasoning access denied: tenant mismatch",
				"method", req.Method, "path", req.URL.Path,
				"user_tenant", user.TenantID, "trail_tenant", trail[0].TenantID)
			writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
			return
		}
	}

	tree := reasoning.BuildReasoningTree(trail)
	text := reasoning.RenderReasoningText(tree)

	resp := ReasoningResponse{
		ExecutionID: executionID,
		Tree:        tree,
		Text:        text,
	}

	if req.URL.Query().Get("debug") == "true" {
		resp.Debug = buildDebugEntries(trail)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// buildDebugResponse fetches audit trail and builds debug response.
func (r *Router) buildDebugResponse(_ context.Context, executionID string) *ReasoningDebug {
	trail, _ := r.reasoningStore.GetAuditTrail(context.Background(), executionID)
	if trail == nil {
		return nil
	}
	return buildDebugEntries(trail)
}

// buildDebugEntries converts audit events to debug response.
func buildDebugEntries(trail []audit.AuditEvent) *ReasoningDebug {
	debugEntries := make([]AuditEntryJSON, len(trail))
	for i, e := range trail {
		debugEntries[i] = AuditEntryJSON{
			TraceID:    e.TraceID,
			StepID:     e.StepID,
			StepName:   e.StepName,
			StepType:   e.StepType,
			Timestamp:  e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
			Status:     e.Status,
			Error:      e.Error,
			DurationMs: int(e.Duration.Milliseconds()),
		}
	}
	return &ReasoningDebug{AuditTrail: debugEntries}
}

// extractExecutionID parses the execution ID from /v1/execution/{id}/reasoning
func extractExecutionID(path string) string {
	prefix := "/v1/execution/"
	if len(path) <= len(prefix) {
		return ""
	}
	trimmed := path[len(prefix):]
	if idx := indexOfSlash(trimmed); idx > 0 {
		return trimmed[:idx]
	}
	return ""
}

func indexOfSlash(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
