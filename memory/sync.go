package memory

import (
	"context"
	"log/slog"
	"time"
)

// SyncMarkdownToSQLite scans existing Markdown session files and imports their
// metadata into the SQLite sessions table. This is a one-time migration for
// existing deployments. After sync, the DualWriteConversationStore keeps both
// stores in sync automatically.
func SyncMarkdownToSQLite(ctx context.Context, mdStore *MarkdownMemoryStore, sessionState SessionStateStore) error {
	sessions, err := mdStore.ListSessions(ctx, "")
	if err != nil {
		return err
	}

	synced := 0
	for _, si := range sessions {
		preview := si.LastEntry
		if len(preview) > 200 {
			preview = preview[:200]
		}
		meta := SessionMeta{
			SessionID:          si.SessionID,
			TenantID:           si.TenantID,
			MessageCount:       si.EntryCount,
			LastMessagePreview: preview,
			CreatedAt:          si.CreatedAt,
			UpdatedAt:          si.UpdatedAt,
		}
		if err := sessionState.UpsertSession(ctx, meta); err != nil {
			slog.WarnContext(ctx, "sync: failed to upsert session",
				"session_id", si.SessionID, "error", err)
			continue
		}
		synced++
	}

	slog.InfoContext(ctx, "markdown-to-sqlite session sync complete",
		"total", len(sessions), "synced", synced)
	return nil
}

// StartReconciliation launches a background goroutine that periodically
// re-syncs Markdown sessions to SQLite, repairing any drift caused by
// transient SQLite failures in the dual-write path.
// Returns a stop function to shut down the goroutine.
func StartReconciliation(ctx context.Context, mdStore *MarkdownMemoryStore, sessionState SessionStateStore, interval time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := SyncMarkdownToSQLite(ctx, mdStore, sessionState); err != nil {
					slog.WarnContext(ctx, "periodic reconciliation failed", "error", err)
				}
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
