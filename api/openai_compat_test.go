package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-runtime/api"
)

// tokenStreamAgent is a mock agent that emits a sequence of tokens via the
// ProgressCallback (type "token") and then returns the joined text. This lets
// streaming-format tests assert how tokens are framed on the wire.
type tokenStreamAgent struct {
	tokens []string
}

func (a *tokenStreamAgent) HandleMessage(ctx context.Context, req agent.MessageRequest) (*agent.MessageResponse, error) {
	var full strings.Builder
	if req.ProgressCallback != nil {
		for _, tok := range a.tokens {
			req.ProgressCallback("token", tok, 0, "")
			full.WriteString(tok)
		}
	} else {
		for _, tok := range a.tokens {
			full.WriteString(tok)
		}
	}
	return &agent.MessageResponse{
		SessionID: req.SessionID,
		Message:   full.String(),
		SkillUsed: "respond",
	}, nil
}

// TestChatCompletions_NonStreaming_OpenAIFormat verifies the OpenAI-compatible
// endpoint returns a standard chat.completion object for non-streaming requests.
func TestChatCompletions_NonStreaming_OpenAIFormat(t *testing.T) {
	mockResp := &agent.MessageResponse{SessionID: "s1", Message: "Hello back", SkillUsed: "respond"}
	handler := api.NewRouter(api.RouterConfig{Agent: &mockAgent{response: mockResp}})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v\nbody: %s", err, rr.Body.String())
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("Expected object=chat.completion, got %v", resp["object"])
	}
	if id, _ := resp["id"].(string); !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("Expected id prefix chatcmpl-, got %q", resp["id"])
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(choices))
	}
	choice, _ := choices[0].(map[string]any)
	if choice["finish_reason"] != "stop" {
		t.Errorf("Expected finish_reason=stop, got %v", choice["finish_reason"])
	}
	msg, _ := choice["message"].(map[string]any)
	if msg["role"] != "assistant" {
		t.Errorf("Expected role=assistant, got %v", msg["role"])
	}
	if msg["content"] != "Hello back" {
		t.Errorf("Expected content 'Hello back', got %v", msg["content"])
	}
}

// TestChatCompletions_Streaming_OpenAISSEFormat verifies the streaming mode
// emits the OpenAI chat.completion.chunk SSE shape: one delta chunk per token,
// a final chunk with finish_reason=stop, then a literal [DONE] sentinel.
func TestChatCompletions_Streaming_OpenAISSEFormat(t *testing.T) {
	a := &tokenStreamAgent{tokens: []string{"Hel", "lo", " world"}}
	handler := api.NewRouter(api.RouterConfig{Agent: a})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Each SSE event is a "data: <json>\n\n" line; the last is "data: [DONE]".
	lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
	var dataLines []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "data: ") {
			dataLines = append(dataLines, strings.TrimPrefix(l, "data: "))
		}
	}
	if len(dataLines) < 3 {
		t.Fatalf("Expected >=3 data lines (3 tokens + final + DONE), got %d: %v", len(dataLines), dataLines)
	}

	// First three chunks carry the tokens as delta.content.
	for i, want := range []string{"Hel", "lo", " world"} {
		var chunk map[string]any
		if err := json.Unmarshal([]byte(dataLines[i]), &chunk); err != nil {
			t.Fatalf("data line %d not JSON: %v\nraw: %s", i, err, dataLines[i])
		}
		if chunk["object"] != "chat.completion.chunk" {
			t.Errorf("chunk %d: expected object=chat.completion.chunk, got %v", i, chunk["object"])
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) != 1 {
			t.Fatalf("chunk %d: expected 1 choice, got %d", i, len(choices))
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if delta["content"] != want {
			t.Errorf("chunk %d: expected delta.content=%q, got %v", i, want, delta["content"])
		}
	}

	// The final content chunk must carry finish_reason=stop.
	var finalChunk map[string]any
	if err := json.Unmarshal([]byte(dataLines[len(dataLines)-2]), &finalChunk); err != nil {
		t.Fatalf("final chunk not JSON: %v", err)
	}
	finalChoices, _ := finalChunk["choices"].([]any)
	finalChoice, _ := finalChoices[0].(map[string]any)
	if finalChoice["finish_reason"] != "stop" {
		t.Errorf("expected final finish_reason=stop, got %v", finalChoice["finish_reason"])
	}

	// Last data line must be the literal [DONE] sentinel.
	if dataLines[len(dataLines)-1] != "[DONE]" {
		t.Errorf("expected final data line '[DONE]', got %q", dataLines[len(dataLines)-1])
	}
}

// TestChatCompletions_RequiresAuth verifies the OpenAI endpoint is gated by
// the v1 auth middleware — a rejecting middleware must yield 401. This is the
// endpoint third parties hit with stock SDKs, so an auth regression is
// high-impact.
func TestChatCompletions_RequiresAuth(t *testing.T) {
	handler := api.NewRouter(api.RouterConfig{
		Agent: &mockAgent{response: &agent.MessageResponse{Message: "ok"}},
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			})
		},
	})

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 when auth middleware rejects, got %d", rr.Code)
	}
}

