package api

import (
	"context"

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

// QueryExecutions returns recent execution records from audit logs.
func (s *AuditExecutionStore) QueryExecutions(ctx context.Context, limit int) ([]ExecutionRecord, error) {
	events, err := s.logger.Query(ctx, execution_logs.QueryFilter{
		Action: "skills.execute",
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}

	records := make([]ExecutionRecord, 0, len(events))
	for _, e := range events {
		rec := ExecutionRecord{
			ExecutionID: e.ID,
			SessionID:   e.Metadata["session_id"],
			SkillID:     e.Resource,
			DurationMs:  e.Duration.Milliseconds(),
			Status:      e.Outcome,
		}
		if e.Outcome == "failure" || e.Outcome == "error" {
			rec.Error = e.Metadata["error"]
		}
		records = append(records, rec)
	}

	return records, nil
}
