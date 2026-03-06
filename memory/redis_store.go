// Package memory implements short-term memory storage for sessions.
package memory

import (
	"context"
	"errors"
	"sync"
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

// RedisMemoryStore implements ShortTermStore (in-memory stub for now).
type RedisMemoryStore struct {
	mu       sync.RWMutex
	entries  map[string]*Entry
	sessions map[string][]string // sessionID -> []entryIDs
}

// NewRedisMemoryStore creates a new memory store.
func NewRedisMemoryStore() *RedisMemoryStore {
	return &RedisMemoryStore{
		entries:  make(map[string]*Entry),
		sessions: make(map[string][]string),
	}
}

// Store saves a memory entry.
func (s *RedisMemoryStore) Store(ctx context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry.CreatedAt = time.Now()
	s.entries[entry.ID] = &entry

	// Index by session
	s.sessions[entry.SessionID] = append(s.sessions[entry.SessionID], entry.ID)

	return nil
}

// Retrieve gets a memory entry by ID.
func (s *RedisMemoryStore) Retrieve(ctx context.Context, id string) (*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.entries[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Check TTL expiration
	if entry.TTL > 0 && time.Since(entry.CreatedAt) > entry.TTL {
		return nil, ErrNotFound
	}

	return entry, nil
}

// ListBySession returns all entries for a session.
func (s *RedisMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids, exists := s.sessions[sessionID]
	if !exists {
		return []Entry{}, nil
	}

	result := make([]Entry, 0, len(ids))
	for _, id := range ids {
		if entry, ok := s.entries[id]; ok {
			result = append(result, *entry)
		}
	}

	return result, nil
}

// Delete removes a memory entry.
func (s *RedisMemoryStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.entries[id]
	if !exists {
		return nil // idempotent
	}

	// Remove from session index
	sessionID := entry.SessionID
	ids := s.sessions[sessionID]
	for i, eid := range ids {
		if eid == id {
			s.sessions[sessionID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}

	delete(s.entries, id)
	return nil
}

// ClearSession removes all entries for a session.
func (s *RedisMemoryStore) ClearSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ids, exists := s.sessions[sessionID]
	if !exists {
		return nil
	}

	for _, id := range ids {
		delete(s.entries, id)
	}
	delete(s.sessions, sessionID)

	return nil
}
