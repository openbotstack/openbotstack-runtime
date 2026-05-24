package jsonrpc

import (
	"encoding/json"
	"testing"
)

func TestParseSSEEvent(t *testing.T) {
	input := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n")
	raw, err := parseSSEEvent(input)
	if err != nil {
		t.Fatalf("parseSSEEvent: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", resp["jsonrpc"])
	}
}

func TestParseSSEEvent_NoData(t *testing.T) {
	input := []byte("event: message\n\n")
	_, err := parseSSEEvent(input)
	if err == nil {
		t.Fatal("expected error for no data")
	}
}
