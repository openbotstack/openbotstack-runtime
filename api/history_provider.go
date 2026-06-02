package api

import (
	"context"
	"log/slog"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// HistoryProviderImpl adapts memory stores to the HistoryProvider interface.
// Uses SQLite (SessionStateStore) for fast session listing, Markdown for content.
type HistoryProviderImpl struct {
	mdStore      *memory.MarkdownMemoryStore
	sessionState memory.SessionStateStore
}

// NewHistoryProvider creates a new HistoryProvider adapter.
func NewHistoryProvider(mdStore *memory.MarkdownMemoryStore, sessionState memory.SessionStateStore) *HistoryProviderImpl {
	return &HistoryProviderImpl{mdStore: mdStore, sessionState: sessionState}
}

// GetSessionHistory retrieves messages for a session.
func (p *HistoryProviderImpl) GetSessionHistory(ctx context.Context, sessionID string) ([]aitypes.Message, error) {
	if p.sessionState != nil {
		si, err := p.sessionState.GetSession(ctx, sessionID)
		if err == nil && si != nil && p.mdStore != nil {
			msgs, err := p.mdStore.GetHistory(ctx, si.TenantID, "", sessionID, 0)
			if err == nil && len(msgs) > 0 {
				return msgs, nil
			}
		}
	}
	if p.mdStore != nil {
		msgs, err := p.mdStore.GetHistoryBySessionID(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		return msgs, nil
	}
	return []aitypes.Message{}, nil
}

// ListSessions returns all sessions for the current tenant.
func (p *HistoryProviderImpl) ListSessions(ctx context.Context) ([]SessionSummary, error) {
	if p.sessionState != nil {
		sessions, err := p.sessionState.ListSessions(ctx)
		if err != nil {
			return nil, err
		}
		if len(sessions) > 0 {
			return convertSessionSummaries(sessions), nil
		}
	}
	if p.mdStore != nil {
		tenantID := ""
		if user, ok := middleware.UserFromContext(ctx); ok {
			tenantID = user.TenantID
		}
		sessions, err := p.mdStore.ListSessions(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		return convertSessionSummaries(sessions), nil
	}
	return []SessionSummary{}, nil
}

// DeleteSession removes all entries for a session.
func (p *HistoryProviderImpl) DeleteSession(ctx context.Context, sessionID string) error {
	if p.sessionState != nil {
		if err := p.sessionState.DeleteSession(ctx, sessionID); err != nil {
			slog.WarnContext(ctx, "failed to delete session from SQLite", "session_id", sessionID, "error", err)
		}
	}
	if p.mdStore != nil {
		return p.mdStore.DeleteSessionBySessionID(ctx, sessionID)
	}
	return nil
}

func convertSessionSummaries(sessions []memory.SessionInfo) []SessionSummary {
	result := make([]SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, SessionSummary{
			SessionID:  s.SessionID,
			TenantID:   s.TenantID,
			LastEntry:  s.LastEntry,
			EntryCount: s.EntryCount,
			CreatedAt:  s.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  s.UpdatedAt.Format(time.RFC3339),
		})
	}
	return result
}
