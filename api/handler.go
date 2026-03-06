package api

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

var (
	// ErrMissingTenantID is returned when tenant ID is missing.
	ErrMissingTenantID = errors.New("api: tenant_id is required")

	// ErrMissingMessage is returned when message is empty.
	ErrMissingMessage = errors.New("api: message is required")
)

// ChatHandler processes chat messages.
type ChatHandler struct {
	mu       sync.RWMutex
	sessions map[string][]Message
}

// NewChatHandler creates a new chat handler.
func NewChatHandler() *ChatHandler {
	return &ChatHandler{
		sessions: make(map[string][]Message),
	}
}

// Process handles a chat request.
func (h *ChatHandler) Process(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Validation
	if req.TenantID == "" {
		return nil, ErrMissingTenantID
	}
	if req.Message == "" {
		return nil, ErrMissingMessage
	}

	// Auto-generate session ID if not provided
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.New().String()
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Add user message to history
	h.sessions[sessionID] = append(h.sessions[sessionID], Message{
		Role:    "user",
		Content: req.Message,
	})

	// TODO: Integrate with agent execution and model providers
	// For now, echo back with prefix
	responseContent := "I received: " + req.Message

	// Add assistant response to history
	h.sessions[sessionID] = append(h.sessions[sessionID], Message{
		Role:    "assistant",
		Content: responseContent,
	})

	return &ChatResponse{
		SessionID: sessionID,
		Message:   responseContent,
	}, nil
}

// GetHistory returns the message history for a session.
func (h *ChatHandler) GetHistory(ctx context.Context, sessionID string) ([]Message, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	history, exists := h.sessions[sessionID]
	if !exists {
		return []Message{}, nil
	}

	// Return a copy
	result := make([]Message, len(history))
	copy(result, history)
	return result, nil
}
