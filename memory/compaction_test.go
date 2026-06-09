package memory

import (
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

func TestTurnSummaryTokenEstimate(t *testing.T) {
	ts := TurnSummary{
		Topic:   "Auth discussion",
		Summary: "Decided on JWT with refresh tokens",
	}
	tokens := ts.EstimateTokens()
	if tokens <= 0 {
		t.Error("tokens should be positive")
	}
	// Topic (15) + Summary (34) ≈ 49 chars / 4 ≈ 12
	if tokens > 20 {
		t.Errorf("tokens = %d, too high for short summary", tokens)
	}
}

func TestTurnSummaryTokenEstimate_WithDecisions(t *testing.T) {
	ts := TurnSummary{
		Topic:     "DB schema",
		Summary:   "Chose PostgreSQL",
		Decisions: []string{"Use JSONB for metadata", "Create users table"},
		Facts:     []string{"Team uses PostgreSQL 15"},
	}
	tokens := ts.EstimateTokens()
	if tokens <= 0 {
		t.Error("tokens should be positive")
	}
}

func TestCompactionPlan_Empty(t *testing.T) {
	plan := CompactionPlan{}
	if len(plan.MessagesToCompress) != 0 {
		t.Error("should be empty")
	}
	if len(plan.TurnsToArchive) != 0 {
		t.Error("should be empty")
	}
}

func TestCompactionResult_TokensSaved(t *testing.T) {
	result := &CompactionResult{
		TokensSaved: 500,
	}
	if result.TokensSaved != 500 {
		t.Errorf("TokensSaved = %d, want 500", result.TokensSaved)
	}
}

func TestZonedMessage_ZoneType(t *testing.T) {
	msg := aitypes.NewTextMessage("user", "hello")
	full := ZonedMessage{Zone: ZoneFull, Message: &msg}
	if full.Zone != ZoneFull {
		t.Error("expected ZoneFull")
	}
	compressed := ZonedMessage{Zone: ZoneCompressed, TurnSummary: &TurnSummary{Topic: "test"}}
	if compressed.Zone != ZoneCompressed {
		t.Error("expected ZoneCompressed")
	}
	archive := ZonedMessage{Zone: ZoneArchive, ArchiveSummary: "old summary"}
	if archive.Zone != ZoneArchive {
		t.Error("expected ZoneArchive")
	}
}

func TestZonedMessages_TokenEstimate(t *testing.T) {
	msg1 := aitypes.NewTextMessage("user", "Hello world")
	msg2 := aitypes.NewTextMessage("assistant", "Hi there")
	msgs := []ZonedMessage{
		{Zone: ZoneFull, Message: &msg1},
		{Zone: ZoneFull, Message: &msg2},
		{Zone: ZoneCompressed, TurnSummary: &TurnSummary{Topic: "DB", Summary: "Chose PostgreSQL"}},
		{Zone: ZoneArchive, ArchiveSummary: "Session about API design"},
	}
	tokens := EstimateZonedTokens(msgs)
	if tokens <= 0 {
		t.Error("tokens should be positive")
	}
}

func TestZonedMessages_EmptyTokenEstimate(t *testing.T) {
	tokens := EstimateZonedTokens(nil)
	if tokens != 0 {
		t.Errorf("nil should return 0, got %d", tokens)
	}
}
