package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// VectorDocument represents a document stored in the vector database.
type VectorDocument struct {
	ID        string
	Content   string
	Embedding []float32
	TenantID  string
	UserID    string
	SessionID string
	Role      string
	CreatedAt time.Time
}

// SearchOptions controls vector search behavior.
type SearchOptions struct {
	TenantID  string // required: tenant scope
	UserID    string // optional: restrict to user (empty = all users)
	SessionID string // optional: restrict to session (empty = all sessions)
	Limit     int    // max results (default: 10)
}

// DeleteFilter specifies which documents to delete.
type DeleteFilter struct {
	TenantID  string
	SessionID string // optional
	Before    time.Time
}

// SearchResult is a vector document with a similarity score.
type SearchResult struct {
	VectorDocument
	Score float32 // cosine similarity (1.0 = identical)
}

// VectorStore provides vector storage and similarity search.
// Implementations: pgvector (production), future: sqlite-vec, milvus.
type VectorStore interface {
	// Store saves a document with its embedding.
	Store(ctx context.Context, doc VectorDocument) error

	// Search returns the top-K most similar documents by cosine distance.
	Search(ctx context.Context, query []float32, opts SearchOptions) ([]SearchResult, error)

	// Delete removes documents matching the filter.
	Delete(ctx context.Context, filter DeleteFilter) error

	// Close releases resources.
	Close() error
}

// PgVectorStore implements VectorStore using PostgreSQL + pgvector.
type PgVectorStore struct {
	pool       *pgxpool.Pool
	dimensions int
}

// NewPgVectorStore creates a new pgvector-backed vector store.
// The pool must be connected to a database with the pgvector extension enabled.
func NewPgVectorStore(pool *pgxpool.Pool, dimensions int) *PgVectorStore {
	return &PgVectorStore{
		pool:       pool,
		dimensions: dimensions,
	}
}

// Migrate creates the necessary tables and indexes.
func (s *PgVectorStore) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS memory_vectors (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			embedding vector(%d),
			tenant_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, s.dimensions),
		`CREATE INDEX IF NOT EXISTS idx_memory_vectors_tenant ON memory_vectors(tenant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_vectors_tenant_user ON memory_vectors(tenant_id, user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_vectors_session ON memory_vectors(tenant_id, session_id)`,
	}

	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("vector migrate: %w", err)
		}
	}

	// Create HNSW index for fast cosine similarity search (idempotent)
	hnswStmt := `CREATE INDEX IF NOT EXISTS idx_memory_vectors_embedding ON memory_vectors USING hnsw (embedding vector_cosine_ops)`
	if _, err := s.pool.Exec(ctx, hnswStmt); err != nil {
		// HNSW index creation can be slow on large tables; log but don't fail
		slog.WarnContext(ctx, "vector store: HNSW index creation deferred (may need manual creation)", "error", err)
	}

	return nil
}

// Store saves a document with its embedding.
func (s *PgVectorStore) Store(ctx context.Context, doc VectorDocument) error {
	embedStr := formatVector(doc.Embedding)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO memory_vectors (id, content, embedding, tenant_id, user_id, session_id, role, created_at)
		 VALUES ($1, $2, $3::vector, $4, $5, $6, $7, $8)
		 ON CONFLICT (id) DO UPDATE SET content = $2, embedding = $3::vector`,
		doc.ID, doc.Content, embedStr,
		doc.TenantID, doc.UserID, doc.SessionID, doc.Role, doc.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("vector store: %w", err)
	}
	return nil
}

// Search returns the top-K most similar documents by cosine distance.
func (s *PgVectorStore) Search(ctx context.Context, query []float32, opts SearchOptions) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	queryStr := formatVector(query)

	var sb strings.Builder
	sb.WriteString(`SELECT id, content, tenant_id, user_id, session_id, role, created_at,
		1 - (embedding <=> $1::vector) AS score
		FROM memory_vectors WHERE tenant_id = $2`)
	args := []any{queryStr, opts.TenantID}
	argIdx := 3

	if opts.UserID != "" {
		fmt.Fprintf(&sb, ` AND user_id = $%d`, argIdx)
		args = append(args, opts.UserID)
		argIdx++
	}
	if opts.SessionID != "" {
		fmt.Fprintf(&sb, ` AND session_id = $%d`, argIdx)
		args = append(args, opts.SessionID)
		argIdx++
	}

	fmt.Fprintf(&sb, ` ORDER BY embedding <=> $1::vector LIMIT $%d`, argIdx)
	args = append(args, opts.Limit)

	rows, err := s.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}
	defer rows.Close() //nolint:errcheck // pgx.Rows.Close() has no return value

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ID, &r.Content, &r.TenantID, &r.UserID, &r.SessionID, &r.Role, &r.CreatedAt, &r.Score); err != nil {
			return nil, fmt.Errorf("vector search scan: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// Delete removes documents matching the filter.
func (s *PgVectorStore) Delete(ctx context.Context, filter DeleteFilter) error {
	if filter.SessionID != "" {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM memory_vectors WHERE tenant_id = $1 AND session_id = $2`,
			filter.TenantID, filter.SessionID)
		return err
	}
	if !filter.Before.IsZero() {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM memory_vectors WHERE tenant_id = $1 AND created_at < $2`,
			filter.TenantID, filter.Before)
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM memory_vectors WHERE tenant_id = $1`, filter.TenantID)
	return err
}

// Close releases resources. Does NOT close the underlying pool — caller owns it.
func (s *PgVectorStore) Close() error {
	// Pool lifecycle is managed by the caller (main.go).
	return nil
}

// formatVector converts a []float32 to pgvector format: "[0.1,0.2,0.3]"
// Uses strconv.FormatFloat with full float32 precision to avoid silent truncation.
func formatVector(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}
