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

// ShortTermStore provides short-term memory operations.
type ShortTermStore interface {
	Store(ctx context.Context, entry Entry) error
	Retrieve(ctx context.Context, id string) (*Entry, error)
	ListBySession(ctx context.Context, sessionID string) ([]Entry, error)
	Delete(ctx context.Context, id string) error
	ClearSession(ctx context.Context, sessionID string) error
}
