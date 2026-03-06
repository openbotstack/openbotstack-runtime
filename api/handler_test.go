package api_test

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-runtime/api"
)

func TestChatHandlerProcess(t *testing.T) {
	handler := api.NewChatHandler()
	ctx := context.Background()

	req := api.ChatRequest{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   "Hello",
	}

	resp, err := handler.Process(ctx, req)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if resp.SessionID != req.SessionID {
		t.Errorf("Expected session ID '%s', got '%s'", req.SessionID, resp.SessionID)
	}
	if resp.Message == "" {
		t.Error("Expected non-empty response message")
	}
}

func TestChatHandlerValidation(t *testing.T) {
	handler := api.NewChatHandler()
	ctx := context.Background()

	tests := []struct {
		name    string
		req     api.ChatRequest
		wantErr bool
	}{
		{
			name:    "valid request",
			req:     api.ChatRequest{TenantID: "t", UserID: "u", Message: "hi"},
			wantErr: false,
		},
		{
			name:    "missing tenant",
			req:     api.ChatRequest{UserID: "u", Message: "hi"},
			wantErr: true,
		},
		{
			name:    "missing message",
			req:     api.ChatRequest{TenantID: "t", UserID: "u"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := handler.Process(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChatHandlerSessionCreate(t *testing.T) {
	handler := api.NewChatHandler()
	ctx := context.Background()

	// Without session ID, should auto-generate
	req := api.ChatRequest{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Message:  "Hello",
	}

	resp, err := handler.Process(ctx, req)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if resp.SessionID == "" {
		t.Error("Expected auto-generated session ID")
	}
}

func TestChatHandlerHistory(t *testing.T) {
	handler := api.NewChatHandler()
	ctx := context.Background()

	req := api.ChatRequest{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		SessionID: "history-test",
		Message:   "First message",
	}

	_, _ = handler.Process(ctx, req)

	history, err := handler.GetHistory(ctx, "history-test")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}

	if len(history) == 0 {
		t.Error("Expected non-empty history")
	}
}
