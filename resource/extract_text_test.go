package resource

import (
	"strings"
	"testing"
)

func TestExtractText_UTF8(t *testing.T) {
	doc := extractText([]byte("hello 世界"), "text/plain")
	if doc.Text != "hello 世界" {
		t.Errorf("Text: got %q, want %q", doc.Text, "hello 世界")
	}
	if doc.Layout != LayoutSingleColumn {
		t.Errorf("Layout: got %v, want single_column", doc.Layout)
	}
	if doc.Note != "" {
		t.Errorf("unexpected Note for valid UTF-8: %q", doc.Note)
	}
}

func TestExtractText_Markdown(t *testing.T) {
	doc := extractText([]byte("# Title\n\nbody"), "text/markdown")
	if !strings.Contains(doc.Text, "Title") {
		t.Errorf("Markdown Text lost content: %q", doc.Text)
	}
	if doc.ContentType != "text/markdown" {
		t.Errorf("ContentType not preserved: %q", doc.ContentType)
	}
}

func TestExtractText_NonUTF8_SetsNote(t *testing.T) {
	// 0xff 0xfe is not valid UTF-8 (looks like a UTF-16LE BOM without the bytes).
	doc := extractText([]byte{0xff, 0xfe, 0x00, 'a'}, "text/plain")
	if doc.Note == "" {
		t.Error("expected a Note warning for non-UTF-8 input, got none")
	}
}
