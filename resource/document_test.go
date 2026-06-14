package resource

import (
	"testing"
)

// TestDocument_ToMap pins the wire shape that crosses the tool boundary.
// ToMap owns the flattening of Document — no JSON round-trip, no alias hack
// in the tool layer. The "content" alias for "text" lives here.
func TestDocument_ToMap(t *testing.T) {
	doc := Document{
		ID:          "id-1",
		Source:      "https://example.com/x.pdf",
		Title:       "Title",
		ContentType: "application/pdf",
		Charset:     "utf-8",
		Language:    "en",
		Text:        "the extracted text",
		Layout:      LayoutTwoColumn,
		Images: []ImageRef{
			{URL: "https://example.com/img.png", Page: 1},
		},
		Metadata:  map[string]string{"byline": "Author"},
		Truncated: true,
		Note:      "a note",
	}

	m := doc.ToMap()

	// Core fields.
	if m["id"] != "id-1" {
		t.Errorf("id: got %v", m["id"])
	}
	if m["text"] != "the extracted text" {
		t.Errorf("text: got %v", m["text"])
	}
	// Alias: "content" mirrors "text" so planner templates using
	// {{resource_read.content}} resolve. Owned here, not in the tool.
	if m["content"] != "the extracted text" {
		t.Errorf("content alias: got %v", m["content"])
	}
	if m["content_type"] != "application/pdf" {
		t.Errorf("content_type: got %v", m["content_type"])
	}
	if m["layout"] != string(LayoutTwoColumn) {
		t.Errorf("layout: got %v", m["layout"])
	}
	if m["truncated"] != true {
		t.Errorf("truncated: got %v", m["truncated"])
	}

	// Images must be a usable []map[string]any (not []interface{} of
	// map[string]interface{} as a JSON round-trip would produce).
	imgs, ok := m["images"].([]map[string]any)
	if !ok {
		t.Fatalf("images should be []map[string]any, got %T", m["images"])
	}
	if len(imgs) != 1 || imgs[0]["url"] != "https://example.com/img.png" || imgs[0]["page"] != 1 {
		t.Errorf("images content wrong: %v", imgs)
	}

	// Metadata stays map[string]any (string values).
	meta, ok := m["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata should be map[string]any, got %T", m["metadata"])
	}
	if meta["byline"] != "Author" {
		t.Errorf("metadata byline: got %v", meta["byline"])
	}
}

func TestDocument_ToMap_NoTextNoAlias(t *testing.T) {
	// When Text is empty, the "content" alias is omitted (zero-value), matching
	// the Document's own omitempty semantics.
	doc := Document{ContentType: "text/plain"}
	m := doc.ToMap()
	if _, ok := m["content"]; ok {
		t.Error("content alias should be absent when Text is empty")
	}
	if _, ok := m["text"]; ok {
		t.Error("text should be absent when empty (omitempty)")
	}
}
