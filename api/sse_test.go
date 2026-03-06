package api_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/api"
)

func TestSSEStreamCreate(t *testing.T) {
	stream := api.NewSSEStream()
	if stream == nil {
		t.Fatal("NewSSEStream returned nil")
	}
}

func TestSSEStreamSend(t *testing.T) {
	stream := api.NewSSEStream()

	event := api.SSEEvent{
		Event: "message",
		Data:  "Hello, World!",
	}

	err := stream.Send(event)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}
}

func TestSSEStreamMultipleEvents(t *testing.T) {
	stream := api.NewSSEStream()

	events := []api.SSEEvent{
		{Event: "start", Data: "Starting..."},
		{Event: "chunk", Data: "Part 1"},
		{Event: "chunk", Data: "Part 2"},
		{Event: "done", Data: "Complete"},
	}

	for _, e := range events {
		err := stream.Send(e)
		if err != nil {
			t.Fatalf("Send failed: %v", err)
		}
	}

	if stream.Count() != 4 {
		t.Errorf("Expected 4 events, got %d", stream.Count())
	}
}

func TestSSEStreamClose(t *testing.T) {
	stream := api.NewSSEStream()
	_ = stream.Send(api.SSEEvent{Data: "test"})

	err := stream.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestSSEHandlerWrite(t *testing.T) {
	rr := httptest.NewRecorder()
	handler := api.NewSSEHandler(rr)

	err := handler.WriteEvent(api.SSEEvent{
		Event: "message",
		Data:  "Hello",
	})
	if err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "event: message") {
		t.Errorf("Expected 'event: message' in body, got: %s", body)
	}
	if !strings.Contains(body, "data: Hello") {
		t.Errorf("Expected 'data: Hello' in body, got: %s", body)
	}
}

func TestSSEHandlerStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	rr := httptest.NewRecorder()
	handler := api.NewSSEHandler(rr)

	// Simulate streaming
	go func() {
		for i := 0; i < 3; i++ {
			_ = handler.WriteEvent(api.SSEEvent{
				Event: "chunk",
				Data:  "data-" + string(rune('0'+i)),
			})
			time.Sleep(10 * time.Millisecond)
		}
	}()

	<-ctx.Done()

	// Check headers
	if rr.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Expected Content-Type text/event-stream")
	}
}
