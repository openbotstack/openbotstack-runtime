package store

import (
	"context"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/telemetry"
)

// SpanQuery filters spans for retrieval.
type SpanQuery struct {
	TraceID     *telemetry.TraceID
	ExecutionID *string
	TimeRange   *TimeRange
	Limit       int
	Offset      int
}

// EventQuery filters telemetry events for retrieval.
type EventQuery struct {
	TraceID   *telemetry.TraceID
	Component *string
	Level     *telemetry.EventLevel
	TimeRange *TimeRange
	Limit     int
	Offset    int
}

// TimeRange constrains queries to a time window.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// SpanStore records and queries telemetry spans.
type SpanStore interface {
	RecordSpan(ctx context.Context, span telemetry.Span) error
	QuerySpans(ctx context.Context, query SpanQuery) ([]telemetry.Span, error)
}

// EventStore records and queries telemetry events.
type EventStore interface {
	RecordEvent(ctx context.Context, event telemetry.TelemetryEvent) error
	QueryEvents(ctx context.Context, query EventQuery) ([]telemetry.TelemetryEvent, error)
}

// RingBufferSpanStore is an in-memory ring buffer for spans.
type RingBufferSpanStore struct {
	mu    sync.RWMutex
	buf   []telemetry.Span
	size  int
	count int
}

// NewRingBufferSpanStore creates a ring buffer with the given capacity.
func NewRingBufferSpanStore(capacity int) *RingBufferSpanStore {
	return &RingBufferSpanStore{
		buf:  make([]telemetry.Span, 0, capacity),
		size: capacity,
	}
}

// RecordSpan appends a span, evicting oldest when at capacity.
func (s *RingBufferSpanStore) RecordSpan(_ context.Context, span telemetry.Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) >= s.size {
		copy(s.buf, s.buf[1:])
		s.buf = s.buf[:len(s.buf)-1]
	}
	s.buf = append(s.buf, span)
	s.count++
	return nil
}

// QuerySpans returns spans matching the query filters.
func (s *RingBufferSpanStore) QuerySpans(_ context.Context, q SpanQuery) ([]telemetry.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []telemetry.Span
	for _, sp := range s.buf {
		if q.TraceID != nil && sp.TraceID != *q.TraceID {
			continue
		}
		if q.ExecutionID != nil {
			if sp.Attributes == nil || sp.Attributes["execution_id"] != *q.ExecutionID {
				continue
			}
		}
		if q.TimeRange != nil {
			if sp.StartTime.Before(q.TimeRange.Start) || sp.EndTime.After(q.TimeRange.End) {
				continue
			}
		}
		result = append(result, sp)
	}

	result = applyPagination(result, q.Limit, q.Offset)
	return result, nil
}

// RingBufferEventStore is an in-memory ring buffer for telemetry events.
type RingBufferEventStore struct {
	mu    sync.RWMutex
	buf   []telemetry.TelemetryEvent
	size  int
	count int
}

// NewRingBufferEventStore creates a ring buffer with the given capacity.
func NewRingBufferEventStore(capacity int) *RingBufferEventStore {
	return &RingBufferEventStore{
		buf:  make([]telemetry.TelemetryEvent, 0, capacity),
		size: capacity,
	}
}

// RecordEvent appends an event, evicting oldest when at capacity.
func (s *RingBufferEventStore) RecordEvent(_ context.Context, evt telemetry.TelemetryEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.buf) >= s.size {
		copy(s.buf, s.buf[1:])
		s.buf = s.buf[:len(s.buf)-1]
	}
	s.buf = append(s.buf, evt)
	s.count++
	return nil
}

// QueryEvents returns events matching the query filters.
func (s *RingBufferEventStore) QueryEvents(_ context.Context, q EventQuery) ([]telemetry.TelemetryEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []telemetry.TelemetryEvent
	for _, evt := range s.buf {
		if q.TraceID != nil && evt.TraceID != *q.TraceID {
			continue
		}
		if q.Component != nil && evt.Component != *q.Component {
			continue
		}
		if q.Level != nil && evt.Level != *q.Level {
			continue
		}
		if q.TimeRange != nil {
			if evt.Timestamp.Before(q.TimeRange.Start) || evt.Timestamp.After(q.TimeRange.End) {
				continue
			}
		}
		result = append(result, evt)
	}

	result = applyPaginationEvents(result, q.Limit, q.Offset)
	return result, nil
}

func applyPagination(spans []telemetry.Span, limit, offset int) []telemetry.Span {
	if offset > len(spans) {
		return nil
	}
	spans = spans[offset:]
	if limit > 0 && limit < len(spans) {
		spans = spans[:limit]
	}
	return spans
}

func applyPaginationEvents(events []telemetry.TelemetryEvent, limit, offset int) []telemetry.TelemetryEvent {
	if offset > len(events) {
		return nil
	}
	events = events[offset:]
	if limit > 0 && limit < len(events) {
		events = events[:limit]
	}
	return events
}
