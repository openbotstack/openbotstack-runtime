package memory_test

import (
	"testing"

	"github.com/openbotstack/openbotstack-runtime/memory"
)

func TestFormatAndParseFrontmatter(t *testing.T) {
	meta := map[string]string{
		"session_id":    "sess-abc",
		"tenant_id":     "default",
		"user_id":       "user-1",
		"created_at":    "2026-04-14T10:00:00Z",
		"updated_at":    "2026-04-14T10:05:00Z",
		"message_count": "4",
	}

	fm := memory.FormatFrontmatter(meta)
	parsed, body, err := memory.ParseFrontmatter([]byte(fm))
	if err != nil {
		t.Fatalf("ParseFrontmatter returned error: %v", err)
	}

	for k, want := range meta {
		got, ok := parsed[k]
		if !ok {
			t.Errorf("missing key %q in parsed frontmatter", k)
			continue
		}
		if got != want {
			t.Errorf("parsed[%q] = %q, want %q", k, got, want)
		}
	}

	// Body should be empty (frontmatter only)
	if len(body) != 0 {
		t.Errorf("expected empty body, got %q", string(body))
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	input := []byte("Just some regular text\nwithout any frontmatter.\n")
	meta, body, err := memory.ParseFrontmatter(input)
	if err != nil {
		t.Fatalf("ParseFrontmatter returned error: %v", err)
	}
	if len(meta) != 0 {
		t.Errorf("expected empty meta, got %v", meta)
	}
	if string(body) != string(input) {
		t.Errorf("body = %q, want %q", string(body), string(input))
	}
}

func TestFormatMessageBlock(t *testing.T) {
	got := memory.FormatMessageBlock("user", "What is the weather?", "2026-04-14T10:00:00Z")
	want := "\n## [2026-04-14T10:00:00Z] user\n\nWhat is the weather?\n"
	if got != want {
		t.Errorf("FormatMessageBlock() = %q, want %q", got, want)
	}
}

func TestParseMessageBlocks(t *testing.T) {
	body := []byte("\n## [2026-04-14T10:00:00Z] user\n\nWhat is the weather in Tokyo?\n\n## [2026-04-14T10:00:05Z] assistant\n\nLet me check the weather for you.\n\n## [2026-04-14T10:00:10Z] assistant\n\nThe weather in Tokyo is sunny, 22C.\n")

	msgs := memory.ParseMessageBlocks(body)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "user" {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, "user")
	}
	if msgs[0].Content != "What is the weather in Tokyo?" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "What is the weather in Tokyo?")
	}

	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, "assistant")
	}
	if msgs[1].Content != "Let me check the weather for you." {
		t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "Let me check the weather for you.")
	}

	if msgs[2].Role != "assistant" {
		t.Errorf("msgs[2].Role = %q, want %q", msgs[2].Role, "assistant")
	}
	if msgs[2].Content != "The weather in Tokyo is sunny, 22C." {
		t.Errorf("msgs[2].Content = %q, want %q", msgs[2].Content, "The weather in Tokyo is sunny, 22C.")
	}
}

func TestParseMessageBlocksEmpty(t *testing.T) {
	msgs := memory.ParseMessageBlocks([]byte{})
	if msgs == nil {
		t.Fatal("expected non-nil slice for empty body")
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestMessageWithMarkdownContent(t *testing.T) {
	body := []byte("\n## [2026-04-14T10:00:00Z] user\n\nShow me a Go hello world.\n\n## [2026-04-14T10:00:05Z] assistant\n\nHere is a Go hello world:\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}\n```\n\nAnd a list:\n- Item 1\n- Item 2\n- Item 3\n\n## [2026-04-14T10:00:10Z] user\n\nThanks!\n")

	msgs := memory.ParseMessageBlocks(body)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "user" || msgs[0].Content != "Show me a Go hello world." {
		t.Errorf("msgs[0] = %+v", msgs[0])
	}

	wantContent := "Here is a Go hello world:\n\n```go\npackage main\n\nimport \"fmt\"\n\nfunc main() {\n    fmt.Println(\"Hello, World!\")\n}\n```\n\nAnd a list:\n- Item 1\n- Item 2\n- Item 3"
	if msgs[1].Content != wantContent {
		t.Errorf("msgs[1].Content = %q\nwant %q", msgs[1].Content, wantContent)
	}

	if msgs[2].Role != "user" || msgs[2].Content != "Thanks!" {
		t.Errorf("msgs[2] = %+v", msgs[2])
	}
}

func TestParseMessageBlocksTrailingContent(t *testing.T) {
	// No trailing newline after last message
	body := []byte("\n## [2026-04-14T10:00:00Z] user\n\nHello\n\n## [2026-04-14T10:00:05Z] assistant\n\nHi there!")
	msgs := memory.ParseMessageBlocks(body)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello" {
		t.Errorf("msgs[0].Content = %q, want %q", msgs[0].Content, "Hello")
	}
	if msgs[1].Content != "Hi there!" {
		t.Errorf("msgs[1].Content = %q, want %q", msgs[1].Content, "Hi there!")
	}
}
