package api

import (
	"context"
	"sort"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// AuditExecutionStore adapts AuditLogger to ExecutionStore.
type AuditExecutionStore struct {
	logger execution_logs.AuditLogger
}

// NewAuditExecutionStore creates an ExecutionStore backed by audit logs.
func NewAuditExecutionStore(logger execution_logs.AuditLogger) *AuditExecutionStore {
	return &AuditExecutionStore{logger: logger}
}

// QueryExecutions returns recent execution records derived from audit logs.
// The harness emits one audit event per step with action "harness.step"; these
// are grouped by execution (RequestID) into a single ExecutionRecord each,
// with aggregated step count, total duration, and overall status.
func (s *AuditExecutionStore) QueryExecutions(ctx context.Context, limit int) ([]ExecutionRecord, error) {
	// Fetch recent executor events (more than `limit` so grouping still yields
	// enough distinct executions after collapsing steps).
	fetchN := limit * 5
	if fetchN < 100 {
		fetchN = 100
	}
	events, err := s.logger.Query(ctx, execution_logs.QueryFilter{
		Source: audit.SourceExecutor,
		Limit:  fetchN,
	})
	if err != nil {
		return nil, err
	}

	// Group step events by execution (RequestID), preserving most-recent-first order.
	type agg struct {
		executionID string
		sessionID   string
		skillID     string // first step's resource name
		totalMs     int64
		status      string
		errMsg      string
		lastTS      time.Time // most recent event timestamp, for ordering
	}
	groups := make(map[string]*agg)
	for _, e := range events {
		key := e.RequestID
		if key == "" {
			key = e.ID
		}
		g, ok := groups[key]
		if !ok {
			g = &agg{executionID: key, status: "completed"}
			groups[key] = g
		}
		if g.skillID == "" {
			g.skillID = e.Resource
		}
		g.totalMs += e.Duration.Milliseconds()
		g.sessionID = e.Metadata["session_id"]
		if e.Timestamp.After(g.lastTS) {
			g.lastTS = e.Timestamp
		}
		if e.Outcome == "failure" || e.Outcome == "error" || e.Status == "error" {
			g.status = "error"
			if e.Error != "" {
				g.errMsg = e.Error
			} else if e.Metadata["error"] != "" {
				g.errMsg = e.Metadata["error"]
			}
		}
	}

	// Order explicitly by most-recent-event descending. Do NOT rely on the
	// logger's query ordering (SQLite sorts DESC, in-memory does not).
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return groups[keys[i]].lastTS.After(groups[keys[j]].lastTS)
	})

	records := make([]ExecutionRecord, 0, len(keys))
	for _, key := range keys {
		g := groups[key]
		records = append(records, ExecutionRecord{
			ExecutionID: g.executionID,
			SessionID:   g.sessionID,
			SkillID:     g.skillID,
			DurationMs:  g.totalMs,
			Status:      g.status,
			Error:       g.errMsg,
		})
		if len(records) >= limit {
			break
		}
	}
	return records, nil
}
