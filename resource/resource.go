package resource

import (
	"bytes"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// ReadResource normalises raw bytes from a source into a Document by detecting
// the content type and dispatching to the appropriate extractor.
//
// contentType is the HTTP Content-Type header (may be empty). When empty, the
// bytes are sniffed via magic bytes (http.DetectContentType plus format-specific
// signatures) so a PDF served without a Content-Type header is still dispatched
// to the PDF extractor rather than treated as plain text.
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

	// Reconcile the declared content type with the bytes' magic signature.
	// Servers mislabel (an HTML error page served as application/pdf is
	// common), which would otherwise force the wrong extractor and report a
	// spurious failure. Two cases:
	//   - header missing/generic → sniff and trust the sniff;
	//   - header names a binary format (PDF/DOCX/image) but the bytes don't
	//     match its magic → sniff overrides the (wrong) header.
	if contentType == "" || contentType == "application/octet-stream" {
		if sniffed := sniffContentType(data); sniffed != "" {
			contentType = sniffed
		}
	} else if mismatchedMagic(data, contentType) {
		if sniffed := sniffContentType(data); sniffed != "" && sniffed != contentType {
			contentType = sniffed
		}
	}

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

// sniffContentType inspects magic bytes to identify a supported format. It
// returns the canonical MIME type, or "" if nothing was recognised. PDF and
// DOCX are identified by their signatures (http.DetectContentType reports
// "application/octet-stream" for both and is therefore not sufficient on its
// own). Images and generic HTML fall back to http.DetectContentType.
func sniffContentType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("%PDF")):
		return ContentTypePDF
	case isDOCXBytes(data):
		return ContentTypeDOCX
	}
	// Leading-whitespace-tolerant HTML / XML detection (http.DetectContentType
	// sometimes returns text/plain for HTML fragments).
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	lower := bytes.ToLower(trimmed)
	if bytes.HasPrefix(lower, []byte("<!doctype html")) || bytes.HasPrefix(lower, []byte("<html")) {
		return ContentTypeHTML
	}
	// Defer to the stdlib sniffing registry for images and other common types.
	ct := http.DetectContentType(data)
	// http.DetectContentType defaults to "text/plain; charset=utf-8" for
	// arbitrary bytes — that's not a confident sniff, so don't override.
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return ""
}

// mismatchedMagic reports whether the bytes' magic signature contradicts the
// declared content type — i.e. the header names a binary format but the bytes
// don't start with that format's signature. Used to detect server mislabeling
// (e.g. an HTML error page served as application/pdf) and re-sniff. Text
// formats are not checked: their magic is unreliable and sniffing would
// second-guess every text/plain response.
func mismatchedMagic(data []byte, declared string) bool {
	switch declared {
	case ContentTypePDF:
		return !bytes.HasPrefix(data, []byte("%PDF"))
	case ContentTypeDOCX:
		return !isDOCXBytes(data)
	}
	if strings.HasPrefix(declared, "image/") {
		// Re-sniff only if the stdlib doesn't think it's an image at all.
		return !strings.HasPrefix(http.DetectContentType(data), "image/")
	}
	return false
}

// isDOCXBytes reports whether data is a ZIP archive containing the OPC part
// word/document.xml — the defining signature of a .docx file.
func isDOCXBytes(data []byte) bool {
	// ZIP local-file-header magic: PK\x03\x04 (or empty-archive PK\x05\x06).
	if !(bytes.HasPrefix(data, []byte("PK\x03\x04")) || bytes.HasPrefix(data, []byte("PK\x05\x06"))) {
		return false
	}
	return bytes.Contains(data, []byte("word/document.xml"))
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
