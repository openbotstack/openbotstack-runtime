package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/control/agent"
)

// OpenAIChatRequest mirrors the OpenAI Chat Completions request shape so
// third parties can use stock OpenAI SDKs against this endpoint.
type OpenAIChatRequest struct {
	Model    string             `json:"model"`
	Messages []OpenAIMessage    `json:"messages"`
	Stream   bool               `json:"stream,omitempty"`
}

// OpenAIMessage is a single message in the OpenAI chat format.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// handleChatCompletions exposes an OpenAI-compatible /v1/chat/completions
// endpoint (both streaming and non-streaming). It adapts the OpenAI request
// shape to the internal agent: the last user message becomes the agent input,
// and the agent's response is framed back into the chat.completion object.
// Internal execution events (planning tokens, step status) are deliberately
// NOT exposed on this wire format — only assistant content deltas, matching
// the OpenAI streaming contract that third-party SDKs expect.
func (r *Router) handleChatCompletions(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	// Decode precedes the agent-availability guard (in agentRequest below),
	// matching /v1/chat and /v1/chat/stream. A malformed body on an
	// agent-less server yields 400, not 503 — consistent across endpoints.
	var body OpenAIChatRequest
	req.Body = http.MaxBytesReader(w, req.Body, 1<<20)
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}
	userMsg := lastUserMessage(body.Messages)
	if strings.TrimSpace(userMsg) == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "messages: no user content")
		return
	}

	agentReq, ok := r.agentRequest(w, req, "", "", "", userMsg)
	if !ok {
		return
	}

	completionID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	model := body.Model
	if model == "" {
		model = "openbotstack"
	}

	if body.Stream {
		r.streamChatCompletion(w, req, agentReq, completionID, created, model)
		return
	}

	resp, err := r.agent.HandleMessage(req.Context(), agentReq)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "agent error")
		return
	}
	content := ""
	if resp != nil {
		content = resp.Message
	}
	writeJSON(w, http.StatusOK, openAICompletion(completionID, created, model, content))
}

// lastUserMessage extracts the content of the most recent role=user message.
// Returns "" when no user message exists; the caller treats that as a 400.
func lastUserMessage(msgs []OpenAIMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// openAICompletion builds a non-streaming chat.completion object.
func openAICompletion(id string, created int64, model, content string) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
	}
}

// streamChatCompletion emits the OpenAI chat.completion.chunk SSE format.
// Only assistant content tokens are forwarded as delta chunks; internal
// execution events (planning tokens, step status) are ignored on this wire
// so third-party OpenAI SDKs see a clean, standard stream.
func (r *Router) streamChatCompletion(w http.ResponseWriter, req *http.Request, agentReq agent.MessageRequest, id string, created int64, model string) {
	sse := NewSSEHandler(w)

	chunk := func(delta map[string]any, finishReason any) {
		payload := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{
				{"index": 0, "delta": delta, "finish_reason": finishReason},
			},
		}
		data, _ := json.Marshal(payload)
		_ = sse.WriteEvent(SSEEvent{Data: string(data)})
	}

	// No leading role chunk: OpenAI streaming permits content-only delta
	// chunks, and stock SDKs treat a missing role as "assistant".
	var streamed bool
	agentReq.ProgressCallback = func(eventType, content string, turn int, tool string) {
		// Forward only user-facing content deltas. Everything else (planning
		// tokens, step status) is intentionally absent from this stream.
		if eventType == "token" && content != "" {
			streamed = true
			chunk(map[string]any{"content": content}, nil)
		}
	}

	resp, err := r.agent.HandleMessage(req.Context(), agentReq)
	// If the agent returned a final message that was NOT streamed as tokens
	// (e.g. a Wasm skill producing a single output), emit it as one delta so
	// non-streaming skills still surface content on this endpoint. When tokens
	// were streamed, the message is the same text already sent — skip it.
	if err == nil && resp != nil && resp.Message != "" && !streamed {
		chunk(map[string]any{"content": resp.Message}, nil)
	}

	// Error terminal: if the agent failed, do NOT emit a success finish_reason.
	// Surface the failure to the client (finish_reason=error + an error delta)
	// and log loudly so operators see truncated generations.
	if err != nil {
		slog.ErrorContext(req.Context(), "openai chat completion: agent failed mid-stream",
			"id", id, "streamed", streamed, "error", err)
		chunk(map[string]any{"content": "\n\n[stream interrupted: generation failed]"}, "error")
		_ = sse.WriteEvent(SSEEvent{Data: "[DONE]"})
		return
	}

	// Final chunk: empty delta + finish_reason=stop, then the [DONE] sentinel.
	chunk(map[string]any{}, "stop")
	_ = sse.WriteEvent(SSEEvent{Data: "[DONE]"})
}

