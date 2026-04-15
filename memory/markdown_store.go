package memory

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
)

// MarkdownMemoryStore implements agent.ConversationStore using filesystem markdown files.
//
// Directory structure follows the 3+1 layered model (System → Tenant → User → Session):
//
//	{dataDir}/memory/{tenant_id}/users/{user_id}/sessions/{session_id}.md
//	{dataDir}/memory/{tenant_id}/users/{user_id}/sessions/{session_id}_summary.md
//
// Thread safety is provided via per-file RWMutex stored in a sync.Map.
type MarkdownMemoryStore struct {
	dataDir string
	locks   sync.Map // map[string]*sync.RWMutex
}

// NewMarkdownMemoryStore creates a new markdown-backed conversation store.
func NewMarkdownMemoryStore(dataDir string) (*MarkdownMemoryStore, error) {
	s := &MarkdownMemoryStore{dataDir: dataDir}
	// Ensure base directory exists
	if err := os.MkdirAll(filepath.Join(dataDir, "memory"), 0755); err != nil {
		return nil, fmt.Errorf("memory: failed to create data directory: %w", err)
	}
	return s, nil
}

// AppendMessage adds a message to a session's conversation file.
func (s *MarkdownMemoryStore) AppendMessage(ctx context.Context, msg agent.SessionMessage) error {
	if err := validateID(msg.TenantID, "tenant"); err != nil {
		return err
	}
	if err := validateID(msg.UserID, "user"); err != nil {
		return err
	}
	if err := validateID(msg.SessionID, "session"); err != nil {
		return err
	}

	path := s.sessionPath(msg.TenantID, msg.UserID, msg.SessionID)
	lock := s.getLock(path)
	lock.Lock()
	defer lock.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("memory: failed to create session directory: %w", err)
	}

	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		meta := map[string]string{
			"session_id":    msg.SessionID,
			"tenant_id":     msg.TenantID,
			"user_id":       msg.UserID,
			"created_at":    msg.Timestamp,
			"updated_at":    msg.Timestamp,
			"message_count": "1",
		}
		content := FormatFrontmatter(meta) + FormatMessageBlock(msg.Role, msg.Content, msg.Timestamp)
		return os.WriteFile(path, []byte(content), 0600)
	}
	if err != nil {
		return fmt.Errorf("memory: failed to stat session file: %w", err)
	}

	// Read-modify-write for existing file
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("memory: failed to open session file: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("memory: failed to read session file: %w", err)
	}

	meta, body, parseErr := ParseFrontmatter(data)
	if parseErr != nil {
		slog.WarnContext(ctx, "memory: corrupt frontmatter, recreating",
			"path", path, "error", parseErr)
		meta = map[string]string{
			"session_id": msg.SessionID,
			"tenant_id":  msg.TenantID,
			"user_id":    msg.UserID,
		}
		body = nil
	}

	meta["updated_at"] = msg.Timestamp
	meta["message_count"] = incrementCount(meta["message_count"])

	newContent := FormatFrontmatter(meta) + string(body) + FormatMessageBlock(msg.Role, msg.Content, msg.Timestamp)

	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("memory: failed to truncate: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("memory: failed to seek: %w", err)
	}
	if _, err := f.WriteString(newContent); err != nil {
		return fmt.Errorf("memory: failed to write: %w", err)
	}

	return nil
}

// GetHistory retrieves messages for a session in chronological order.
func (s *MarkdownMemoryStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]agent.Message, error) {
	if err := validateID(tenantID, "tenant"); err != nil {
		return nil, err
	}
	if err := validateID(userID, "user"); err != nil {
		return nil, err
	}
	if err := validateID(sessionID, "session"); err != nil {
		return nil, err
	}

	path := s.sessionPath(tenantID, userID, sessionID)
	lock := s.getLock(path)
	lock.RLock()
	defer lock.RUnlock()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []agent.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: failed to read session file: %w", err)
	}

	_, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("memory: failed to parse session file: %w", err)
	}

	messages := ParseMessageBlocks(body)
	if maxMessages > 0 && len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	return messages, nil
}

// GetSummary retrieves the current summary for a session.
func (s *MarkdownMemoryStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	if err := validateID(tenantID, "tenant"); err != nil {
		return "", err
	}
	if err := validateID(userID, "user"); err != nil {
		return "", err
	}
	if err := validateID(sessionID, "session"); err != nil {
		return "", err
	}

	path := s.summaryPath(tenantID, userID, sessionID)
	lock := s.getLock(path)
	lock.RLock()
	defer lock.RUnlock()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("memory: failed to read summary file: %w", err)
	}

	_, body, err := ParseFrontmatter(data)
	if err != nil {
		return "", fmt.Errorf("memory: failed to parse summary file: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

// StoreSummary persists a summary for a session.
func (s *MarkdownMemoryStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	if err := validateID(tenantID, "tenant"); err != nil {
		return err
	}
	if err := validateID(userID, "user"); err != nil {
		return err
	}
	if err := validateID(sessionID, "session"); err != nil {
		return err
	}

	path := s.summaryPath(tenantID, userID, sessionID)
	lock := s.getLock(path)
	lock.Lock()
	defer lock.Unlock()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("memory: failed to create summary directory: %w", err)
	}

	// Read actual message count from session file for accurate metadata
	msgCount := 0
	sessionFilePath := s.sessionPath(tenantID, userID, sessionID)
	if sd, err := os.ReadFile(sessionFilePath); err == nil {
		if m, _, _ := ParseFrontmatter(sd); m != nil {
			_, _ = fmt.Sscanf(m["message_count"], "%d", &msgCount)
		}
	}

	meta := FormatSummaryFrontmatter(sessionID, msgCount)
	meta["created_at"] = time.Now().UTC().Format("2006-01-02T15:04:05.999999999Z")
	content := FormatFrontmatter(meta) + summary + "\n"

	return os.WriteFile(path, []byte(content), 0600)
}

// ClearSession removes all messages and summary for a session.
func (s *MarkdownMemoryStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	if err := validateID(tenantID, "tenant"); err != nil {
		return err
	}
	if err := validateID(userID, "user"); err != nil {
		return err
	}
	if err := validateID(sessionID, "session"); err != nil {
		return err
	}

	sessionPath := s.sessionPath(tenantID, userID, sessionID)
	summaryPath := s.summaryPath(tenantID, userID, sessionID)

	// Acquire both locks to prevent races
	sessionLock := s.getLock(sessionPath)
	summaryLock := s.getLock(summaryPath)
	sessionLock.Lock()
	defer sessionLock.Unlock()
	summaryLock.Lock()
	defer summaryLock.Unlock()

	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory: failed to remove session file: %w", err)
	}
	if err := os.Remove(summaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory: failed to remove summary file: %w", err)
	}
	return nil
}

// DataDir returns the configured data directory path.
func (s *MarkdownMemoryStore) DataDir() string {
	return s.dataDir
}

// --- internal helpers (3+1 layered paths) ---

func (s *MarkdownMemoryStore) sessionPath(tenantID, userID, sessionID string) string {
	return filepath.Join(s.dataDir, "memory",
		sanitizePath(tenantID), "users", sanitizePath(userID),
		"sessions", sanitizePath(sessionID)+".md")
}

func (s *MarkdownMemoryStore) summaryPath(tenantID, userID, sessionID string) string {
	return filepath.Join(s.dataDir, "memory",
		sanitizePath(tenantID), "users", sanitizePath(userID),
		"sessions", sanitizePath(sessionID)+"_summary.md")
}

func (s *MarkdownMemoryStore) getLock(path string) *sync.RWMutex {
	val, _ := s.locks.LoadOrStore(path, &sync.RWMutex{})
	return val.(*sync.RWMutex)
}

// sanitizePath prevents path traversal by replacing dangerous characters.
func sanitizePath(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	id = strings.ReplaceAll(id, "..", "_")
	id = strings.TrimSpace(id)
	if id == "" {
		return "_"
	}
	return id
}

// validateID checks that an ID is valid for use in file paths.
func validateID(id, kind string) error {
	if id == "" {
		return fmt.Errorf("memory: %s ID is empty", kind)
	}
	if strings.ContainsRune(id, '\x00') {
		return fmt.Errorf("memory: %s ID contains null byte", kind)
	}
	if strings.Contains(id, "..") {
		return fmt.Errorf("memory: %s ID contains path traversal", kind)
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("memory: %s ID contains path separators", kind)
	}
	return nil
}

// incrementCount parses and increments a count string.
func incrementCount(s string) string {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return fmt.Sprintf("%d", n+1)
}
