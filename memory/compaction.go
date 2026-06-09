package memory

import (
	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// CompressionZone identifies a recency zone in the session history.
type CompressionZone string

const (
	ZoneFull       CompressionZone = "full"      // verbatim messages
	ZoneCompressed CompressionZone = "compressed" // per-turn summaries
	ZoneArchive    CompressionZone = "archive"    // single session summary
)

// TurnSummary is a compressed representation of a conversation turn.
type TurnSummary struct {
	Timestamp string
	Topic     string
	Summary   string
	Decisions []string
	Facts     []string
}

// EstimateTokens returns a rough token estimate using chars/4 heuristic.
func (ts *TurnSummary) EstimateTokens() int {
	total := len(ts.Topic) + len(ts.Summary)
	for _, d := range ts.Decisions {
		total += len(d)
	}
	for _, f := range ts.Facts {
		total += len(f)
	}
	return total / 4
}

// CompactionPlan describes what to compress in one round.
type CompactionPlan struct {
	MessagesToCompress []aitypes.Message // Zone 1 → Zone 2
	TurnsToArchive    []TurnSummary      // Zone 2 → Zone 3
	ArchiveSummary    string            // existing archive to merge into

	// Internal: items kept (not compressed/archived) in this round
	fullMsgsToKeep     []aitypes.Message
	compressedToKeep   []TurnSummary
}

// CompactionResult is the output of one compression round.
type CompactionResult struct {
	NewTurnSummaries []TurnSummary
	UpdatedArchive   string
	TokensSaved      int
}

// ZonedMessage represents a message in a specific compression zone.
// Exactly one of Message, TurnSummary, or ArchiveSummary is populated.
type ZonedMessage struct {
	Zone           CompressionZone
	Message        *aitypes.Message   // set when Zone == ZoneFull
	TurnSummary    *TurnSummary        // set when Zone == ZoneCompressed
	ArchiveSummary string              // set when Zone == ZoneArchive
	Timestamp      string              // RFC3339 timestamp, used for ZoneFull formatting
}

// EstimateZonedTokens estimates total tokens across zoned messages.
func EstimateZonedTokens(msgs []ZonedMessage) int {
	total := 0
	for _, zm := range msgs {
		switch zm.Zone {
		case ZoneFull:
			if zm.Message != nil {
				for _, c := range zm.Message.Contents {
					if c.Type == "text" && c.Text != "" {
						total += len(c.Text)
					}
				}
			}
		case ZoneCompressed:
			if zm.TurnSummary != nil {
				// EstimateTokens returns chars/4; multiply back to accumulate raw chars
				total += zm.TurnSummary.EstimateTokens() * 4
			}
		case ZoneArchive:
			total += len(zm.ArchiveSummary)
		}
	}
	return total / 4
}
