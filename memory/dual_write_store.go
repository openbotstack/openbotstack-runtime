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
//
// It transparently passes through optional interface capabilities (ZonedHistoryProvider,
// SummaryMetaProvider) from the inner store, resolved once at construction.
type DualWriteConversationStore struct {
	inner           coreagent.ConversationStore
	sessionState    SessionStateStore
	innerZoned      ZonedHistoryProvider // resolved at construction; nil = not supported
	innerSummaryMeta SummaryMetaProvider  // resolved at construction; nil = not supported
}

// NewDualWriteConversationStore creates a dual-write decorator.
// Optional capabilities (ZonedHistoryProvider, SummaryMetaProvider) are resolved
// once from the inner store at construction time.
func NewDualWriteConversationStore(inner coreagent.ConversationStore, sessionState SessionStateStore) *DualWriteConversationStore {
	dw := &DualWriteConversationStore{inner: inner, sessionState: sessionState}
	// Resolve optional capabilities at construction time.
	caps := ResolveCapabilities(inner)
	dw.innerZoned = caps.ZonedProvider
	dw.innerSummaryMeta = caps.SummaryMeta
	return dw
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
		return fmt.Errorf("dual-write: sqlite metadata update failed for session %s: %w", msg.SessionID, err)
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

// GetSummaryMeta delegates to the resolved SummaryMetaProvider from construction.
func (d *DualWriteConversationStore) GetSummaryMeta(ctx context.Context, tenantID, userID, sessionID string) (*SummaryMetadata, error) {
	if d.innerSummaryMeta != nil {
		return d.innerSummaryMeta.GetSummaryMeta(ctx, tenantID, userID, sessionID)
	}
	return nil, nil
}

// GetZonedHistory delegates to the resolved ZonedHistoryProvider from construction.
func (d *DualWriteConversationStore) GetZonedHistory(ctx context.Context, tenantID, userID, sessionID string) ([]ZonedMessage, error) {
	if d.innerZoned != nil {
		return d.innerZoned.GetZonedHistory(ctx, tenantID, userID, sessionID)
	}
	return nil, fmt.Errorf("memory: zoned history not supported by inner store")
}
