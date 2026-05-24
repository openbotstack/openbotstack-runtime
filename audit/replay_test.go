package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// mockReplayQuerier is a test double for the AuditQuerier interface.
type mockReplayQuerier struct {
	events []audit.AuditEvent
	err    error
}

func (m *mockReplayQuerier) Query(_ context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Filter by RequestID if set
	if filter.RequestID != "" {
		var filtered []audit.AuditEvent
		for _, e := range m.events {
			if e.RequestID == filter.RequestID {
				filtered = append(filtered, e)
			}
		}
		return filtered, nil
	}
	return m.events, nil
}

func (m *mockReplayQuerier) Count(_ context.Context, filter execution_logs.QueryFilter) (int, error) {
	return len(m.events), nil
}

// helper to create a time pointer
func timePtr(t time.Time) *time.Time { return &t }

// --- NORMAL ---

func TestReplayBuilder_FullExecution(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		{
			ID:        "evt-1",
			TenantID:  "tenant-1",
			RequestID: "exec-123",
			Action:    "skills.execute",
			Resource:  "skill/search",
			Outcome:   "started",
			StepID:    "step-1",
			StepName:  "search",
			StepType:  "tool",
			Status:    "started",
			Timestamp: base,
			ToolInput: map[string]any{"query": "test"},
		},
		{
			ID:        "evt-2",
			TenantID:  "tenant-1",
			RequestID: "exec-123",
			Action:    "skills.execute",
			Resource:  "skill/search",
			Outcome:   "success",
			StepID:    "step-1",
			StepName:  "search",
			StepType:  "tool",
			Status:    "completed",
			Duration:  150 * time.Millisecond,
			Timestamp: base.Add(150 * time.Millisecond),
			ToolOutput: map[string]any{"result": "found"},
		},
		{
			ID:        "evt-3",
			TenantID:  "tenant-1",
			RequestID: "exec-123",
			Action:    "skills.execute",
			Resource:  "skill/summarize",
			Outcome:   "started",
			StepID:    "step-2",
			StepName:  "summarize",
			StepType:  "llm",
			Status:    "started",
			Timestamp: base.Add(200 * time.Millisecond),
			ToolInput: map[string]any{"text": "long text"},
		},
		{
			ID:        "evt-4",
			TenantID:  "tenant-1",
			RequestID: "exec-123",
			Action:    "skills.execute",
			Resource:  "skill/summarize",
			Outcome:   "success",
			StepID:    "step-2",
			StepName:  "summarize",
			StepType:  "llm",
			Status:    "completed",
			Duration:  500 * time.Millisecond,
			Timestamp: base.Add(700 * time.Millisecond),
			ToolOutput: "summary result",
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-123")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if replay.ExecutionID != "exec-123" {
		t.Errorf("ExecutionID = %q, want %q", replay.ExecutionID, "exec-123")
	}
	if replay.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", replay.TenantID, "tenant-1")
	}
	if replay.Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Status, "completed")
	}
	if len(replay.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(replay.Steps))
	}

	// Verify first step
	s1 := replay.Steps[0]
	if s1.StepID != "step-1" {
		t.Errorf("Step[0].StepID = %q, want %q", s1.StepID, "step-1")
	}
	if s1.StepName != "search" {
		t.Errorf("Step[0].StepName = %q, want %q", s1.StepName, "search")
	}
	if s1.StepType != "tool" {
		t.Errorf("Step[0].StepType = %q, want %q", s1.StepType, "tool")
	}
	if s1.Status != "completed" {
		t.Errorf("Step[0].Status = %q, want %q", s1.Status, "completed")
	}
	if s1.Duration != 150 {
		t.Errorf("Step[0].Duration = %d, want 150", s1.Duration)
	}

	// Verify total duration
	if replay.TotalDuration != 700 {
		t.Errorf("TotalDuration = %d, want 700", replay.TotalDuration)
	}
}

// --- ABNORMAL --}

func TestReplayBuilder_EmptyExecutionID(t *testing.T) {
	querier := &mockReplayQuerier{}
	builder := NewReplayBuilder(querier)
	_, err := builder.Build(context.Background(), "")

	if err == nil {
		t.Fatal("expected error for empty execution ID, got nil")
	}
	if err != ErrEmptyExecutionID {
		t.Errorf("error = %v, want ErrEmptyExecutionID", err)
	}
}

func TestReplayBuilder_NonexistentExecution(t *testing.T) {
	querier := &mockReplayQuerier{events: []audit.AuditEvent{
		{ID: "e1", RequestID: "exec-999", Action: "test", Timestamp: time.Now()},
	}}
	builder := NewReplayBuilder(querier)
	_, err := builder.Build(context.Background(), "exec-nonexistent")

	if err == nil {
		t.Fatal("expected error for nonexistent execution, got nil")
	}
	if err != ErrExecutionNotFound {
		t.Errorf("error = %v, want ErrExecutionNotFound", err)
	}
}

func TestReplayBuilder_NoEvents(t *testing.T) {
	querier := &mockReplayQuerier{events: nil}
	builder := NewReplayBuilder(querier)
	_, err := builder.Build(context.Background(), "exec-empty")

	if err == nil {
		t.Fatal("expected error for no events, got nil")
	}
	if err != ErrExecutionNotFound {
		t.Errorf("error = %v, want ErrExecutionNotFound", err)
	}
}

func TestReplayBuilder_PartialSteps(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-partial",
			StepID: "step-1", StepName: "step1", StepType: "tool",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base,
		},
		// step-1 has no completed event
		{
			ID: "e2", TenantID: "t1", RequestID: "exec-partial",
			StepID: "step-2", StepName: "step2", StepType: "llm",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base.Add(100 * time.Millisecond),
		},
		{
			ID: "e3", TenantID: "t1", RequestID: "exec-partial",
			StepID: "step-2", StepName: "step2", StepType: "llm",
			Status: "completed", Outcome: "success", Action: "skills.execute",
			Duration: 200 * time.Millisecond,
			Timestamp: base.Add(300 * time.Millisecond),
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-partial")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Overall status should be "partial" because step-1 has no completion
	if replay.Status != "partial" {
		t.Errorf("Status = %q, want %q", replay.Status, "partial")
	}
	if len(replay.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(replay.Steps))
	}

	// step-1 should be "started" (never completed)
	if replay.Steps[0].Status != "started" {
		t.Errorf("Step[0].Status = %q, want %q", replay.Steps[0].Status, "started")
	}
	// step-2 should be "completed"
	if replay.Steps[1].Status != "completed" {
		t.Errorf("Step[1].Status = %q, want %q", replay.Steps[1].Status, "completed")
	}
}

func TestReplayBuilder_OutOfOrderEvents(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		// Deliberately out of order
		{
			ID: "e2", TenantID: "t1", RequestID: "exec-ooo",
			StepID: "step-1", StepName: "search", StepType: "tool",
			Status: "completed", Outcome: "success", Action: "skills.execute",
			Duration: 100 * time.Millisecond,
			Timestamp: base.Add(100 * time.Millisecond),
		},
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-ooo",
			StepID: "step-1", StepName: "search", StepType: "tool",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base,
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-ooo")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(replay.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(replay.Steps))
	}
	// Step should still be completed
	if replay.Steps[0].Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Steps[0].Status, "completed")
	}
	if replay.Steps[0].Duration != 100 {
		t.Errorf("Duration = %d, want 100", replay.Steps[0].Duration)
	}
}

func TestReplayBuilder_FailedStep(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-fail",
			StepID: "step-1", StepName: "search", StepType: "tool",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base,
		},
		{
			ID: "e2", TenantID: "t1", RequestID: "exec-fail",
			StepID: "step-1", StepName: "search", StepType: "tool",
			Status: "failed", Outcome: "failure", Action: "skills.execute",
			Error:   "timeout",
			Duration: 60 * time.Second,
			Timestamp: base.Add(60 * time.Second),
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-fail")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if replay.Status != "failed" {
		t.Errorf("Status = %q, want %q", replay.Status, "failed")
	}
	if len(replay.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(replay.Steps))
	}
	if replay.Steps[0].Status != "failed" {
		t.Errorf("Step[0].Status = %q, want %q", replay.Steps[0].Status, "failed")
	}
	if replay.Steps[0].Error != "timeout" {
		t.Errorf("Step[0].Error = %q, want %q", replay.Steps[0].Error, "timeout")
	}
}

func TestReplayBuilder_NilProvider(t *testing.T) {
	builder := NewReplayBuilder(nil)
	_, err := builder.Build(context.Background(), "exec-123")

	if err == nil {
		t.Fatal("expected error for nil provider, got nil")
	}
	if err != ErrNilProvider {
		t.Errorf("error = %v, want ErrNilProvider", err)
	}
}

func TestReplayBuilder_SingleStep(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-single",
			StepID: "step-1", StepName: "echo", StepType: "tool",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base,
		},
		{
			ID: "e2", TenantID: "t1", RequestID: "exec-single",
			StepID: "step-1", StepName: "echo", StepType: "tool",
			Status: "completed", Outcome: "success", Action: "skills.execute",
			Duration: 50 * time.Millisecond,
			Timestamp: base.Add(50 * time.Millisecond),
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-single")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(replay.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(replay.Steps))
	}
	if replay.Steps[0].StepName != "echo" {
		t.Errorf("StepName = %q, want %q", replay.Steps[0].StepName, "echo")
	}
	if replay.Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Status, "completed")
	}
}

func TestReplayBuilder_LargeExecution(t *testing.T) {
	base := time.Now()
	var events []audit.AuditEvent

	// Create 100 steps with started + completed each
	for i := 0; i < 100; i++ {
		startTime := base.Add(time.Duration(i*2) * time.Second)
		sid := fmt.Sprintf("step-%03d", i)
		events = append(events, audit.AuditEvent{
			ID: "start-" + sid,
			TenantID: "t1", RequestID: "exec-large",
			StepID:   sid,
			StepName: sid,
			StepType: "tool",
			Status:   "started", Outcome: "started",
			Action:    "skills.execute",
			Timestamp: startTime,
		})
		events = append(events, audit.AuditEvent{
			ID: "end-" + sid,
			TenantID: "t1", RequestID: "exec-large",
			StepID:   sid,
			StepName: sid,
			StepType: "tool",
			Status:   "completed", Outcome: "success",
			Action:    "skills.execute",
			Duration:  time.Second,
			Timestamp: startTime.Add(time.Second),
		})
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-large")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(replay.Steps) != 100 {
		t.Errorf("len(Steps) = %d, want 100", len(replay.Steps))
	}
	if replay.Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Status, "completed")
	}
}

func TestReplayBuilder_StepsMissingStarted(t *testing.T) {
	base := time.Now()
	// Only completed events, no started events
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-no-start",
			StepID: "step-1", StepName: "search", StepType: "tool",
			Status: "completed", Outcome: "success", Action: "skills.execute",
			Duration: 100 * time.Millisecond,
			Timestamp: base.Add(100 * time.Millisecond),
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-no-start")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(replay.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(replay.Steps))
	}
	// Step should be completed even without started event
	if replay.Steps[0].Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Steps[0].Status, "completed")
	}
	if replay.Steps[0].Duration != 100 {
		t.Errorf("Duration = %d, want 100", replay.Steps[0].Duration)
	}
}

func TestReplayBuilder_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	querier := &mockReplayQuerier{events: []audit.AuditEvent{
		{ID: "e1", RequestID: "exec-ctx", Action: "test", Timestamp: time.Now()},
	}}
	builder := NewReplayBuilder(querier)
	_, err := builder.Build(ctx, "exec-ctx")

	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestReplayBuilder_QueryError(t *testing.T) {
	querier := &mockReplayQuerier{err: context.DeadlineExceeded}
	builder := NewReplayBuilder(querier)
	_, err := builder.Build(context.Background(), "exec-err")

	if err == nil {
		t.Fatal("expected error from query failure, got nil")
	}
}

func TestReplayBuilder_ToolInputOutputSerialized(t *testing.T) {
	base := time.Now()
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-io",
			StepID: "step-1", StepName: "calc", StepType: "tool",
			Status: "started", Outcome: "started", Action: "skills.execute",
			Timestamp: base,
			ToolInput: map[string]any{"a": 1, "b": 2},
		},
		{
			ID: "e2", TenantID: "t1", RequestID: "exec-io",
			StepID: "step-1", StepName: "calc", StepType: "tool",
			Status: "completed", Outcome: "success", Action: "skills.execute",
			Duration: 10 * time.Millisecond,
			Timestamp: base.Add(10 * time.Millisecond),
			ToolOutput: map[string]any{"result": 3},
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-io")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if len(replay.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(replay.Steps))
	}

	// Verify input is serialized
	if len(replay.Steps[0].Input) == 0 {
		t.Error("expected non-empty Input")
	}
	var input map[string]any
	if err := json.Unmarshal(replay.Steps[0].Input, &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if input["a"].(float64) != 1 || input["b"].(float64) != 2 {
		t.Errorf("input = %v, want a=1, b=2", input)
	}

	// Verify output is serialized
	if len(replay.Steps[0].Output) == 0 {
		t.Error("expected non-empty Output")
	}
	var output map[string]any
	if err := json.Unmarshal(replay.Steps[0].Output, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if output["result"].(float64) != 3 {
		t.Errorf("output = %v, want result=3", output)
	}
}

func TestReplayBuilder_MultipleTenantsIsolated(t *testing.T) {
	base := time.Now()
	// Events from two different executions (different request IDs)
	events := []audit.AuditEvent{
		{
			ID: "e1", TenantID: "t1", RequestID: "exec-isolate",
			StepID: "step-1", Status: "completed", Outcome: "success",
			Action: "skills.execute", Duration: 50 * time.Millisecond,
			Timestamp: base,
		},
		{
			ID: "e2", TenantID: "t2", RequestID: "exec-other",
			StepID: "step-x", Status: "completed", Outcome: "success",
			Action: "skills.execute", Duration: 50 * time.Millisecond,
			Timestamp: base,
		},
	}

	querier := &mockReplayQuerier{events: events}
	builder := NewReplayBuilder(querier)
	replay, err := builder.Build(context.Background(), "exec-isolate")

	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Should only include events from exec-isolate
	if replay.TenantID != "t1" {
		t.Errorf("TenantID = %q, want %q", replay.TenantID, "t1")
	}
	if len(replay.Steps) != 1 {
		t.Errorf("len(Steps) = %d, want 1 (isolated to single execution)", len(replay.Steps))
	}
}
