package harness

import (
	"fmt"
	"strings"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

func TestEstimateMessageTokens(t *testing.T) {
	msg1 := "Hello, this is a test message"
	msg2 := "I understand your question"
	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", msg1),
		aitypes.NewTextMessage("assistant", msg2),
	}
	tokens := EstimateMessageTokens(msgs)
	if tokens <= 0 {
		t.Fatal("tokens should be positive")
	}
	expected := (len(msg1) + len(msg2)) / 4
	if tokens != expected {
		t.Errorf("tokens = %d, want %d", tokens, expected)
	}
}

func TestEstimateMessageTokens_Nil(t *testing.T) {
	if EstimateMessageTokens(nil) != 0 {
		t.Error("nil should return 0")
	}
	if EstimateMessageTokens([]aitypes.Message{}) != 0 {
		t.Error("empty slice should return 0")
	}
}

func TestEstimateMessageTokens_MultipleContentBlocks(t *testing.T) {
	msg := aitypes.Message{
		Role: "user",
		Contents: []aitypes.ContentBlock{
			aitypes.NewTextBlock("first part"),
			aitypes.NewTextBlock("second part"),
		},
	}
	tokens := EstimateMessageTokens([]aitypes.Message{msg})
	expected := (10 + 11) / 4 // 5
	if tokens != expected {
		t.Errorf("tokens = %d, want %d", tokens, expected)
	}
}

func TestTruncateHistoryByToken_DropsOldest(t *testing.T) {
	msgs := make([]aitypes.Message, 10)
	for i := range msgs {
		msgs[i] = aitypes.NewTextMessage("user", fmt.Sprintf("msg %d: %s", i, strings.Repeat("x", 76)))
	}
	// Each message: 83 chars. Budget: 80 tokens = 320 chars.
	// 320/83 ≈ 3.8 → keeps 3 messages (249 chars = 62 tokens)
	truncated := TruncateHistoryByToken(msgs, 80)
	if len(truncated) >= len(msgs) {
		t.Fatalf("should have dropped messages, got %d of %d", len(truncated), len(msgs))
	}
	lastTruncated := truncated[len(truncated)-1]
	lastOriginal := msgs[len(msgs)-1]
	if aitypes.FlattenToText(lastTruncated.Contents) != aitypes.FlattenToText(lastOriginal.Contents) {
		t.Error("should keep the last message from original")
	}
	estTokens := EstimateMessageTokens(truncated)
	if estTokens > 80 {
		t.Errorf("truncated history %d tokens exceeds budget 80", estTokens)
	}
}

func TestTruncateHistoryByToken_WithinBudget(t *testing.T) {
	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", "short"),
		aitypes.NewTextMessage("assistant", "reply"),
	}
	truncated := TruncateHistoryByToken(msgs, 100)
	if len(truncated) != len(msgs) {
		t.Errorf("should keep all messages when within budget, got %d of %d", len(truncated), len(msgs))
	}
}

func TestTruncateHistoryByToken_ZeroBudget(t *testing.T) {
	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", "hello"),
	}
	truncated := TruncateHistoryByToken(msgs, 0)
	if len(truncated) != 0 {
		t.Errorf("zero budget should return empty, got %d", len(truncated))
	}
}

func TestTruncateHistoryByToken_Nil(t *testing.T) {
	truncated := TruncateHistoryByToken(nil, 100)
	if truncated != nil {
		t.Error("nil input should return nil")
	}
}

func TestTruncateHistoryByToken_SingleMessageExceedsBudget(t *testing.T) {
	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", strings.Repeat("x", 1000)),
	}
	truncated := TruncateHistoryByToken(msgs, 10)
	if len(truncated) != 1 {
		t.Errorf("should keep at least 1 message, got %d", len(truncated))
	}
}
