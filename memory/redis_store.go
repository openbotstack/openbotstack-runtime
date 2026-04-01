package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
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

// RedisMemoryStore implements ShortTermStore using Redis.
type RedisMemoryStore struct {
	client *redis.Client
	prefix string
}

// NewRedisMemoryStore creates a new Redis memory store.
func NewRedisMemoryStore(client *redis.Client) *RedisMemoryStore {
	return &RedisMemoryStore{
		client: client,
		prefix: "memory:",
	}
}

// entryKey returns the Redis key for an entry.
func (s *RedisMemoryStore) entryKey(id string) string {
	return s.prefix + "entry:" + id
}

// sessionKey returns the Redis key for a session's entry list.
func (s *RedisMemoryStore) sessionKey(sessionID string) string {
	return s.prefix + "session:" + sessionID
}

// Store saves a memory entry.
func (s *RedisMemoryStore) Store(ctx context.Context, entry Entry) error {
	entry.CreatedAt = time.Now()
	
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	pipe := s.client.Pipeline()
	
	// Store the entry
	pipe.Set(ctx, s.entryKey(entry.ID), data, entry.TTL)
	
	// Add to session index
	pipe.SAdd(ctx, s.sessionKey(entry.SessionID), entry.ID)
	if entry.TTL > 0 {
		// Set TTL on the session index as well so it cleans up, though it might extend if new entries are added
		// We set it to the max TTL we've seen or something.
		pipe.Expire(ctx, s.sessionKey(entry.SessionID), max(entry.TTL, 24*time.Hour))
	}
	
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to store entry: %w", err)
	}
	
	return nil
}

// Retrieve gets a memory entry by ID.
func (s *RedisMemoryStore) Retrieve(ctx context.Context, id string) (*Entry, error) {
	data, err := s.client.Get(ctx, s.entryKey(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to retrieve entry: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entry: %w", err)
	}

	return &entry, nil
}

// ListBySession returns all entries for a session.
func (s *RedisMemoryStore) ListBySession(ctx context.Context, sessionID string) ([]Entry, error) {
	// Get all entry IDs for the session
	ids, err := s.client.SMembers(ctx, s.sessionKey(sessionID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get session members: %w", err)
	}

	if len(ids) == 0 {
		return []Entry{}, nil
	}

	// Fetch all entries
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.entryKey(id)
	}

	// Use MGet to fetch all at once
	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to mget entries: %w", err)
	}

	var results []Entry
	for _, val := range values {
		if val == nil {
			continue // Expired or deleted
		}
		
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		
		var entry Entry
		if err := json.Unmarshal([]byte(strVal), &entry); err == nil {
			results = append(results, entry)
		}
	}

	return results, nil
}

// Delete removes a memory entry.
func (s *RedisMemoryStore) Delete(ctx context.Context, id string) error {
	// To delete it properly we also need to remove it from the session index.
	// We first need to get the entry to know its session ID.
	entry, err := s.Retrieve(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil // idempotent
		}
		return err
	}

	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.entryKey(id))
	pipe.SRem(ctx, s.sessionKey(entry.SessionID), id)
	
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete entry: %w", err)
	}
	
	return nil
}

// ClearSession removes all entries for a session.
func (s *RedisMemoryStore) ClearSession(ctx context.Context, sessionID string) error {
	sessionKey := s.sessionKey(sessionID)
	
	// Get all entry IDs
	ids, err := s.client.SMembers(ctx, sessionKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get session members: %w", err)
	}

	if len(ids) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	for _, id := range ids {
		pipe.Del(ctx, s.entryKey(id))
	}
	pipe.Del(ctx, sessionKey)
	
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear session: %w", err)
	}

	return nil
}
