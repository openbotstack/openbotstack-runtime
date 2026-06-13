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
