package memory

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/openbotstack/openbotstack-core/control/agent"
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
func FormatMessageBlock(role, content, timestamp string) string {
	return fmt.Sprintf("\n## [%s] %s\n\n%s\n", timestamp, role, content)
}

// messageHeaderRe matches "## [timestamp] role" lines.
var messageHeaderRe = regexp.MustCompile(`^## \[([^\]]+)\]\s+(\S+)`)

// ParseMessageBlocks extracts messages from the body section.
// Parses "## [timestamp] role" headings followed by content.
// Returns []agent.Message.
func ParseMessageBlocks(body []byte) []agent.Message {
	if len(body) == 0 {
		return []agent.Message{}
	}

	var messages []agent.Message
	scanner := bufio.NewScanner(bytes.NewReader(body))
	var (
		currentRole    string
		currentContent strings.Builder
		inMessage      bool
		contentStarted bool // tracks whether we've seen non-blank content after a header
	)

	flush := func() {
		if !inMessage {
			return
		}
		content := strings.TrimRight(currentContent.String(), "\n")
		messages = append(messages, agent.Message{
			Role:    currentRole,
			Content: content,
		})
		currentContent.Reset()
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
			if !contentStarted {
				// Skip leading blank lines between header and content
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
		return []agent.Message{}
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
