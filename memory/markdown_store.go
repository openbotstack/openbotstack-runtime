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

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// MarkdownMemoryStore implements coreagent.ConversationStore using filesystem markdown files.
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
func (s *MarkdownMemoryStore) AppendMessage(ctx context.Context, msg coreagent.SessionMessage) error {
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
		content := FormatFrontmatter(meta) + FormatMessageBlock(msg.Role, msg.Content, msg.Timestamp, msg.ExecutionID)
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

	newContent := FormatFrontmatter(meta) + string(body) + FormatMessageBlock(msg.Role, msg.Content, msg.Timestamp, msg.ExecutionID)

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
func (s *MarkdownMemoryStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]aitypes.Message, error) {
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
		return []aitypes.Message{}, nil
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

// ListSessions scans the filesystem for session files and returns summaries.
// It reads the frontmatter from each .md file in the sessions directories.
func (s *MarkdownMemoryStore) ListSessions(ctx context.Context, tenantID string) ([]SessionInfo, error) {
	memoryDir := filepath.Join(s.dataDir, "memory")

	// If tenantID specified, scan only that tenant's directory
	var searchDirs []string
	if tenantID != "" {
		searchDirs = []string{filepath.Join(memoryDir, sanitizePath(tenantID))}
	} else {
		// Scan all tenants
		entries, err := os.ReadDir(memoryDir)
		if err != nil {
			if os.IsNotExist(err) {
				return []SessionInfo{}, nil
			}
			return nil, fmt.Errorf("memory: failed to read memory dir: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				searchDirs = append(searchDirs, filepath.Join(memoryDir, e.Name()))
			}
		}
	}

	var sessions []SessionInfo
	for _, tenantDir := range searchDirs {
		// Walk: tenantDir/users/*/sessions/*.md (excluding *_summary.md)
		usersDir := filepath.Join(tenantDir, "users")
		userEntries, err := os.ReadDir(usersDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		for _, ue := range userEntries {
			if !ue.IsDir() {
				continue
			}
			sessionsDir := filepath.Join(usersDir, ue.Name(), "sessions")
			sessionFiles, err := os.ReadDir(sessionsDir)
			if err != nil {
				continue
			}
			for _, sf := range sessionFiles {
				if sf.IsDir() || !strings.HasSuffix(sf.Name(), ".md") || strings.HasSuffix(sf.Name(), "_summary.md") {
					continue
				}

				filePath := filepath.Join(sessionsDir, sf.Name())
				lock := s.getLock(filePath)
				lock.RLock()
				data, err := os.ReadFile(filePath)
				lock.RUnlock()
				if err != nil {
					continue
				}

				meta, _, err := ParseFrontmatter(data)
				if err != nil {
					continue
				}

				sessionID := meta["session_id"]
				tID := meta["tenant_id"]
				if sessionID == "" {
					// Derive from filename
					sessionID = strings.TrimSuffix(sf.Name(), ".md")
				}

				createdAt, _ := time.Parse(time.RFC3339Nano, meta["created_at"])
				updatedAt, _ := time.Parse(time.RFC3339Nano, meta["updated_at"])
				var entryCount int
				_, _ = fmt.Sscanf(meta["message_count"], "%d", &entryCount)

				si := SessionInfo{
					SessionID:  sessionID,
					TenantID:   tID,
					EntryCount: entryCount,
					CreatedAt:  createdAt,
					UpdatedAt:  updatedAt,
				}

				sessions = append(sessions, si)
			}
		}
	}

	// Sort by updated_at descending
	for i := 1; i < len(sessions); i++ {
		for j := i; j > 0 && sessions[j].UpdatedAt.After(sessions[j-1].UpdatedAt); j-- {
			sessions[j], sessions[j-1] = sessions[j-1], sessions[j]
		}
	}

	return sessions, nil
}

// DeleteSession removes a session by scanning for its file.
func (s *MarkdownMemoryStore) DeleteSessionBySessionID(ctx context.Context, sessionID string) error {
	memoryDir := filepath.Join(s.dataDir, "memory")

	// Walk all tenant/user directories to find the session file
	tenantEntries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("memory: failed to read memory dir: %w", err)
	}

	for _, te := range tenantEntries {
		if !te.IsDir() {
			continue
		}
		usersDir := filepath.Join(memoryDir, te.Name(), "users")
		userEntries, err := os.ReadDir(usersDir)
		if err != nil {
			continue
		}
		for _, ue := range userEntries {
			if !ue.IsDir() {
				continue
			}
			sessionFile := filepath.Join(usersDir, ue.Name(), "sessions", sanitizePath(sessionID)+".md")
			summaryFile := filepath.Join(usersDir, ue.Name(), "sessions", sanitizePath(sessionID)+"_summary.md")
			// Try to remove both files
			_ = os.Remove(sessionFile)
			_ = os.Remove(summaryFile)
		}
	}
	return nil
}

// GetHistoryBySessionID retrieves messages for a session by scanning the filesystem
// to find the session file. Unlike GetHistory which requires tenantID and userID,
// this method locates the file by session ID alone.
func (s *MarkdownMemoryStore) GetHistoryBySessionID(ctx context.Context, sessionID string) ([]aitypes.Message, error) {
	memoryDir := filepath.Join(s.dataDir, "memory")

	tenantEntries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []aitypes.Message{}, nil
		}
		return nil, fmt.Errorf("memory: failed to read memory dir: %w", err)
	}

	targetName := sanitizePath(sessionID) + ".md"
	for _, te := range tenantEntries {
		if !te.IsDir() {
			continue
		}
		usersDir := filepath.Join(memoryDir, te.Name(), "users")
		userEntries, err := os.ReadDir(usersDir)
		if err != nil {
			continue
		}
		for _, ue := range userEntries {
			if !ue.IsDir() {
				continue
			}
			filePath := filepath.Join(usersDir, ue.Name(), "sessions", targetName)
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			_, body, parseErr := ParseFrontmatter(data)
			if parseErr != nil {
				continue
			}
			return ParseMessageBlocks(body), nil
		}
	}

	return []aitypes.Message{}, nil
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
