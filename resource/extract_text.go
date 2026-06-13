package resource

import (
	"strings"
	"unicode/utf8"
)

// extractText handles plain-text content types (text/plain, text/markdown,
// application/json, application/xml, etc.). v1 reads UTF-8 directly per spec.
// When the bytes are not valid UTF-8 it sanitizes to valid UTF-8 (invalid
// sequences → replacement char) and warns via Note so the caller knows the
// text may be partially garbled.
func extractText(data []byte, contentType string) Document {
	doc := Document{
		ContentType: contentType,
		Layout:      LayoutSingleColumn,
	}
	if utf8.Valid(data) {
		doc.Text = string(data)
		return doc
	}
	// strings.ToValidUTF8 replaces invalid byte sequences with the replacement
	// char, guaranteeing valid UTF-8 for downstream consumers.
	doc.Text = strings.ToValidUTF8(string(data), "�")
	doc.Note = "source was not valid UTF-8; invalid bytes replaced (text may be garbled)"
	return doc
}
