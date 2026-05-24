package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// LineageNode represents a single node in the execution lineage graph.
type LineageNode struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"` // "execution", "step", "tool_call", "llm_request", "policy_check"
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	StartedAt  time.Time         `json:"started_at"`
	DurationMs int64             `json:"duration_ms"`
	ParentID   string            `json:"parent_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Payload    json.RawMessage   `json:"payload,omitempty"`
}

// LineageGraph represents the full lineage DAG for an execution.
type LineageGraph struct {
	ExecutionID string        `json:"execution_id"`
	TraceID     string        `json:"trace_id,omitempty"`
	TenantID    string        `json:"tenant_id"`
	Nodes       []LineageNode `json:"nodes"`
}

// LineageBuilder constructs a lineage graph from audit events.
type LineageBuilder struct {
	querier AuditQuerier
}

// NewLineageBuilder creates a lineage builder backed by an audit querier.
func NewLineageBuilder(querier AuditQuerier) *LineageBuilder {
	return &LineageBuilder{querier: querier}
}

// Build constructs the lineage graph for a given execution ID.
func (b *LineageBuilder) Build(ctx context.Context, executionID string) (*LineageGraph, error) {
	events, err := b.querier.Query(ctx, execution_logs.QueryFilter{
		RequestID: executionID,
		Limit:     500,
	})
	if err != nil {
		return nil, fmt.Errorf("lineage query failed: %w", err)
	}
	if len(events) == 0 {
		return nil, nil
	}

	graph := &LineageGraph{
		ExecutionID: executionID,
		Nodes:       make([]LineageNode, 0, len(events)),
	}

	for _, e := range events {
		if graph.TraceID == "" && e.TraceID != "" {
			graph.TraceID = e.TraceID
		}
		if graph.TenantID == "" && e.TenantID != "" {
			graph.TenantID = e.TenantID
		}

		node := LineageNode{
			ID:         e.ID,
			Type:       inferNodeType(e),
			Name:       stepOrAction(e),
			Status:     outcomeOrStatus(e),
			StartedAt:  e.Timestamp,
			DurationMs: e.Duration.Milliseconds(),
			Metadata:   e.Metadata,
		}

		if e.StepID != "" {
			node.ParentID = e.RequestID
		}

		if len(e.ToolInput) > 0 || e.ToolOutput != nil {
			payload := make(map[string]any)
			if len(e.ToolInput) > 0 {
				payload["tool_input"] = e.ToolInput
			}
			if e.ToolOutput != nil {
				payload["tool_output"] = e.ToolOutput
			}
			if raw, err := json.Marshal(payload); err == nil {
				node.Payload = raw
			}
		}

		graph.Nodes = append(graph.Nodes, node)
	}

	sort.Slice(graph.Nodes, func(i, j int) bool {
		return graph.Nodes[i].StartedAt.Before(graph.Nodes[j].StartedAt)
	})

	return graph, nil
}

func inferNodeType(e audit.AuditEvent) string {
	if e.Action == "policy.enforce" || e.Action == "policy.check" {
		return "policy_check"
	}
	if e.StepType == "tool" {
		return "tool_call"
	}
	if e.StepType == "llm" {
		return "llm_request"
	}
	if e.StepID != "" {
		return "step"
	}
	return "execution"
}

func stepOrAction(e audit.AuditEvent) string {
	if e.StepName != "" {
		return e.StepName
	}
	return e.Action
}

func outcomeOrStatus(e audit.AuditEvent) string {
	if e.Status != "" {
		return e.Status
	}
	return e.Outcome
}

// handleLineage returns the lineage graph for an execution.
// Registered at GET /v1/execution/{id}/lineage.
func handleLineage(b *LineageBuilder, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if b == nil || b.querier == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "lineage not configured")
		return
	}

	execID := r.PathValue("id")
	if execID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "execution id is required")
		return
	}

	graph, err := b.Build(r.Context(), execID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to build lineage")
		return
	}
	if graph == nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "execution not found")
		return
	}

	writeJSON(w, http.StatusOK, graph)
}
