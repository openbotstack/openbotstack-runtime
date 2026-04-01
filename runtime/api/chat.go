package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/openbotstack/openbotstack-runtime/runtime"
)

// ChatServer handles incoming HTTP chat requests and manages streaming output.
type ChatServer struct {
	assistant *runtime.AssistantRuntime
	logger    *slog.Logger
}

// NewChatServer initializes a new API server for the bot runtime.
func NewChatServer(assistant *runtime.AssistantRuntime, logger *slog.Logger) *ChatServer {
	return &ChatServer{
		assistant: assistant,
		logger:    logger,
	}
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ServeHTTP implements the POST /chat endpoint and streams tokens via Server-Sent Events.
func (s *ChatServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.Path != "/chat" {
		http.NotFound(w, r)
		return
	}

	start := time.Now()
	
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}

	// Observability: Execution trace logging
	traceID := uuid.New().String()
	s.logger.Info("Chat request started", 
		"trace_id", traceID, 
		"session_id", req.SessionID,
		"message_length", len(req.Message))

	// Embed trace information in context for down-stream tool call tracing
	ctx := context.WithValue(r.Context(), "trace_id", traceID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// CORS headers can be added here if needed

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error("Streaming unsupported", "trace_id", traceID)
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	s.logger.Info("Delegating to AssistantRuntime", "trace_id", traceID)
	// Process chat
	stream, err := s.assistant.ProcessChat(ctx, req.SessionID, req.Message)
	if err != nil {
		s.logger.Error("Chat processing failed", "trace_id", traceID, "error", err)
		fmt.Fprintf(w, "data: {\"error\": \"%v\"}\n\n", err)
		flusher.Flush()
		return
	}

	// Observability: step logs
	s.logger.Info("Streaming response tokens", "trace_id", traceID)

	for token := range stream {
		// Observability: tool call tracking & intermediate debugging
		s.logger.Debug("Token streamed", "trace_id", traceID, "token", token)
		
		fmt.Fprintf(w, "data: %s\n\n", token)
		flusher.Flush()
	}

	s.logger.Info("Chat request completed", 
		"trace_id", traceID, 
		"duration_ms", time.Since(start).Milliseconds())
}
