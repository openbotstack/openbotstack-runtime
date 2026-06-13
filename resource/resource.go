package resource

import (
	"mime"
	"strings"

	"github.com/google/uuid"
)

// ReadResource normalises raw bytes from a source into a Document by detecting
// the content type and dispatching to the appropriate extractor.
//
// contentType is the HTTP Content-Type header (may be empty). When empty,
// content sniffing from data is attempted.
//
// The returned Document carries a unique ID, the source URL, and the detected
// or provided content type. It is the canonical boundary between "bytes from
// somewhere" and "text we reason about".
func ReadResource(source string, data []byte, contentType string) Document {
	id := uuid.New().String()

	var parseNote string
	// Normalise: strip parameters (e.g. "; charset=utf-8").
	if ct, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = ct
	} else if contentType != "" {
		parseNote = "Content-Type header could not be parsed; using raw value"
	}
	contentType = strings.TrimSpace(contentType)

	var doc Document
	switch {
	case contentType == ContentTypeHTML:
		doc = extractHTML(data)
	case contentType == ContentTypePDF:
		doc = extractPDF(data)
	case contentType == ContentTypeDOCX:
		doc = extractDOCX(data)
	case isImageContentType(contentType):
		doc = Document{
			ContentType: contentType,
			Layout:      LayoutUnknown,
			Images: []ImageRef{{
				URL: source,
			}},
			Note: "Image document. No text extraction performed. Use vision tools to analyze.",
		}
	case isTextualContentType(contentType):
		doc = extractText(data, contentType)
	default:
		// Unknown content type: attempt UTF-8 text extraction as fallback.
		doc = extractText(data, contentType)
		if contentType == "" {
			doc.Note = "Empty content type; text extraction attempted"
		}
	}

	doc.ID = id
	doc.Source = source
	if parseNote != "" && doc.Note == "" {
		doc.Note = parseNote
	} else if parseNote != "" {
		doc.Note = doc.Note + "; " + parseNote
	}
	return doc
}

// isImageContentType reports whether mimeType is an image.
func isImageContentType(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// isTextualContentType reports whether mimeType is a text format that
// extractText handles well.
func isTextualContentType(mimeType string) bool {
	switch mimeType {
	case "text/plain", "text/markdown", "text/csv",
		"application/json", "application/xml", "text/xml":
		return true
	}
	return strings.HasPrefix(mimeType, "text/")
}
