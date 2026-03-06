package llm

import (
	"context"
	"testing"
)

func TestClientCreate(t *testing.T) {
	client := NewClient("https://api.example.com/v1", "test-key", "test-model")

	if client.BaseURL != "https://api.example.com/v1" {
		t.Errorf("Unexpected BaseURL: %s", client.BaseURL)
	}
	if client.APIKey != "test-key" {
		t.Errorf("Unexpected APIKey: %s", client.APIKey)
	}
	if client.Model != "test-model" {
		t.Errorf("Unexpected Model: %s", client.Model)
	}
	if client.HTTPClient == nil {
		t.Error("HTTPClient should not be nil")
	}
}

func TestClientGenerateContextCancelled(t *testing.T) {
	client := NewClient("https://api.example.com/v1", "test-key", "test-model")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.Generate(ctx, "test prompt")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}
