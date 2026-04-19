package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api/middleware"
)

// SQLiteMemoryStore implements ShortTermStore using SQLite.
type SQLiteMemoryStore struct {
	db *sql.DB
}

// NewSQLiteMemoryStore creates a new SQLite-backed memory store.
func NewSQLiteMemoryStore(db *sql.DB) *SQLiteMemoryStore {
	return &SQLiteMemoryStore{db: db}
}

// tenantFromCtx extracts the tenant_id from the authenticated user in context.
// Returns "" if no user is present, allowing backward compatibility with pre-auth data.
func tenantFromCtx(ctx context.Context) string {
	if user, ok := middleware.UserFromContext(ctx); ok {
		return user.TenantID
	}
	return ""
}

// Store saves a memory entry, replacing any existing entry with the same ID.
func (s *SQLiteMemoryStore) Store(ctx context.Context, entry Entry) error {
	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		tagsJSON = []byte("[]")
	}
	ttlSeconds := int64(0)
	if entry.TTL > 0 {
		ttlSeconds = int64(entry.TTL.Seconds())
	}

	tenantID := tenantFromCtx(ctx)

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO session_entries (id, session_id, tenant_id, content, tags, created_at, ttl)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SessionID, tenantID, entry.Content, string(tagsJSON),
		entry.CreatedAt.UTC().Format(time.RFC3339Nano), ttlSeconds,
	)
	if err != nil {
		return fmt.Errorf("store entry %s: %w", entry.ID, err)
	}
	return nil
}

// Retrieve gets a memory entry by ID, with lazy TTL expiry checking.
func (s *SQLiteMemoryStore) Retrieve(ctx context.Context, id string) (*Entry, error) {
	var e Entry
	var tagsJSON, createdAtStr string
	var ttlSeconds int64
	var entryTenantID string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, tenant_id, content, tags, created_at, ttl
		FROM session_entries WHERE id = ?`, id,
	).Scan(&e.ID, &e.SessionID, &entryTenantID, &e.Content, &tagsJSON, &createdAtStr, &ttlSeconds)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("retrieve entry %s: %w", id, err)
	}

	// Verify tenant isolation
	expectedTenant := tenantFromCtx(ctx)
	if expectedTenant != "" && entryTenantID != expectedTenant {
		return nil, ErrNotFound
	}

	if ttlSeconds > 0 {
		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
		if time.Since(createdAt) > time.Duration(ttlSeconds)*time.Second {
			_, _ = s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE id = ?", id)
			return nil, ErrNotFound
		}
	}

	_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	if ttlSeconds > 0 {
		e.TTL = time.Duration(ttlSeconds) * time.Second
	}
	return &e, nil
}

// ListBySession returns all non-expired entries for a session, ordered by created_at.
func (s *SQLiteMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]Entry, error) {
	tenantID := tenantFromCtx(ctx)
	query := `
		SELECT id, session_id, tenant_id, content, tags, created_at, ttl
		FROM session_entries WHERE session_id = ?`
	args := []interface{}{sessionID}

	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list session %s: %w", sessionID, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []Entry
	var expiredIDs []string

	for rows.Next() {
		var e Entry
		var tagsJSON, createdAtStr string
		var ttlSeconds int64
		var entryTenantID string
		if err := rows.Scan(&e.ID, &e.SessionID, &entryTenantID, &e.Content, &tagsJSON,
			&createdAtStr, &ttlSeconds); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}

		if ttlSeconds > 0 {
			createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
			if time.Since(createdAt) > time.Duration(ttlSeconds)*time.Second {
				expiredIDs = append(expiredIDs, e.ID)
				continue
			}
			e.TTL = time.Duration(ttlSeconds) * time.Second
		}

		_ = json.Unmarshal([]byte(tagsJSON), &e.Tags)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		entries = append(entries, e)
	}

	for _, id := range expiredIDs {
		_, _ = s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE id = ?", id)
	}

	return entries, rows.Err()
}

// Delete removes a memory entry by ID, returning ErrNotFound if it doesn't exist.
func (s *SQLiteMemoryStore) Delete(ctx context.Context, id string) error {
	tenantID := tenantFromCtx(ctx)
	query := "DELETE FROM session_entries WHERE id = ?"
	args := []interface{}{id}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete entry %s: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearSession removes all entries for a session.
func (s *SQLiteMemoryStore) ClearSession(ctx context.Context, sessionID string) error {
	tenantID := tenantFromCtx(ctx)
	query := "DELETE FROM session_entries WHERE session_id = ?"
	args := []interface{}{sessionID}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("clear session %s: %w", sessionID, err)
	}
	return nil
}

// ListSessions returns all sessions for the current tenant, ordered by most recent first.
// Uses a correlated subquery for last_entry to avoid N+1 query pattern.
func (s *SQLiteMemoryStore) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	tenantID := tenantFromCtx(ctx)
	query := `
		SELECT
			e.session_id, e.tenant_id, COUNT(*) as entry_count,
			MIN(e.created_at) as created_at, MAX(e.created_at) as updated_at,
			(SELECT e2.content FROM session_entries e2
			 WHERE e2.session_id = e.session_id AND e2.tenant_id = e.tenant_id
			 ORDER BY e2.created_at DESC LIMIT 1) as last_entry
		FROM session_entries e`
	args := []interface{}{}
	if tenantID != "" {
		query += " WHERE e.tenant_id = ?"
		args = append(args, tenantID)
	}
	query += " GROUP BY e.session_id, e.tenant_id ORDER BY updated_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var createdAtStr, updatedAtStr string
		var lastEntry sql.NullString
		if err := rows.Scan(&si.SessionID, &si.TenantID, &si.EntryCount, &createdAtStr, &updatedAtStr, &lastEntry); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		si.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		si.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
		if lastEntry.Valid {
			si.LastEntry = lastEntry.String
		}
		sessions = append(sessions, si)
	}

	return sessions, rows.Err()
}
