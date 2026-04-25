package adapters

import (
	"context"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// HistoryProvider adapts memory stores to the api.HistoryProvider interface.
// Uses SQLite (SessionStateStore) for fast session listing, Markdown for content.
type HistoryProvider struct {
	mdStore      *memory.MarkdownMemoryStore
	sessionState memory.SessionStateStore
}

// NewHistoryProvider creates a new HistoryProvider adapter.
func NewHistoryProvider(mdStore *memory.MarkdownMemoryStore, sessionState memory.SessionStateStore) *HistoryProvider {
	return &HistoryProvider{mdStore: mdStore, sessionState: sessionState}
}

// GetSessionHistory retrieves messages for a session.
func (p *HistoryProvider) GetSessionHistory(ctx context.Context, sessionID string) ([]api.Message, error) {
	if p.sessionState != nil {
		si, err := p.sessionState.GetSession(ctx, sessionID)
		if err == nil && si != nil && p.mdStore != nil {
			msgs, err := p.mdStore.GetHistory(ctx, si.TenantID, "", sessionID, 0)
			if err == nil && len(msgs) > 0 {
				return convertMessages(msgs), nil
			}
		}
	}
	if p.mdStore != nil {
		msgs, err := p.mdStore.GetHistoryBySessionID(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		return convertMessages(msgs), nil
	}
	return []api.Message{}, nil
}

// ListSessions returns all sessions for the current tenant.
func (p *HistoryProvider) ListSessions(ctx context.Context) ([]api.SessionSummary, error) {
	if p.sessionState != nil {
		sessions, err := p.sessionState.ListSessions(ctx)
		if err != nil {
			return nil, err
		}
		if len(sessions) > 0 {
			return convertSummaries(sessions), nil
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
		return convertSummaries(sessions), nil
	}
	return []api.SessionSummary{}, nil
}

// DeleteSession removes all entries for a session.
func (p *HistoryProvider) DeleteSession(ctx context.Context, sessionID string) error {
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

func convertMessages(msgs []agent.Message) []api.Message {
	result := make([]api.Message, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, api.Message{Role: m.Role, Content: m.Content})
	}
	return result
}

func convertSummaries(sessions []memory.SessionInfo) []api.SessionSummary {
	result := make([]api.SessionSummary, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, api.SessionSummary{
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
