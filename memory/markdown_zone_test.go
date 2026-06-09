package memory_test

import (
	"strings"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// --- ParseZonedBlocks: backward compatibility (no zone markers) ---

func TestParseZonedBlocks_NoZones_AllFull(t *testing.T) {
	body := []byte("\n## [2026-04-14T10:00:00Z] user\n\nHello\n\n## [2026-04-14T10:00:01Z] assistant\n\nHi\n")
	zoned := memory.ParseZonedBlocks(body)
	if len(zoned) != 2 {
		t.Fatalf("expected 2 zoned messages, got %d", len(zoned))
	}
	for i, z := range zoned {
		if z.Zone != memory.ZoneFull {
			t.Errorf("zoned[%d].Zone = %q, want %q", i, z.Zone, memory.ZoneFull)
		}
		if z.Message == nil {
			t.Fatalf("zoned[%d].Message is nil", i)
		}
	}
	if zoned[0].Message.Role != "user" {
		t.Errorf("zoned[0].Role = %q, want user", zoned[0].Message.Role)
	}
	if aitypes.FlattenToText(zoned[1].Message.Contents) != "Hi" {
		t.Errorf("zoned[1] content = %q, want Hi", aitypes.FlattenToText(zoned[1].Message.Contents))
	}
}

func TestParseZonedBlocks_Empty(t *testing.T) {
	zoned := memory.ParseZonedBlocks(nil)
	if len(zoned) != 0 {
		t.Errorf("expected 0, got %d", len(zoned))
	}
}

// --- ParseZonedBlocks: three-zone file ---

func TestParseZonedBlocks_ThreeZones(t *testing.T) {
	body := []byte(`<!-- zone:archive -->
## [2026-01-01] Session Summary

User discussed API design and chose REST over GraphQL.

<!-- zone:compressed -->
## [turn:2026-04-14T10:05] Auth discussion

Decided on JWT with refresh tokens.

## [turn:2026-04-14T10:12] DB schema

Chose PostgreSQL with JSONB.

<!-- zone:full -->
## [2026-04-14T10:20:00Z] user

What about rate limiting?

## [2026-04-14T10:20:01Z] assistant

Use a sliding window approach.
`)
	zoned := memory.ParseZonedBlocks(body)
	if len(zoned) != 5 {
		t.Fatalf("expected 5 zoned messages, got %d", len(zoned))
	}

	// Zone 3: Archive
	if zoned[0].Zone != memory.ZoneArchive {
		t.Errorf("zoned[0].Zone = %q, want archive", zoned[0].Zone)
	}
	if zoned[0].ArchiveSummary != "User discussed API design and chose REST over GraphQL." {
		t.Errorf("zoned[0].ArchiveSummary = %q", zoned[0].ArchiveSummary)
	}

	// Zone 2: Compressed turn 1
	if zoned[1].Zone != memory.ZoneCompressed {
		t.Errorf("zoned[1].Zone = %q, want compressed", zoned[1].Zone)
	}
	if zoned[1].TurnSummary == nil {
		t.Fatal("zoned[1].TurnSummary is nil")
	}
	if zoned[1].TurnSummary.Topic != "Auth discussion" {
		t.Errorf("zoned[1].Topic = %q, want Auth discussion", zoned[1].TurnSummary.Topic)
	}
	if zoned[1].TurnSummary.Summary != "Decided on JWT with refresh tokens." {
		t.Errorf("zoned[1].Summary = %q", zoned[1].TurnSummary.Summary)
	}
	if zoned[1].TurnSummary.Timestamp != "2026-04-14T10:05" {
		t.Errorf("zoned[1].Timestamp = %q", zoned[1].TurnSummary.Timestamp)
	}

	// Zone 2: Compressed turn 2
	if zoned[2].Zone != memory.ZoneCompressed {
		t.Errorf("zoned[2].Zone = %q, want compressed", zoned[2].Zone)
	}
	if zoned[2].TurnSummary.Topic != "DB schema" {
		t.Errorf("zoned[2].Topic = %q, want DB schema", zoned[2].TurnSummary.Topic)
	}

	// Zone 1: Full messages
	if zoned[3].Zone != memory.ZoneFull {
		t.Errorf("zoned[3].Zone = %q, want full", zoned[3].Zone)
	}
	if zoned[3].Message == nil {
		t.Fatal("zoned[3].Message is nil")
	}
	if zoned[3].Message.Role != "user" {
		t.Errorf("zoned[3].Role = %q, want user", zoned[3].Message.Role)
	}

	if zoned[4].Zone != memory.ZoneFull {
		t.Errorf("zoned[4].Zone = %q, want full", zoned[4].Zone)
	}
	if aitypes.FlattenToText(zoned[4].Message.Contents) != "Use a sliding window approach." {
		t.Errorf("zoned[4] content = %q", aitypes.FlattenToText(zoned[4].Message.Contents))
	}
}

// --- ParseZonedBlocks: only compressed zone ---

func TestParseZonedBlocks_CompressedOnly(t *testing.T) {
	body := []byte(`<!-- zone:compressed -->
## [turn:2026-04-14T10:05] Auth

Decided on JWT.

<!-- zone:full -->
## [2026-04-14T10:20:00Z] user

Hello
`)
	zoned := memory.ParseZonedBlocks(body)
	if len(zoned) != 2 {
		t.Fatalf("expected 2, got %d", len(zoned))
	}
	if zoned[0].Zone != memory.ZoneCompressed {
		t.Errorf("zoned[0].Zone = %q", zoned[0].Zone)
	}
	if zoned[1].Zone != memory.ZoneFull {
		t.Errorf("zoned[1].Zone = %q", zoned[1].Zone)
	}
}

// --- FormatTurnSummary ---

func TestFormatTurnSummary(t *testing.T) {
	ts := memory.TurnSummary{
		Timestamp: "2026-04-14T10:05",
		Topic:     "Auth discussion",
		Summary:   "Decided on JWT with refresh tokens.",
	}
	got := memory.FormatTurnSummary(ts)
	want := "\n## [turn:2026-04-14T10:05] Auth discussion\n\nDecided on JWT with refresh tokens.\n"
	if got != want {
		t.Errorf("FormatTurnSummary() = %q, want %q", got, want)
	}
}

func TestFormatTurnSummary_WithDecisions(t *testing.T) {
	ts := memory.TurnSummary{
		Timestamp: "2026-04-14T10:12",
		Topic:     "DB schema",
		Summary:   "Chose PostgreSQL with JSONB.",
		Decisions: []string{"Use JSONB for metadata", "Create users table"},
		Facts:     []string{"Team uses PostgreSQL 15"},
	}
	got := memory.FormatTurnSummary(ts)
	if got == "" {
		t.Error("FormatTurnSummary returned empty string")
	}
	// Should contain the turn header
	if !strings.Contains(got, "## [turn:2026-04-14T10:12] DB schema") {
		t.Errorf("missing turn header in %q", got)
	}
	// Should contain decisions
	if !strings.Contains(got, "Use JSONB for metadata") {
		t.Errorf("missing decision in %q", got)
	}
	// Should contain facts
	if !strings.Contains(got, "Team uses PostgreSQL 15") {
		t.Errorf("missing fact in %q", got)
	}
}

// --- FormatArchiveSection ---

func TestFormatArchiveSection(t *testing.T) {
	got := memory.FormatArchiveSection("User discussed API design and chose REST.")
	if !strings.Contains(got, "<!-- zone:archive -->") {
		t.Errorf("missing zone marker in %q", got)
	}
	if !strings.Contains(got, "User discussed API design and chose REST.") {
		t.Errorf("missing summary in %q", got)
	}
}

// --- FormatZonedBody: full three-zone formatting ---

func TestFormatZonedBody_ThreeZones(t *testing.T) {
	msg := aitypes.NewTextMessage("user", "Hello")
	zoned := []memory.ZonedMessage{
		{Zone: memory.ZoneArchive, ArchiveSummary: "Old session about API design."},
		{Zone: memory.ZoneCompressed, TurnSummary: &memory.TurnSummary{
			Timestamp: "2026-04-14T10:05",
			Topic:     "Auth",
			Summary:   "Decided on JWT.",
		}},
		{Zone: memory.ZoneFull, Message: &msg},
	}
	got := memory.FormatZonedBody(zoned)

	// Verify zone markers present
	if !strings.Contains(got, "<!-- zone:archive -->") {
		t.Error("missing archive zone marker")
	}
	if !strings.Contains(got, "<!-- zone:compressed -->") {
		t.Error("missing compressed zone marker")
	}
	if !strings.Contains(got, "<!-- zone:full -->") {
		t.Error("missing full zone marker")
	}
	// Verify content present
	if !strings.Contains(got, "Old session about API design.") {
		t.Error("missing archive summary")
	}
	if !strings.Contains(got, "Decided on JWT.") {
		t.Error("missing turn summary")
	}
	if !strings.Contains(got, "Hello") {
		t.Error("missing full message content")
	}
}

// --- Round-trip: format then parse ---

func TestFormatZonedBody_RoundTrip(t *testing.T) {
	msg1 := aitypes.NewTextMessage("user", "What about rate limiting?")
	msg2 := aitypes.NewTextMessage("assistant", "Use a sliding window approach.")
	original := []memory.ZonedMessage{
		{Zone: memory.ZoneArchive, ArchiveSummary: "User discussed API design."},
		{Zone: memory.ZoneCompressed, TurnSummary: &memory.TurnSummary{
			Timestamp: "2026-04-14T10:05",
			Topic:     "Auth discussion",
			Summary:   "Decided on JWT with refresh tokens.",
		}},
		{Zone: memory.ZoneFull, Message: &msg1},
		{Zone: memory.ZoneFull, Message: &msg2},
	}

	formatted := memory.FormatZonedBody(original)
	parsed := memory.ParseZonedBlocks([]byte(formatted))

	if len(parsed) != 4 {
		t.Fatalf("round-trip: expected 4, got %d", len(parsed))
	}

	// Verify archive
	if parsed[0].Zone != memory.ZoneArchive {
		t.Errorf("round-trip [0]: zone = %q", parsed[0].Zone)
	}
	if parsed[0].ArchiveSummary != "User discussed API design." {
		t.Errorf("round-trip [0]: summary = %q", parsed[0].ArchiveSummary)
	}

	// Verify compressed
	if parsed[1].Zone != memory.ZoneCompressed {
		t.Errorf("round-trip [1]: zone = %q", parsed[1].Zone)
	}
	if parsed[1].TurnSummary.Topic != "Auth discussion" {
		t.Errorf("round-trip [1]: topic = %q", parsed[1].TurnSummary.Topic)
	}

	// Verify full messages
	if parsed[2].Zone != memory.ZoneFull {
		t.Errorf("round-trip [2]: zone = %q", parsed[2].Zone)
	}
	if aitypes.FlattenToText(parsed[2].Message.Contents) != "What about rate limiting?" {
		t.Errorf("round-trip [2]: content = %q", aitypes.FlattenToText(parsed[2].Message.Contents))
	}
}

// --- ParseZonedBlocks: turn summary with decisions/facts ---

func TestParseZonedBlocks_TurnWithDecisionsAndFacts(t *testing.T) {
	body := []byte(`<!-- zone:compressed -->
## [turn:2026-04-14T10:12] DB schema

Chose PostgreSQL with JSONB.

<!-- decisions -->
  - Use JSONB for metadata
  - Create users table
<!-- facts -->
  - Team uses PostgreSQL 15

<!-- zone:full -->
## [2026-04-14T10:20:00Z] user

Hello
`)
	zoned := memory.ParseZonedBlocks(body)
	// Should have 2 zoned messages (1 compressed + 1 full)
	if len(zoned) != 2 {
		t.Fatalf("expected 2, got %d", len(zoned))
	}
	ts := zoned[0].TurnSummary
	if ts == nil {
		t.Fatal("TurnSummary is nil")
	}
	if ts.Topic != "DB schema" {
		t.Errorf("Topic = %q, want DB schema", ts.Topic)
	}
	if len(ts.Decisions) < 1 {
		t.Errorf("Decisions = %v, want at least 1", ts.Decisions)
	}
	if len(ts.Facts) < 1 {
		t.Errorf("Facts = %v, want at least 1", ts.Facts)
	}
}

// M-6: Verify Timestamp is propagated to ZonedMessage from parsed headers
func TestParseZonedBlocks_TimestampInFullZone(t *testing.T) {
	body := []byte("\n## [2026-04-14T10:20:00Z] user\n\nHello\n\n## [2026-04-14T10:20:01Z] assistant\n\nHi\n")
	zoned := memory.ParseZonedBlocks(body)
	if len(zoned) != 2 {
		t.Fatalf("expected 2, got %d", len(zoned))
	}
	if zoned[0].Timestamp != "2026-04-14T10:20:00Z" {
		t.Errorf("zoned[0].Timestamp = %q, want 2026-04-14T10:20:00Z", zoned[0].Timestamp)
	}
	if zoned[1].Timestamp != "2026-04-14T10:20:01Z" {
		t.Errorf("zoned[1].Timestamp = %q, want 2026-04-14T10:20:01Z", zoned[1].Timestamp)
	}
}

// Verify FormatZonedBody uses ZonedMessage.Timestamp
func TestFormatZonedBody_UsesTimestamp(t *testing.T) {
	msg := aitypes.NewTextMessage("user", "Hello")
	zoned := []memory.ZonedMessage{
		{Zone: memory.ZoneFull, Timestamp: "2026-04-14T10:20:00Z", Message: &msg},
	}
	got := memory.FormatZonedBody(zoned)
	if !strings.Contains(got, "## [2026-04-14T10:20:00Z] user") {
		t.Errorf("FormatZonedBody should use ZonedMessage.Timestamp, got %q", got)
	}
}
