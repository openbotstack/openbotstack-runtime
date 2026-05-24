package api

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

type mockLineageQuerier struct {
	events []audit.AuditEvent
	err    error
}

func (m *mockLineageQuerier) Query(_ context.Context, _ execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	return m.events, m.err
}

func (m *mockLineageQuerier) Count(_ context.Context, _ execution_logs.QueryFilter) (int, error) {
	return len(m.events), nil
}

func TestLineageBuilder_EmptyResult(t *testing.T) {
	b := NewLineageBuilder(&mockLineageQuerier{events: nil})
	graph, err := b.Build(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if graph != nil {
		t.Error("graph should be nil for nonexistent execution")
	}
}

func TestLineageBuilder_SingleExecution(t *testing.T) {
	now := time.Now()
	b := NewLineageBuilder(&mockLineageQuerier{
		events: []audit.AuditEvent{
			{
				ID:        "evt-1",
				TenantID:  "t1",
				RequestID: "exec-1",
				Action:    "skills.execute",
				Resource:  "skill/summarize",
				Outcome:   "success",
				Duration:  100 * time.Millisecond,
				Timestamp: now,
			},
		},
	})

	graph, err := b.Build(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if graph == nil {
		t.Fatal("graph should not be nil")
	}
	if graph.ExecutionID != "exec-1" {
		t.Errorf("ExecutionID = %q, want %q", graph.ExecutionID, "exec-1")
	}
	if graph.TenantID != "t1" {
		t.Errorf("TenantID = %q, want %q", graph.TenantID, "t1")
	}
	if len(graph.Nodes) != 1 {
		t.Fatalf("Nodes = %d, want 1", len(graph.Nodes))
	}
	if graph.Nodes[0].Type != "execution" {
		t.Errorf("Type = %q, want %q", graph.Nodes[0].Type, "execution")
	}
}

func TestLineageBuilder_WithSteps(t *testing.T) {
	now := time.Now()
	b := NewLineageBuilder(&mockLineageQuerier{
		events: []audit.AuditEvent{
			{
				ID:        "evt-1", RequestID: "exec-1", Action: "skills.execute",
				Outcome: "success", Timestamp: now, Duration: 200 * time.Millisecond,
				TenantID: "t1", TraceID: "trace-abc",
			},
			{
				ID: "evt-2", RequestID: "exec-1", Action: "skills.execute",
				StepID: "step-0", StepName: "search", StepType: "tool",
				Outcome: "success", Timestamp: now.Add(10 * time.Millisecond), Duration: 80 * time.Millisecond,
				ToolInput:  map[string]any{"query": "test"},
				ToolOutput: "found 3 results",
			},
			{
				ID: "evt-3", RequestID: "exec-1", Action: "policy.enforce",
				Outcome: "allowed", Timestamp: now.Add(5 * time.Millisecond),
			},
		},
	})

	graph, err := b.Build(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(graph.Nodes) != 3 {
		t.Fatalf("Nodes = %d, want 3", len(graph.Nodes))
	}
	if graph.TraceID != "trace-abc" {
		t.Errorf("TraceID = %q, want %q", graph.TraceID, "trace-abc")
	}

	// Check node types
	types := make(map[string]int)
	for _, n := range graph.Nodes {
		types[n.Type]++
	}
	if types["execution"] != 1 {
		t.Errorf("expected 1 execution node, got %d", types["execution"])
	}
	if types["tool_call"] != 1 {
		t.Errorf("expected 1 tool_call node, got %d", types["tool_call"])
	}
	if types["policy_check"] != 1 {
		t.Errorf("expected 1 policy_check node, got %d", types["policy_check"])
	}

	// Check chronological order
	for i := 1; i < len(graph.Nodes); i++ {
		if graph.Nodes[i].StartedAt.Before(graph.Nodes[i-1].StartedAt) {
			t.Errorf("nodes not sorted: node[%d] at %v before node[%d] at %v",
				i, graph.Nodes[i].StartedAt, i-1, graph.Nodes[i-1].StartedAt)
		}
	}

	// Check step has parent
	var stepNode *LineageNode
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == "tool_call" {
			stepNode = &graph.Nodes[i]
			break
		}
	}
	if stepNode == nil {
		t.Fatal("tool_call node not found")
	}
	if stepNode.ParentID != "exec-1" {
		t.Errorf("step ParentID = %q, want %q", stepNode.ParentID, "exec-1")
	}
	if stepNode.Payload == nil {
		t.Error("step should have payload from ToolInput/ToolOutput")
	}
}

func TestLineageBuilder_QueryError(t *testing.T) {
	b := NewLineageBuilder(&mockLineageQuerier{
		err: context.DeadlineExceeded,
	})
	_, err := b.Build(context.Background(), "exec-1")
	if err == nil {
		t.Error("should return error on query failure")
	}
}

func TestInferNodeType(t *testing.T) {
	tests := []struct {
		action   string
		stepType string
		stepID   string
		want     string
	}{
		{"policy.enforce", "", "", "policy_check"},
		{"skills.execute", "tool", "s1", "tool_call"},
		{"skills.execute", "llm", "s2", "llm_request"},
		{"skills.execute", "skill", "s3", "step"},
		{"skills.execute", "", "", "execution"},
	}
	for _, tt := range tests {
		e := audit.AuditEvent{Action: tt.action, StepType: tt.stepType, StepID: tt.stepID}
		got := inferNodeType(e)
		if got != tt.want {
			t.Errorf("inferNodeType(%q, %q, %q) = %q, want %q", tt.action, tt.stepType, tt.stepID, got, tt.want)
		}
	}
}
