package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
)

// SQLiteMCPStore persists MCP server configurations to SQLite.
type SQLiteMCPStore struct {
	db *sql.DB
}

// NewSQLiteMCPStore creates a new store.
func NewSQLiteMCPStore(db *sql.DB) *SQLiteMCPStore {
	return &SQLiteMCPStore{db: db}
}

// Create inserts a new server config.
func (s *SQLiteMCPStore) Create(ctx context.Context, cfg mcpcore.ServerConfig) error {
	argsJSON, err := json.Marshal(cfg.Args)
	if err != nil {
		return fmt.Errorf("marshal args: %w", err)
	}
	envJSON, err := json.Marshal(cfg.Env)
	if err != nil {
		return fmt.Errorf("marshal env: %w", err)
	}
	authJSON, err := marshalAuth(cfg.Auth)
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}
	now := time.Now().Format(time.RFC3339Nano)
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mcp_servers (id, name, transport, command, args, url, env, auth, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cfg.ID, cfg.Name, cfg.Transport, cfg.Command, string(argsJSON), cfg.URL, string(envJSON), authJSON, enabled, now, now,
	)
	if err != nil {
		return fmt.Errorf("insert mcp server: %w", err)
	}
	return nil
}

// Get retrieves a server config by ID.
func (s *SQLiteMCPStore) Get(ctx context.Context, id string) (*mcpcore.ServerConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, transport, command, args, url, env, auth, enabled
		FROM mcp_servers WHERE id = ?`, id,
	)
	return s.scanConfig(row)
}

// List returns all server configs.
func (s *SQLiteMCPStore) List(ctx context.Context) ([]mcpcore.ServerConfig, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, transport, command, args, url, env, auth, enabled
		FROM mcp_servers ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var configs []mcpcore.ServerConfig
	for rows.Next() {
		cfg, err := s.scanConfigRow(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, *cfg)
	}
	return configs, rows.Err()
}

// Update modifies an existing server config.
func (s *SQLiteMCPStore) Update(ctx context.Context, cfg mcpcore.ServerConfig) error {
	argsJSON, err := json.Marshal(cfg.Args)
	if err != nil {
		return fmt.Errorf("marshal args: %w", err)
	}
	envJSON, err := json.Marshal(cfg.Env)
	if err != nil {
		return fmt.Errorf("marshal env: %w", err)
	}
	authJSON, err := marshalAuth(cfg.Auth)
	if err != nil {
		return fmt.Errorf("marshal auth: %w", err)
	}
	now := time.Now().Format(time.RFC3339Nano)
	enabled := 0
	if cfg.Enabled {
		enabled = 1
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE mcp_servers SET name=?, transport=?, command=?, args=?, url=?, env=?, auth=?, enabled=?, updated_at=?
		WHERE id=?`,
		cfg.Name, cfg.Transport, cfg.Command, string(argsJSON), cfg.URL, string(envJSON), authJSON, enabled, now, cfg.ID,
	)
	if err != nil {
		return fmt.Errorf("update mcp server: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mcp server %q not found", cfg.ID)
	}
	return nil
}

// Delete removes a server config.
func (s *SQLiteMCPStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mcp server %q not found", id)
	}
	return nil
}

func (s *SQLiteMCPStore) scanConfig(row *sql.Row) (*mcpcore.ServerConfig, error) {
	var cfg mcpcore.ServerConfig
	var argsJSON, envJSON, authJSON string
	var enabled int
	err := row.Scan(&cfg.ID, &cfg.Name, &cfg.Transport, &cfg.Command, &argsJSON, &cfg.URL, &envJSON, &authJSON, &enabled)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("mcp server not found")
	}
	if err != nil {
		return nil, err
	}
	return populateConfig(&cfg, argsJSON, envJSON, authJSON, enabled)
}

func (s *SQLiteMCPStore) scanConfigRow(rows *sql.Rows) (*mcpcore.ServerConfig, error) {
	var cfg mcpcore.ServerConfig
	var argsJSON, envJSON, authJSON string
	var enabled int
	err := rows.Scan(&cfg.ID, &cfg.Name, &cfg.Transport, &cfg.Command, &argsJSON, &cfg.URL, &envJSON, &authJSON, &enabled)
	if err != nil {
		return nil, err
	}
	return populateConfig(&cfg, argsJSON, envJSON, authJSON, enabled)
}

func populateConfig(cfg *mcpcore.ServerConfig, argsJSON, envJSON, authJSON string, enabled int) (*mcpcore.ServerConfig, error) {
	cfg.Enabled = enabled == 1
	if err := json.Unmarshal([]byte(argsJSON), &cfg.Args); err != nil {
		return nil, fmt.Errorf("unmarshal args for %q: %w", cfg.ID, err)
	}
	if err := json.Unmarshal([]byte(envJSON), &cfg.Env); err != nil {
		return nil, fmt.Errorf("unmarshal env for %q: %w", cfg.ID, err)
	}
	auth, err := unmarshalAuth(authJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal auth for %q: %w", cfg.ID, err)
	}
	cfg.Auth = auth
	return cfg, nil
}

func marshalAuth(auth *mcpcore.ServerAuth) (string, error) {
	if auth == nil {
		return "", nil
	}
	data, err := json.Marshal(auth)
	if err != nil {
		return "", fmt.Errorf("marshal auth: %w", err)
	}
	return string(data), nil
}

func unmarshalAuth(data string) (*mcpcore.ServerAuth, error) {
	if data == "" {
		return nil, nil
	}
	var auth mcpcore.ServerAuth
	if err := json.Unmarshal([]byte(data), &auth); err != nil {
		return nil, fmt.Errorf("unmarshal auth: %w", err)
	}
	if auth.Type == "" && auth.Token == "" && auth.Header == "" &&
		len(auth.Headers) == 0 && len(auth.EnvAuth) == 0 {
		return nil, nil
	}
	return &auth, nil
}
