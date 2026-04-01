package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MemoryManager provides methods for session tracking and long-term semantic retrieval.
type MemoryManager interface {
	AddSessionMemory(ctx context.Context, sessionID, text string) error
	GetSessionContext(ctx context.Context, sessionID string) ([]string, error)
	
	AddPersistentMemory(ctx context.Context, userID, text string, embedding []float32) error
	SemanticSearch(ctx context.Context, userID string, queryEmbedding []float32, limit int) ([]string, error)
}

// PostgresMemoryManager implements MemoryManager using PostgreSQL and pgvector.
type PostgresMemoryManager struct {
	pool *pgxpool.Pool
}

// NewPostgresMemoryManager creates a new Postgres-based vector store.
func NewPostgresMemoryManager(pool *pgxpool.Pool) *PostgresMemoryManager {
	return &PostgresMemoryManager{
		pool: pool,
	}
}

// AddSessionMemory saves a piece of conversation to the short-term session.
func (m *PostgresMemoryManager) AddSessionMemory(ctx context.Context, sessionID, text string) error {
	query := `INSERT INTO session_memory (session_id, content, created_at) VALUES ($1, $2, NOW())`
	_, err := m.pool.Exec(ctx, query, sessionID, text)
	return err
}

// GetSessionContext retrieves recent conversation context for a session.
func (m *PostgresMemoryManager) GetSessionContext(ctx context.Context, sessionID string) ([]string, error) {
	query := `
		SELECT content 
		FROM session_memory 
		WHERE session_id = $1 
		ORDER BY created_at DESC 
		LIMIT 50
	`
	rows, err := m.pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query session memory: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		// Prepend to maintain chronological order
		results = append([]string{content}, results...)
	}
	return results, rows.Err()
}

// AddPersistentMemory stores data with an embedding for semantic search.
func (m *PostgresMemoryManager) AddPersistentMemory(ctx context.Context, userID, text string, embedding []float32) error {
	// Format the embedding array for pgvector
	strValues := make([]string, len(embedding))
	for i, v := range embedding {
		strValues[i] = fmt.Sprintf("%f", v)
	}
	embedStr := "[" + strings.Join(strValues, ",") + "]"

	query := `INSERT INTO persistent_memory (user_id, content, embedding) VALUES ($1, $2, $3::vector)`
	_, err := m.pool.Exec(ctx, query, userID, text, embedStr)
	return err
}

// SemanticSearch performs a nearest-neighbor search using pgvector.
func (m *PostgresMemoryManager) SemanticSearch(ctx context.Context, userID string, queryEmbedding []float32, limit int) ([]string, error) {
	strValues := make([]string, len(queryEmbedding))
	for i, v := range queryEmbedding {
		strValues[i] = fmt.Sprintf("%f", v)
	}
	embedStr := "[" + strings.Join(strValues, ",") + "]"

	// Uses L2 distance (<->) for nearest neighbors
	query := `
		SELECT content 
		FROM persistent_memory 
		WHERE user_id = $1 
		ORDER BY embedding <-> $2::vector 
		LIMIT $3
	`
	rows, err := m.pool.Query(ctx, query, userID, embedStr, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search failed: %w", err)
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		results = append(results, content)
	}
	return results, rows.Err()
}
