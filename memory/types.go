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

// ShortTermStore provides short-term memory operations.
type ShortTermStore interface {
	Store(ctx context.Context, entry Entry) error
	Retrieve(ctx context.Context, id string) (*Entry, error)
	ListBySession(ctx context.Context, sessionID string) ([]Entry, error)
	Delete(ctx context.Context, id string) error
	ClearSession(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context) ([]SessionInfo, error)
}
