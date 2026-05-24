package store

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/telemetry"
)

func strPtr(s string) *string                         { return &s }
func levelPtr(l telemetry.EventLevel) *telemetry.EventLevel { return &l }

func TestRingBufferSpanStore_RecordAndQuery(t *testing.T) {
	store := NewRingBufferSpanStore(100)
	traceID := telemetry.NewTraceID()

	span1 := telemetry.Span{
		TraceID:   traceID,
		SpanID:    telemetry.NewSpanID(),
		Name:      "execution.run",
		Kind:      telemetry.SpanKindExecution,
		StartTime: time.Now().Add(-1 * time.Second),
		EndTime:   time.Now(),
		Status:    telemetry.SpanStatusOK,
	}
	span2 := telemetry.Span{
		TraceID:   traceID,
		SpanID:    telemetry.NewSpanID(),
		Name:      "tool.call",
		Kind:      telemetry.SpanKindToolCall,
		StartTime: time.Now().Add(-500 * time.Millisecond),
		EndTime:   time.Now(),
		Status:    telemetry.SpanStatusOK,
	}

	if err := store.RecordSpan(context.Background(), span1); err != nil {
		t.Fatalf("RecordSpan span1: %v", err)
	}
	if err := store.RecordSpan(context.Background(), span2); err != nil {
		t.Fatalf("RecordSpan span2: %v", err)
	}

	spans, err := store.QuerySpans(context.Background(), SpanQuery{TraceID: &traceID})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans count = %d, want 2", len(spans))
	}
}

func TestRingBufferSpanStore_Eviction(t *testing.T) {
	store := NewRingBufferSpanStore(5)
	for i := 0; i < 10; i++ {
		span := telemetry.Span{
			TraceID:   telemetry.NewTraceID(),
			SpanID:    telemetry.NewSpanID(),
			Name:      "test",
			Kind:      telemetry.SpanKindExecution,
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Status:    telemetry.SpanStatusOK,
		}
		if err := store.RecordSpan(context.Background(), span); err != nil {
			t.Fatalf("RecordSpan %d: %v", i, err)
		}
	}
	all, err := store.QuerySpans(context.Background(), SpanQuery{})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("after overflow: spans = %d, want 5 (ring buffer size)", len(all))
	}
}

func TestRingBufferSpanStore_QueryByExecutionID(t *testing.T) {
	store := NewRingBufferSpanStore(100)
	traceID := telemetry.NewTraceID()
	execID := "exec-test-123"

	span := telemetry.Span{
		TraceID:    traceID,
		SpanID:     telemetry.NewSpanID(),
		Name:       "execution.run",
		Kind:       telemetry.SpanKindExecution,
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Status:     telemetry.SpanStatusOK,
		Attributes: map[string]string{"execution_id": execID},
	}
	store.RecordSpan(context.Background(), span)

	spans, err := store.QuerySpans(context.Background(), SpanQuery{ExecutionID: &execID})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans count = %d, want 1", len(spans))
	}
}

func TestRingBufferEventStore_RecordAndQuery(t *testing.T) {
	store := NewRingBufferEventStore(100)
	traceID := telemetry.NewTraceID()

	evt := telemetry.TelemetryEvent{
		Timestamp: time.Now(),
		TraceID:   traceID,
		SpanID:    telemetry.NewSpanID(),
		Component: "planner",
		Operation: "plan",
		Level:     telemetry.EventLevelInfo,
		Message:   "plan generated",
	}
	if err := store.RecordEvent(context.Background(), evt); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	events, err := store.QueryEvents(context.Background(), EventQuery{Component: strPtr("planner")})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
}

func TestRingBufferEventStore_FilterByLevel(t *testing.T) {
	store := NewRingBufferEventStore(100)
	store.RecordEvent(context.Background(), telemetry.TelemetryEvent{
		Timestamp: time.Now(),
		Component: "provider",
		Level:     telemetry.EventLevelError,
		Message:   "connection failed",
	})
	store.RecordEvent(context.Background(), telemetry.TelemetryEvent{
		Timestamp: time.Now(),
		Component: "provider",
		Level:     telemetry.EventLevelInfo,
		Message:   "connection restored",
	})

	errLevel := telemetry.EventLevelError
	events, err := store.QueryEvents(context.Background(), EventQuery{Level: levelPtr(errLevel)})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("error events = %d, want 1", len(events))
	}
	if events[0].Message != "connection failed" {
		t.Fatalf("event message = %q, want %q", events[0].Message, "connection failed")
	}
}

func TestRingBufferEventStore_Pagination(t *testing.T) {
	store := NewRingBufferEventStore(100)
	for i := 0; i < 10; i++ {
		store.RecordEvent(context.Background(), telemetry.TelemetryEvent{
			Timestamp: time.Now(),
			Component: "test",
			Level:     telemetry.EventLevelInfo,
			Message:   "event",
		})
	}

	page1, _ := store.QueryEvents(context.Background(), EventQuery{Limit: 5, Offset: 0})
	page2, _ := store.QueryEvents(context.Background(), EventQuery{Limit: 5, Offset: 5})
	if len(page1) != 5 {
		t.Fatalf("page1 = %d, want 5", len(page1))
	}
	if len(page2) != 5 {
		t.Fatalf("page2 = %d, want 5", len(page2))
	}
}
