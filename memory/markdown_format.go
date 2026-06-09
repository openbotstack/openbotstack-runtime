package memory

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
)

// FormatFrontmatter builds YAML frontmatter from a key-value map.
// Output: "---\nkey: \"value\"\n---\n"
func FormatFrontmatter(meta map[string]string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	for k, v := range meta {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(strconv.Quote(v))
		sb.WriteString("\n")
	}
	sb.WriteString("---\n")
	return sb.String()
}

// ParseFrontmatter splits raw file content into frontmatter metadata and body.
// Returns (metadata map, body bytes, error).
// If no frontmatter found, returns empty meta and full body.
func ParseFrontmatter(data []byte) (map[string]string, []byte, error) {
	meta := make(map[string]string)

	// Must start with "---\n"
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return meta, data, nil
	}

	// Find closing "---\n" after the opening one
	rest := data[4:]
	closingIdx := bytes.Index(rest, []byte("\n---\n"))
	if closingIdx < 0 {
		// Try "---\n" at the very end (no trailing newline after closing)
		closingIdx = bytes.Index(rest, []byte("\n---"))
		if closingIdx < 0 || closingIdx+4 != len(rest) {
			return meta, data, nil
		}
		body := rest[closingIdx+5:] // skip "\n---\n"
		parseYAMLLines(rest[:closingIdx], meta)
		return meta, body, nil
	}

	body := rest[closingIdx+5:] // skip "\n---\n"
	parseYAMLLines(rest[:closingIdx], meta)
	return meta, body, nil
}

// parseYAMLLines parses simple "key: \"value\"" lines into meta.
func parseYAMLLines(data []byte, meta map[string]string) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Unquote if quoted
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			if u, err := strconv.Unquote(val); err == nil {
				val = u
			}
		}
		meta[key] = val
	}
}

// FormatMessageBlock creates a markdown section for one message.
// Output: "\n## [timestamp] role\n\ncontent\n"
// If executionID is set, it's embedded as "<!-- exec:ID -->" before content.
func FormatMessageBlock(role, content, timestamp string, executionID string) string {
	header := fmt.Sprintf("\n## [%s] %s\n", timestamp, role)
	if executionID != "" {
		return header + fmt.Sprintf("\n<!-- exec:%s -->\n\n%s\n", executionID, content)
	}
	return header + fmt.Sprintf("\n%s\n", content)
}

// messageHeaderRe matches "## [timestamp] role" lines. Timestamp may be empty.
var messageHeaderRe = regexp.MustCompile(`^## \[([^\]]*)\]\s+(\S+)`)

// execCommentRe matches "<!-- exec:UUID -->" comments embedded in message blocks.
var execCommentRe = regexp.MustCompile(`^<!-- exec:(.+) -->$`)

// ParseMessageBlocks extracts messages from the body section.
// Parses "## [timestamp] role" headings followed by content.
// Returns []aitypes.Message with optional ExecutionID from "<!-- exec:ID -->" comments.
func ParseMessageBlocks(body []byte) []aitypes.Message {
	if len(body) == 0 {
		return []aitypes.Message{}
	}

	var messages []aitypes.Message
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var (
		currentRole    string
		currentContent strings.Builder
		currentExecID  string
		inMessage      bool
		contentStarted bool
	)

	flush := func() {
		if !inMessage {
			return
		}
		content := strings.TrimRight(currentContent.String(), "\n")
		messages = append(messages, aitypes.Message{
			Role:        currentRole,
			Contents:    []aitypes.ContentBlock{aitypes.NewTextBlock(content)},
			ExecutionID: currentExecID,
		})
		currentContent.Reset()
		currentExecID = ""
		inMessage = false
		contentStarted = false
	}

	for scanner.Scan() {
		line := scanner.Text()
		matches := messageHeaderRe.FindStringSubmatch(line)
		if matches != nil {
			flush()
			currentRole = matches[2]
			inMessage = true
			contentStarted = false
			continue
		}
		if inMessage {
			// Check for execution ID comment
			if execMatches := execCommentRe.FindStringSubmatch(line); execMatches != nil {
				currentExecID = execMatches[1]
				continue
			}
			if !contentStarted {
				if line == "" {
					continue
				}
				contentStarted = true
				currentContent.WriteString(line)
			} else {
				currentContent.WriteByte('\n')
				currentContent.WriteString(line)
			}
		}
	}

	flush()

	if messages == nil {
		return []aitypes.Message{}
	}
	return messages
}

// FormatSummaryFrontmatter builds frontmatter for a summary file.
func FormatSummaryFrontmatter(sessionID string, sourceMessageCount int) map[string]string {
	return map[string]string{
		"session_id":          sessionID,
		"type":                "summary",
		"source_message_count": strconv.Itoa(sourceMessageCount),
	}
}

// zoneMarkerRe matches "<!-- zone:archive|compressed|full -->" markers.
var zoneMarkerRe = regexp.MustCompile(`^<!-- zone:(archive|compressed|full) -->$`)

// turnHeaderRe matches "## [turn:timestamp] topic" lines.
var turnHeaderRe = regexp.MustCompile(`^## \[turn:([^\]]+)\]\s+(.+)$`)

// archiveHeaderRe matches "## [date] Session Summary" lines.
var archiveHeaderRe = regexp.MustCompile(`^## \[([^\]]+)\]\s+Session Summary$`)

// listItemRe matches "  - item" lines under Decisions/Facts sections.
var listItemRe = regexp.MustCompile(`^  - (.+)$`)

var factsHeaderRe = regexp.MustCompile(`^<!-- facts -->$`)
var decisionsHeaderRe = regexp.MustCompile(`^<!-- decisions -->$`)

// ParseZonedBlocks parses body with zone markers into zoned messages.
// Backward compatible: files without zone markers return all messages in ZoneFull.
func ParseZonedBlocks(body []byte) []ZonedMessage {
	if len(body) == 0 {
		return nil
	}

	var result []ZonedMessage
	currentZone := ZoneFull // default for backward compat

	scanner := bufio.NewScanner(bytes.NewReader(body))
	var (
		// For ZoneFull messages
		currentRole      string
		currentTimestamp string
		currentContent   strings.Builder
		currentExecID    string
		inMessage        bool
		contentStarted   bool

		// For ZoneCompressed turns
		turnTimestamp string
		turnTopic     string
		turnContent   strings.Builder
		inTurn        bool
		turnStarted   bool
		turnSection   string // "decisions" or "facts"
		turnDecisions []string
		turnFacts     []string

		// For ZoneArchive
		archiveContent strings.Builder
		inArchive      bool
		archiveStarted bool
	)

	flushMessage := func() {
		if !inMessage {
			return
		}
		content := strings.TrimRight(currentContent.String(), "\n")
		result = append(result, ZonedMessage{
			Zone:      currentZone,
			Timestamp: currentTimestamp,
			Message: &aitypes.Message{
				Role:        currentRole,
				Contents:    []aitypes.ContentBlock{aitypes.NewTextBlock(content)},
				ExecutionID: currentExecID,
			},
		})
		currentContent.Reset()
		currentExecID = ""
		inMessage = false
		contentStarted = false
	}

	flushTurn := func() {
		if !inTurn {
			return
		}
		summary := strings.TrimRight(turnContent.String(), "\n")
		result = append(result, ZonedMessage{
			Zone: ZoneCompressed,
			TurnSummary: &TurnSummary{
				Timestamp: turnTimestamp,
				Topic:     turnTopic,
				Summary:   summary,
				Decisions: turnDecisions,
				Facts:     turnFacts,
			},
		})
		turnContent.Reset()
		inTurn = false
		turnStarted = false
		turnSection = ""
		turnDecisions = nil
		turnFacts = nil
	}

	flushArchive := func() {
		if !inArchive {
			return
		}
		content := strings.TrimRight(archiveContent.String(), "\n")
		result = append(result, ZonedMessage{
			Zone:           ZoneArchive,
			ArchiveSummary: content,
		})
		archiveContent.Reset()
		inArchive = false
		archiveStarted = false
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Check for zone markers
		if zoneMatches := zoneMarkerRe.FindStringSubmatch(line); zoneMatches != nil {
			// Flush any in-progress content before switching zones
			if currentZone == ZoneFull {
				flushMessage()
			}
			if currentZone == ZoneCompressed {
				flushTurn()
			}
			if currentZone == ZoneArchive {
				flushArchive()
			}
			currentZone = CompressionZone(zoneMatches[1])
			continue
		}

		switch currentZone {
		case ZoneArchive:
			// Archive: look for header, then collect content
			if archiveMatches := archiveHeaderRe.FindStringSubmatch(line); archiveMatches != nil {
				flushArchive()
				inArchive = true
				archiveStarted = false
				continue
			}
			if inArchive {
				if !archiveStarted {
					if line == "" {
						continue
					}
					archiveStarted = true
					archiveContent.WriteString(line)
				} else {
					archiveContent.WriteByte('\n')
					archiveContent.WriteString(line)
				}
			}

		case ZoneCompressed:
			// Compressed: look for turn headers
			if turnMatches := turnHeaderRe.FindStringSubmatch(line); turnMatches != nil {
				flushTurn()
				turnTimestamp = turnMatches[1]
				turnTopic = turnMatches[2]
				inTurn = true
				turnStarted = false
				turnSection = ""
				continue
			}
			if inTurn {
				// Check for decisions/facts headers
				if decisionsHeaderRe.MatchString(line) {
					turnSection = "decisions"
					continue
				}
				if factsHeaderRe.MatchString(line) {
					turnSection = "facts"
					continue
				}
				// Check for decision/fact items
				if itemMatches := listItemRe.FindStringSubmatch(line); itemMatches != nil {
					switch turnSection {
					case "decisions":
						turnDecisions = append(turnDecisions, itemMatches[1])
					case "facts":
						turnFacts = append(turnFacts, itemMatches[1])
					}
					continue
				}
				if !turnStarted {
					if line == "" {
						continue
					}
					turnStarted = true
					turnContent.WriteString(line)
				} else {
					turnContent.WriteByte('\n')
					turnContent.WriteString(line)
				}
			}

		case ZoneFull:
			// Full: reuse existing message parsing logic
			if matches := messageHeaderRe.FindStringSubmatch(line); matches != nil {
				flushMessage()
				currentTimestamp = matches[1]
				currentRole = matches[2]
				inMessage = true
				contentStarted = false
				continue
			}
			if inMessage {
				if execMatches := execCommentRe.FindStringSubmatch(line); execMatches != nil {
					currentExecID = execMatches[1]
					continue
				}
				if !contentStarted {
					if line == "" {
						continue
					}
					contentStarted = true
					currentContent.WriteString(line)
				} else {
					currentContent.WriteByte('\n')
					currentContent.WriteString(line)
				}
			}
		}
	}

	// Flush remaining
	if currentZone == ZoneFull {
		flushMessage()
	}
	if currentZone == ZoneCompressed {
		flushTurn()
	}
	if currentZone == ZoneArchive {
		flushArchive()
	}

	return result
}

// FormatTurnSummary formats a TurnSummary as a markdown block.
func FormatTurnSummary(ts TurnSummary) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n## [turn:%s] %s\n\n%s\n", ts.Timestamp, ts.Topic, ts.Summary))
	if len(ts.Decisions) > 0 {
		sb.WriteString("\n<!-- decisions -->\n")
		for _, d := range ts.Decisions {
			sb.WriteString(fmt.Sprintf("  - %s\n", d))
		}
	}
	if len(ts.Facts) > 0 {
		sb.WriteString("\n<!-- facts -->\n")
		for _, f := range ts.Facts {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}
	return sb.String()
}

// FormatArchiveSection formats the archive zone with its summary.
func FormatArchiveSection(summary string) string {
	ts := time.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf("<!-- zone:archive -->\n## [%s] Session Summary\n\n%s\n", ts, summary)
}

// FormatZonedBody formats all zones into a single markdown body string.
func FormatZonedBody(zoned []ZonedMessage) string {
	var sb strings.Builder
	prevZone := CompressionZone("")

	for _, zm := range zoned {
		// Emit zone marker when zone changes
		if zm.Zone != prevZone {
			if prevZone != "" {
				sb.WriteByte('\n')
			}
			sb.WriteString(fmt.Sprintf("<!-- zone:%s -->\n", zm.Zone))
			prevZone = zm.Zone
		}

		switch zm.Zone {
		case ZoneArchive:
			if zm.ArchiveSummary != "" {
				sb.WriteString(fmt.Sprintf("## [%s] Session Summary\n\n%s\n", time.Now().UTC().Format(time.RFC3339), zm.ArchiveSummary))
			}
		case ZoneCompressed:
			if zm.TurnSummary != nil {
				sb.WriteString(FormatTurnSummary(*zm.TurnSummary))
			}
		case ZoneFull:
			if zm.Message != nil {
				content := aitypes.FlattenToText(zm.Message.Contents)
				ts := zm.Timestamp
				if ts == "" {
					ts = "unknown"
				}
				sb.WriteString(FormatMessageBlock(zm.Message.Role, content, ts, zm.Message.ExecutionID))
			}
		}
	}

	return sb.String()
}
