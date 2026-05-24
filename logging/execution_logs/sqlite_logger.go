package execution_logs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
)

// ChainSigner signs audit events for tamper-proof chain integrity.
type ChainSigner interface {
	Sign(event audit.AuditEvent, prevSignature string) (string, error)
}

// SQLiteAuditLogger implements AuditLogger using SQLite.
type SQLiteAuditLogger struct {
	db     *sql.DB
	signer ChainSigner
}

// NewSQLiteAuditLogger creates a new SQLite-backed audit logger.
func NewSQLiteAuditLogger(db *sql.DB) *SQLiteAuditLogger {
	return &SQLiteAuditLogger{db: db}
}

// SetSigner sets an optional chain signer for tamper-proof audit events.
func (l *SQLiteAuditLogger) SetSigner(s ChainSigner) {
	l.signer = s
}

// Log records an audit event.
func (l *SQLiteAuditLogger) Log(ctx context.Context, event audit.AuditEvent) error {
	metadataJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	var signature string
	if l.signer != nil {
		prevSig := l.lastSignature()
		// Sign only the fields stored in DB so verification can reconstruct the same payload.
		signable := audit.AuditEvent{
			ID:        event.ID,
			TenantID:  event.TenantID,
			UserID:    event.UserID,
			RequestID: event.RequestID,
			Action:    event.Action,
			Resource:  event.Resource,
			Outcome:   event.Outcome,
			Duration:  event.Duration.Truncate(time.Millisecond),
			Timestamp: event.Timestamp.UTC(),
			Source:    event.Source,
		}
		sig, err := l.signer.Sign(signable, prevSig)
		if err != nil {
			return fmt.Errorf("sign event %s: %w", event.ID, err)
		}
		signature = sig
	}

	_, err = l.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, tenant_id, user_id, request_id, action, resource,
		                        outcome, source, duration_ms, metadata, timestamp, signature)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.TenantID,
		event.UserID,
		event.RequestID,
		event.Action,
		event.Resource,
		event.Outcome,
		string(event.Source),
		event.Duration.Milliseconds(),
		string(metadataJSON),
		event.Timestamp.UTC().Format(time.RFC3339Nano),
		signature,
	)
	if err != nil {
		return fmt.Errorf("log event %s: %w", event.ID, err)
	}
	return nil
}

// lastSignature returns the signature of the most recent audit event.
func (l *SQLiteAuditLogger) lastSignature() string {
	var sig string
	err := l.db.QueryRow(`SELECT signature FROM audit_logs ORDER BY timestamp DESC, id DESC LIMIT 1`).Scan(&sig)
	if err != nil {
		return ""
	}
	return sig
}

// Query retrieves audit events matching the filter.
func (l *SQLiteAuditLogger) Query(ctx context.Context, filter QueryFilter) ([]audit.AuditEvent, error) {
	query := `SELECT id, tenant_id, user_id, request_id, action, resource,
	                 outcome, source, duration_ms, metadata, timestamp, signature
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
	defer func() { _ = rows.Close() }()

	var events []audit.AuditEvent
	for rows.Next() {
		var e audit.AuditEvent
		var durationMs int64
		var metadataJSON, timestampStr, sourceStr, signatureStr string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.RequestID,
			&e.Action, &e.Resource, &e.Outcome, &sourceStr,
			&durationMs, &metadataJSON, &timestampStr, &signatureStr); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.Source = audit.Source(sourceStr)
		e.Duration = time.Duration(durationMs) * time.Millisecond
		_ = json.Unmarshal([]byte(metadataJSON), &e.Metadata)
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
		events = append(events, e)
	}
	return events, rows.Err()
}

// QueryWithSignatures retrieves audit events with their chain signatures.
func (l *SQLiteAuditLogger) QueryWithSignatures(ctx context.Context, filter QueryFilter) ([]audit.AuditEvent, []string, error) {
	query := `SELECT id, tenant_id, user_id, request_id, action, resource,
	                 outcome, source, duration_ms, metadata, timestamp, signature
	          FROM audit_logs WHERE 1=1`
	var args []any
	query, args = l.buildWhere(query, args, filter)
	query += " ORDER BY timestamp ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []audit.AuditEvent
	var signatures []string
	for rows.Next() {
		var e audit.AuditEvent
		var durationMs int64
		var metadataJSON, timestampStr, sourceStr, signatureStr string
		if err := rows.Scan(&e.ID, &e.TenantID, &e.UserID, &e.RequestID,
			&e.Action, &e.Resource, &e.Outcome, &sourceStr,
			&durationMs, &metadataJSON, &timestampStr, &signatureStr); err != nil {
			return nil, nil, fmt.Errorf("scan event: %w", err)
		}
		e.Source = audit.Source(sourceStr)
		e.Duration = time.Duration(durationMs) * time.Millisecond
		_ = json.Unmarshal([]byte(metadataJSON), &e.Metadata)
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, timestampStr)
		events = append(events, e)
		signatures = append(signatures, signatureStr)
	}
	return events, signatures, rows.Err()
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

// PurgeBefore deletes audit events older than the given cutoff time, optionally filtered by tenant.
func (l *SQLiteAuditLogger) PurgeBefore(ctx context.Context, cutoff time.Time, tenantID string) (int64, error) {
	query := "DELETE FROM audit_logs WHERE timestamp < ?"
	args := []any{cutoff.UTC().Format(time.RFC3339Nano)}

	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	result, err := l.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("purge audit logs: %w", err)
	}
	return result.RowsAffected()
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
	if f.Source != "" {
		query += " AND source = ?"
		args = append(args, string(f.Source))
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
