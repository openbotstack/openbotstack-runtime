package execution_logs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Event represents an audit log entry.
type Event struct {
	ID        string
	TenantID  string
	UserID    string
	RequestID string
	Action    string // e.g., "skills.execute", "model.generate"
	Resource  string // e.g., "skill/search", "model/claude"
	Outcome   string // "success", "failure", "timeout"
	Duration  time.Duration
	Metadata  map[string]string
	Timestamp time.Time
}

// QueryFilter defines filters for audit queries.
type QueryFilter struct {
	TenantID  string
	UserID    string
	RequestID string
	Action    string
	From      time.Time
	To        time.Time
	Limit     int
}

// AuditLogger provides audit logging operations.
type AuditLogger interface {
	Log(ctx context.Context, event Event) error
	Query(ctx context.Context, filter QueryFilter) ([]Event, error)
	Count(ctx context.Context, filter QueryFilter) (int, error)
}

// PGAuditLogger implements AuditLogger using PostgreSQL via pgxpool.
type PGAuditLogger struct {
	pool *pgxpool.Pool
}

// NewPGAuditLogger creates a new audit logger connected to the given pool.
func NewPGAuditLogger(pool *pgxpool.Pool) *PGAuditLogger {
	return &PGAuditLogger{
		pool: pool,
	}
}

// Initialize creates the audit_logs table if it doesn't exist.
func (l *PGAuditLogger) Initialize(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS audit_logs (
		id VARCHAR(255) PRIMARY KEY,
		tenant_id VARCHAR(255) NOT NULL,
		user_id VARCHAR(255) NOT NULL,
		request_id VARCHAR(255) NOT NULL,
		action VARCHAR(255) NOT NULL,
		resource VARCHAR(255) NOT NULL,
		outcome VARCHAR(255) NOT NULL,
		duration_ms BIGINT NOT NULL,
		timestamp TIMESTAMP WITH TIME ZONE NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_id ON audit_logs(tenant_id);
	CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp);
	`
	_, err := l.pool.Exec(ctx, query)
	return err
}

// Log records an audit event.
func (l *PGAuditLogger) Log(ctx context.Context, event Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	query := `
	INSERT INTO audit_logs (id, tenant_id, user_id, request_id, action, resource, outcome, duration_ms, timestamp)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := l.pool.Exec(ctx, query,
		event.ID,
		event.TenantID,
		event.UserID,
		event.RequestID,
		event.Action,
		event.Resource,
		event.Outcome,
		event.Duration.Milliseconds(),
		event.Timestamp,
	)
	return err
}

// Query retrieves audit events matching the filter.
func (l *PGAuditLogger) Query(ctx context.Context, filter QueryFilter) ([]Event, error) {
	query, args := buildQuery("SELECT id, tenant_id, user_id, request_id, action, resource, outcome, duration_ms, timestamp FROM audit_logs", filter)
	
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

	rows, err := l.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var durMs int64
		err := rows.Scan(
			&e.ID,
			&e.TenantID,
			&e.UserID,
			&e.RequestID,
			&e.Action,
			&e.Resource,
			&e.Outcome,
			&durMs,
			&e.Timestamp,
		)
		if err != nil {
			return nil, err
		}
		e.Duration = time.Duration(durMs) * time.Millisecond
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// Count returns the number of events matching the filter.
func (l *PGAuditLogger) Count(ctx context.Context, filter QueryFilter) (int, error) {
	query, args := buildQuery("SELECT COUNT(*) FROM audit_logs", filter)

	var count int
	err := l.pool.QueryRow(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// buildQuery constructs a conditional WHERE clause based on the provided filter.
func buildQuery(base string, f QueryFilter) (string, []any) {
	query := base + " WHERE 1=1"
	var args []any
	argID := 1

	if f.TenantID != "" {
		query += fmt.Sprintf(" AND tenant_id = $%d", argID)
		args = append(args, f.TenantID)
		argID++
	}
	if f.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argID)
		args = append(args, f.UserID)
		argID++
	}
	if f.RequestID != "" {
		query += fmt.Sprintf(" AND request_id = $%d", argID)
		args = append(args, f.RequestID)
		argID++
	}
	if f.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argID)
		args = append(args, f.Action)
		argID++
	}
	if !f.From.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argID)
		args = append(args, f.From)
		argID++
	}
	if !f.To.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argID)
		args = append(args, f.To)
	}
	return query, args
}
