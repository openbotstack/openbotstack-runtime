package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

// SQLiteSessionStateStore implements SessionStateStore using SQLite.
// It manages session metadata (not content) for fast listing and filtering.
// Conversation content remains in Markdown files via agent.ConversationStore.
type SQLiteSessionStateStore struct {
	db          *sql.DB
	strictTenant bool
}

// SessionStoreOption configures a SQLiteSessionStateStore.
type SessionStoreOption func(*SQLiteSessionStateStore)

// WithStrictTenant enables strict tenant isolation — operations without a
// tenant in context return an error instead of querying across tenants.
func WithStrictTenant(strict bool) SessionStoreOption {
	return func(s *SQLiteSessionStateStore) { s.strictTenant = strict }
}

// NewSQLiteSessionStateStore creates a new session state store backed by SQLite.
func NewSQLiteSessionStateStore(db *sql.DB, opts ...SessionStoreOption) *SQLiteSessionStateStore {
	s := &SQLiteSessionStateStore{db: db}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func tenantFromCtx(ctx context.Context) string {
	if user, ok := middleware.UserFromContext(ctx); ok {
		return user.TenantID
	}
	return ""
}

// requireTenant returns the tenant ID from context or an error if strict mode is enabled
// and no tenant is present.
func (s *SQLiteSessionStateStore) requireTenant(ctx context.Context) (string, error) {
	tid := tenantFromCtx(ctx)
	if tid == "" && s.strictTenant {
		return "", fmt.Errorf("memory: tenant ID required (strict mode)")
	}
	return tid, nil
}

// UpsertSession creates or updates session metadata.
// On first call for a session, it inserts a new row.
// On subsequent calls, it increments message_count and updates metadata.
func (s *SQLiteSessionStateStore) UpsertSession(ctx context.Context, meta SessionMeta) error {
	updatedAt := meta.UpdatedAt.Format(time.RFC3339Nano)
	lastPreview := meta.LastMessagePreview
	if len(lastPreview) > 200 {
		lastPreview = lastPreview[:200]
	}

	// Try UPDATE first (increment message_count)
	res, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET message_count = message_count + ?,
		    last_message_preview = ?,
		    updated_at = ?
		WHERE session_id = ? AND tenant_id = ?`,
		meta.MessageCount, lastPreview, updatedAt,
		meta.SessionID, meta.TenantID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}

	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}

	// No existing row — INSERT
	createdAt := meta.CreatedAt.Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions (session_id, tenant_id, user_id, message_count, last_message_preview, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		meta.SessionID, meta.TenantID, meta.UserID,
		meta.MessageCount, lastPreview, createdAt, updatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// ListSessions returns all sessions for the current tenant, ordered by most recent first.
func (s *SQLiteSessionStateStore) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}

	query := `SELECT session_id, tenant_id, message_count, last_message_preview, created_at, updated_at
			  FROM sessions`
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " ORDER BY updated_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var createdAtStr, updatedAtStr string
		if err := rows.Scan(&si.SessionID, &si.TenantID, &si.EntryCount, &si.LastEntry, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		si.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		si.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
		sessions = append(sessions, si)
	}
	return sessions, rows.Err()
}

// GetSession retrieves metadata for a specific session.
// Returns nil if not found.
func (s *SQLiteSessionStateStore) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return nil, err
	}

	query := `SELECT session_id, tenant_id, message_count, last_message_preview, created_at, updated_at
			  FROM sessions WHERE session_id = ?`
	args := []interface{}{sessionID}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	var si SessionInfo
	var createdAtStr, updatedAtStr string
	err = s.db.QueryRowContext(ctx, query, args...).Scan(
		&si.SessionID, &si.TenantID, &si.EntryCount, &si.LastEntry, &createdAtStr, &updatedAtStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	si.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	si.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
	return &si, nil
}

// DeleteSession removes session metadata.
func (s *SQLiteSessionStateStore) DeleteSession(ctx context.Context, sessionID string) error {
	tenantID, err := s.requireTenant(ctx)
	if err != nil {
		return err
	}

	query := "DELETE FROM sessions WHERE session_id = ?"
	args := []interface{}{sessionID}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	_, err = s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
