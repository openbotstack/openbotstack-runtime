package execution_logs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLiteAuditLogger implements AuditLogger using SQLite.
type SQLiteAuditLogger struct {
	db *sql.DB
}

// NewSQLiteAuditLogger creates a new SQLite-backed audit logger.
func NewSQLiteAuditLogger(db *sql.DB) *SQLiteAuditLogger {
	return &SQLiteAuditLogger{db: db}
}

// Log records an audit event.
func (l *SQLiteAuditLogger) Log(ctx context.Context, event Event) error {
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	_, err = l.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, user_id, request_id, action, resource,
		                        outcome, duration_ms, metadata, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.TenantID,
		event.UserID,
		event.RequestID,
		event.Action,
		event.Resource,
		event.Outcome,
		event.Duration.Milliseconds(),
		string(metadataJSON),
		event.Timestamp.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("log event %s: %w", event.ID, err)
	}
	return nil
}

// Query retrieves audit events matching the filter.
func (l *SQLiteAuditLogger) Query(ctx context.Context, filter QueryFilter) ([]Event, error) {
	query := `SELECT id, tenant_id, user_id, request_id, action, resource,
	                 outcome, duration_ms, metadata, timestamp
	          FROM audit_logs WHERE 1=1`
	var args []any
	query, args = l.buildWhere(query, args, filter)
	query += " ORDER BY timestamp DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var durationMs int64
		var metadataJSON, timestampStr string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.RequestID,
			&e.Action, &e.Resource, &e.Outcome, &durationMs,
			&metadataJSON, &timestampStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Duration = time.Duration(durationMs) * time.Millisecond
		json.Unmarshal([]byte(metadataJSON), &e.Metadata)
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// Count returns the number of events matching the filter.
func (l *SQLiteAuditLogger) Count(ctx context.Context, filter QueryFilter) (int, error) {
	query := "SELECT COUNT(*) FROM audit_logs WHERE 1=1"
	var args []any
	query, args = l.buildWhere(query, args, filter)
	var count int
	err := l.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count audit logs: %w", err)
	}
	return count, nil
}

func (l *SQLiteAuditLogger) buildWhere(query string, args []any, f QueryFilter) (string, []any) {
	if f.TenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, f.TenantID)
	}
	if f.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, f.UserID)
	}
	if f.RequestID != "" {
		query += " AND request_id = ?"
		args = append(args, f.RequestID)
	}
	if f.Action != "" {
		query += " AND action = ?"
		args = append(args, f.Action)
	}
	if !f.From.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, f.From.UTC().Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, f.To.UTC().Format(time.RFC3339Nano))
	}
	return query, args
}
