package memory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// DualWriteConversationStore decorates a ConversationStore to also update
// SQLite session metadata on every AppendMessage. Markdown remains the
// canonical content store; SQLite holds metadata for fast listing/filtering.
type DualWriteConversationStore struct {
	inner        coreagent.ConversationStore
	sessionState SessionStateStore
}

// NewDualWriteConversationStore creates a dual-write decorator.
func NewDualWriteConversationStore(inner coreagent.ConversationStore, sessionState SessionStateStore) *DualWriteConversationStore {
	return &DualWriteConversationStore{inner: inner, sessionState: sessionState}
}

// AppendMessage writes to Markdown (primary) then updates SQLite metadata (best-effort).
func (d *DualWriteConversationStore) AppendMessage(ctx context.Context, msg coreagent.SessionMessage) error {
	if err := d.inner.AppendMessage(ctx, msg); err != nil {
		return err
	}

	// Update SQLite metadata — best-effort
	ts, _ := time.Parse(time.RFC3339Nano, msg.Timestamp)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	preview := msg.Content
	if len(preview) > 200 {
		preview = preview[:200]
	}

	meta := SessionMeta{
		SessionID:          msg.SessionID,
		TenantID:           msg.TenantID,
		UserID:             msg.UserID,
		MessageCount:       1,
		LastMessagePreview: preview,
		CreatedAt:          ts,
		UpdatedAt:          ts,
	}

	if err := d.sessionState.UpsertSession(ctx, meta); err != nil {
		slog.WarnContext(ctx, "dual-write: sqlite metadata update failed",
			"session_id", msg.SessionID, "error", err)
	}

	return nil
}

// GetHistory delegates to the inner Markdown store.
func (d *DualWriteConversationStore) GetHistory(ctx context.Context, tenantID, userID, sessionID string, maxMessages int) ([]aitypes.Message, error) {
	return d.inner.GetHistory(ctx, tenantID, userID, sessionID, maxMessages)
}

// GetSummary delegates to the inner Markdown store.
func (d *DualWriteConversationStore) GetSummary(ctx context.Context, tenantID, userID, sessionID string) (string, error) {
	return d.inner.GetSummary(ctx, tenantID, userID, sessionID)
}

// StoreSummary delegates to the inner Markdown store.
func (d *DualWriteConversationStore) StoreSummary(ctx context.Context, tenantID, userID, sessionID, summary string) error {
	return d.inner.StoreSummary(ctx, tenantID, userID, sessionID, summary)
}

// ClearSession removes from both Markdown and SQLite.
func (d *DualWriteConversationStore) ClearSession(ctx context.Context, tenantID, userID, sessionID string) error {
	if err := d.inner.ClearSession(ctx, tenantID, userID, sessionID); err != nil {
		return fmt.Errorf("clear markdown: %w", err)
	}

	if err := d.sessionState.DeleteSession(ctx, sessionID); err != nil {
		slog.WarnContext(ctx, "dual-write: sqlite delete failed on clear",
			"session_id", sessionID, "error", err)
	}
	return nil
}

// GetSummaryMeta delegates to inner store if it supports SummaryMetaProvider.
func (d *DualWriteConversationStore) GetSummaryMeta(ctx context.Context, tenantID, userID, sessionID string) (*SummaryMetadata, error) {
	if provider, ok := d.inner.(SummaryMetaProvider); ok {
		return provider.GetSummaryMeta(ctx, tenantID, userID, sessionID)
	}
	return nil, nil
}
