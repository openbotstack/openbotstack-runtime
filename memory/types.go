package memory

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a memory entry doesn't exist.
	ErrNotFound = errors.New("memory: entry not found")
)

// Entry represents a short-term memory item.
type Entry struct {
	ID        string
	SessionID string
	Content   string
	Tags      []string
	CreatedAt time.Time
	TTL       time.Duration
}

// SessionInfo represents a summary of a session.
type SessionInfo struct {
	SessionID string
	TenantID  string
	LastEntry string    // content of the most recent entry
	EntryCount int
	CreatedAt time.Time // timestamp of the first entry
	UpdatedAt time.Time // timestamp of the last entry
}

// ShortTermStore provides TTL-based short-term memory operations.
// Session metadata listing is handled by SessionStateStore.
type ShortTermStore interface {
	Store(ctx context.Context, entry Entry) error
	Retrieve(ctx context.Context, id string) (*Entry, error)
	ListBySession(ctx context.Context, sessionID string) ([]Entry, error)
	Delete(ctx context.Context, id string) error
	ClearSession(ctx context.Context, sessionID string) error
}

// SessionMeta contains the data needed to create or update a session metadata record.
type SessionMeta struct {
	SessionID          string
	TenantID           string
	UserID             string
	MessageCount       int
	LastMessagePreview string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// SessionStateStore manages session metadata in SQLite for fast listing/filtering.
// Conversation content remains in Markdown files (agent.ConversationStore).
type SessionStateStore interface {
	// UpsertSession creates or updates session metadata.
	UpsertSession(ctx context.Context, meta SessionMeta) error
	// ListSessions returns all sessions for the current tenant, ordered by most recent first.
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	// GetSession retrieves metadata for a specific session.
	GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)
	// DeleteSession removes session metadata.
	DeleteSession(ctx context.Context, sessionID string) error
}
