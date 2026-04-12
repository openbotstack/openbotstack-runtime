package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// SQLiteMemoryStore implements ShortTermStore using SQLite.
type SQLiteMemoryStore struct {
	db *sql.DB
}

// NewSQLiteMemoryStore creates a new SQLite-backed memory store.
func NewSQLiteMemoryStore(db *sql.DB) *SQLiteMemoryStore {
	return &SQLiteMemoryStore{db: db}
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

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO session_entries (id, session_id, content, tags, created_at, ttl)
		VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.SessionID, entry.Content, string(tagsJSON),
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

	err := s.db.QueryRowContext(ctx, `
		SELECT id, session_id, content, tags, created_at, ttl
		FROM session_entries WHERE id = ?`, id,
	).Scan(&e.ID, &e.SessionID, &e.Content, &tagsJSON, &createdAtStr, &ttlSeconds)

	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("retrieve entry %s: %w", id, err)
	}

	if ttlSeconds > 0 {
		createdAt, _ := time.Parse(time.RFC3339Nano, createdAtStr)
		if time.Since(createdAt) > time.Duration(ttlSeconds)*time.Second {
			s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE id = ?", id)
			return nil, ErrNotFound
		}
	}

	json.Unmarshal([]byte(tagsJSON), &e.Tags)
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	if ttlSeconds > 0 {
		e.TTL = time.Duration(ttlSeconds) * time.Second
	}
	return &e, nil
}

// ListBySession returns all non-expired entries for a session, ordered by created_at.
func (s *SQLiteMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, content, tags, created_at, ttl
		FROM session_entries WHERE session_id = ?
		ORDER BY created_at`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var entries []Entry
	var expiredIDs []string

	for rows.Next() {
		var e Entry
		var tagsJSON, createdAtStr string
		var ttlSeconds int64
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Content, &tagsJSON,
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

		json.Unmarshal([]byte(tagsJSON), &e.Tags)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		entries = append(entries, e)
	}

	for _, id := range expiredIDs {
		s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE id = ?", id)
	}

	return entries, rows.Err()
}

// Delete removes a memory entry by ID, returning ErrNotFound if it doesn't exist.
func (s *SQLiteMemoryStore) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE id = ?", id)
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
	_, err := s.db.ExecContext(ctx, "DELETE FROM session_entries WHERE session_id = ?", sessionID)
	if err != nil {
		return fmt.Errorf("clear session %s: %w", sessionID, err)
	}
	return nil
}
