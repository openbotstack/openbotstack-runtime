package builtin

import (
	"context"
	"testing"
	"time"
)

func TestNowTool(t *testing.T) {
	tool := &NowTool{}
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ts, ok := result["timestamp"].(string)
	if !ok || ts == "" {
		t.Fatal("expected non-empty timestamp string")
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("timestamp not ISO 8601: %v", err)
	}
	if parsed.After(time.Now().UTC().Add(time.Second)) {
		t.Error("timestamp should not be in the future")
	}
}

func TestUUIDGenerateTool(t *testing.T) {
	tool := &UUIDGenerateTool{}
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id, ok := result["uuid"].(string)
	if !ok || id == "" {
		t.Fatal("expected non-empty uuid string")
	}
	if len(id) != 36 {
		t.Errorf("uuid length = %d, want 36", len(id))
	}
	// Verify uniqueness
	result2, _ := tool.Execute(context.Background(), nil)
	id2 := result2["uuid"].(string)
	if id == id2 {
		t.Error("two consecutive UUIDs should differ")
	}
}
