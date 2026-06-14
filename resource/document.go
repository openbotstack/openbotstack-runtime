// Package resource reads bytes from a source (URL today; file://, mcp://,
// obs:// tomorrow) and normalizes them into a Document — the canonical,
// format-agnostic representation that downstream stages (summarize, chunk,
// embed, knowledge store) consume.
//
// The pipeline this package seeds:
//
//	Resource → Document → Chunk → Embedding → Knowledge
//
// v1 implements only the Resource→Document step and only the https:// source.
// The Document type is deliberately shaped so later stages can be added
// without changing it.
package resource

// LayoutType describes how text is physically laid out in the source, which
// affects extraction correctness (e.g. two-column PDFs may interleave badly).
type LayoutType string

const (
	// LayoutUnknown is the default when layout has not been determined.
	LayoutUnknown LayoutType = "unknown"
	// LayoutSingleColumn is normal top-to-bottom, left-to-right flow.
	LayoutSingleColumn LayoutType = "single_column"
	// LayoutTwoColumn indicates a two-column document (some PDFs). Extraction
	// order may not be perfect; callers are warned via Document.Note.
	LayoutTwoColumn LayoutType = "two_column"
	// LayoutScanned indicates an image-heavy document with little/no extractable
	// text (e.g. a scanned PDF). Text extraction returns little; OCR/vision is
	// the viable path.
	LayoutScanned LayoutType = "scanned"
)

// ImageRef is a reference to an image inside a Document (e.g. a PDF page image
// or an inline image). The bytes live in Base64 when the source had them
// inline; URL when the source referenced them. resource_read does NOT analyze
// images — it only surfaces them so the planner can route to vision tools.
type ImageRef struct {
	URL         string `json:"url,omitempty"`
	Base64      string `json:"base64,omitempty"`
	Page        int    `json:"page,omitempty"`
	Description string `json:"description,omitempty"`
}

// Document is the canonical, format-agnostic output of reading a resource.
// It is the boundary between "bytes from somewhere" and "text we reason about".
type Document struct {
	ID          string            `json:"id"`
	Source      string            `json:"source"` // the original URL/ref
	Title       string            `json:"title,omitempty"`
	ContentType string            `json:"content_type"` // detected MIME, e.g. application/pdf
	Charset     string            `json:"charset,omitempty"`
	Language    string            `json:"language,omitempty"`
	Text        string            `json:"text"`
	Layout      LayoutType        `json:"layout"`
	Images      []ImageRef        `json:"images,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Truncated   bool              `json:"truncated,omitempty"` // Text was cut to fit a size limit
	Note        string            `json:"note,omitempty"`      // warnings/limitations for the caller (LLM)
}

// Well-known content type constants used by extractors and ReadResource.
const (
	ContentTypeHTML  = "text/html"
	ContentTypePDF   = "application/pdf"
	ContentTypeDOCX  = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// ToMap flattens the Document into the map[string]any shape that crosses the
// builtin-tool boundary (BuiltinTool.Execute returns map[string]any). It owns
// the wire shape in one place — no JSON marshal/unmarshal round-trip, which
// would erode []ImageRef to []interface{} and lose the typed intent.
//
// It also emits a "content" alias for "text": the planner generates templates
// like {{builtin.resource_read.content}} because read_file/web_fetch use that
// key, so Document keeps both "text" (canonical) and "content" (alias)
// resolvable. The alias lives here — not scattered in the tool layer.
func (d Document) ToMap() map[string]any {
	m := map[string]any{
		"id":           d.ID,
		"source":       d.Source,
		"content_type": d.ContentType,
		"layout":       string(d.Layout),
	}
	if d.Title != "" {
		m["title"] = d.Title
	}
	if d.Charset != "" {
		m["charset"] = d.Charset
	}
	if d.Language != "" {
		m["language"] = d.Language
	}
	if d.Text != "" {
		m["text"] = d.Text
		// Alias so planner templates using {{resource_read.content}} resolve.
		m["content"] = d.Text
	}
	if len(d.Images) > 0 {
		imgs := make([]map[string]any, len(d.Images))
		for i, img := range d.Images {
			im := map[string]any{}
			if img.URL != "" {
				im["url"] = img.URL
			}
			if img.Base64 != "" {
				im["base64"] = img.Base64
			}
			if img.Page != 0 {
				im["page"] = img.Page
			}
			if img.Description != "" {
				im["description"] = img.Description
			}
			imgs[i] = im
		}
		m["images"] = imgs
	}
	if len(d.Metadata) > 0 {
		meta := make(map[string]any, len(d.Metadata))
		for k, v := range d.Metadata {
			meta[k] = v
		}
		m["metadata"] = meta
	}
	if d.Truncated {
		m["truncated"] = true
	}
	if d.Note != "" {
		m["note"] = d.Note
	}
	return m
}
