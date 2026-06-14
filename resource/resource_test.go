package resource

import (
	"strings"
	"testing"
)

func TestReadResource_TextDispatch(t *testing.T) {
	doc := ReadResource("test.txt", []byte("hello world"), "text/plain")
	if doc.Text != "hello world" {
		t.Errorf("Text: got %q, want %q", doc.Text, "hello world")
	}
	if doc.ContentType != "text/plain" {
		t.Errorf("ContentType: got %q, want text/plain", doc.ContentType)
	}
	if doc.Layout != LayoutSingleColumn {
		t.Errorf("Layout: got %v, want single_column", doc.Layout)
	}
	if doc.ID == "" {
		t.Error("ID should not be empty")
	}
	if doc.Source != "test.txt" {
		t.Errorf("Source: got %q, want test.txt", doc.Source)
	}
}

func TestReadResource_HTMLDispatch(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test</title></head><body><article><p>Article text here.</p></article></body></html>`
	doc := ReadResource("https://example.com/page", []byte(html), "text/html")
	if !strings.Contains(doc.Text, "Article text here") {
		t.Errorf("article text not extracted: %q", doc.Text)
	}
	if doc.ContentType != "text/html" {
		t.Errorf("ContentType: got %q, want text/html", doc.ContentType)
	}
}

func TestReadResource_ImageDispatch(t *testing.T) {
	doc := ReadResource("https://example.com/photo.jpg", []byte{0xff, 0xd8, 0xff}, "image/jpeg")
	if doc.ContentType != "image/jpeg" {
		t.Errorf("ContentType: got %q, want image/jpeg", doc.ContentType)
	}
	if doc.Layout != LayoutUnknown {
		t.Errorf("Layout: got %v, want unknown for images", doc.Layout)
	}
	if len(doc.Images) == 0 {
		t.Error("Images should have at least one entry for image documents")
	}
	if doc.Text != "" {
		t.Errorf("Text should be empty for images: got %q", doc.Text)
	}
	if !strings.Contains(doc.Note, "vision") {
		t.Errorf("Note should mention vision tools: %q", doc.Note)
	}
}

func TestReadResource_ImagePNG(t *testing.T) {
	doc := ReadResource("icon.png", []byte{0x89, 'P', 'N', 'G'}, "image/png")
	if doc.ContentType != "image/png" {
		t.Errorf("ContentType: got %q, want image/png", doc.ContentType)
	}
	if len(doc.Images) == 0 {
		t.Error("Images should have at least one entry")
	}
}

func TestReadResource_EmptyContentType(t *testing.T) {
	doc := ReadResource("unknown.bin", []byte("some text content"), "")
	if doc.ContentType == "" {
		t.Log("ContentType may be empty for unknown types")
	}
	if doc.Text == "" {
		t.Error("text should not be empty when content is valid UTF-8")
	}
}

// TestReadResource_EmptyContentType_SniffsPDF pins the contract that
// ReadResource docstring advertises: when contentType is empty, the bytes are
// sniffed. A PDF served without a Content-Type header must dispatch to the PDF
// extractor — not be silently treated as plain text.
func TestReadResource_EmptyContentType_SniffsPDF(t *testing.T) {
	data := minimalTextPDF(t, "Sniffed PDF text")
	doc := ReadResource("no-header.pdf", data, "") // empty content type
	if doc.ContentType != "application/pdf" {
		t.Errorf("sniffed content type: got %q, want application/pdf", doc.ContentType)
	}
	if !strings.Contains(doc.Text, "Sniffed PDF text") {
		t.Errorf("PDF text not extracted via sniffing: got %q", doc.Text)
	}
}

// TestReadResource_EmptyContentType_SniffsHTML ensures HTML bytes with no
// content type are sniffed and dispatched to the HTML extractor.
func TestReadResource_EmptyContentType_SniffsHTML(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Sniffed</title></head>
<body><article><p>HTML body via sniffing.</p></article></body></html>`
	doc := ReadResource("no-header.html", []byte(html), "")
	if doc.ContentType != "text/html" {
		t.Errorf("sniffed content type: got %q, want text/html", doc.ContentType)
	}
	if !strings.Contains(doc.Text, "HTML body via sniffing") {
		t.Errorf("HTML text not extracted via sniffing: got %q", doc.Text)
	}
}

// TestReadResource_EmptyContentType_PlainZIPNotDOCX pins the negative case:
// a ZIP archive without the OPC part word/document.xml (e.g. an XLSX, JAR, or
// plain zip) must NOT be misidentified as DOCX by the sniffer.
func TestReadResource_EmptyContentType_PlainZIPNotDOCX(t *testing.T) {
	// Minimal ZIP local-file header + central dir magic, but no word/document.xml.
	zipBytes := []byte{
		'P', 'K', 0x03, 0x04, // local file header signature
		0x14, 0x00, 0x00, 0x00, 0x08, 0x00, // version, flags, method
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // mod time/date
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // crc, sizes
		0x04, 0x00, 0x00, 0x00, // name len, extra len
		'd', 'a', 't', 'a', // entry name "data"
	}
	doc := ReadResource("archive.zip", zipBytes, "")
	if doc.ContentType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("plain ZIP must not be misidentified as DOCX: content_type=%q", doc.ContentType)
	}
}

// TestReadResource_DeclaredPDFButHTML_ReSniffs guards against server
// mislabeling: if Content-Type says application/pdf but the bytes are actually
// HTML (e.g. an error page), ReadResource must re-sniff and dispatch to the
// HTML extractor rather than forcing PDF extraction and reporting a failure.
func TestReadResource_DeclaredPDFButHTML_ReSniffs(t *testing.T) {
	html := []byte(`<!DOCTYPE html><html><head><title>Error</title></head>
<body><article><p>Not a PDF — an error page.</p></article></body></html>`)
	doc := ReadResource("https://example.com/doc.pdf", html, "application/pdf")
	if doc.ContentType != "text/html" {
		t.Errorf("mislabelled PDF should re-sniff to text/html: got %q", doc.ContentType)
	}
	if !strings.Contains(doc.Text, "Not a PDF") {
		t.Errorf("HTML text should be extracted: got %q", doc.Text)
	}
}

// TestReadResource_DeclaredPDFAndIsPDF_KeepsPDF verifies the guard does not
// override a correct PDF declaration.
func TestReadResource_DeclaredPDFAndIsPDF_KeepsPDF(t *testing.T) {
	data := minimalTextPDF(t, "Real PDF content here")
	doc := ReadResource("doc.pdf", data, "application/pdf")
	if doc.ContentType != "application/pdf" {
		t.Errorf("real PDF should keep application/pdf: got %q", doc.ContentType)
	}
}

func TestReadResource_NonUTF8Fallback(t *testing.T) {
	doc := ReadResource("garbled.bin", []byte{0xff, 0xfe, 0x00}, "application/octet-stream")
	// Non-UTF-8 input always triggers the default extractText path, which
	// detects invalid UTF-8 and sets a Note.
	if doc.Note == "" {
		t.Error("expected a Note for non-UTF-8 binary input")
	}
}

func TestReadResource_JSONDispatch(t *testing.T) {
	doc := ReadResource("data.json", []byte(`{"key":"value"}`), "application/json")
	if doc.Text != `{"key":"value"}` {
		t.Errorf("Text: got %q", doc.Text)
	}
	if doc.ContentType != "application/json" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
}

func TestReadResource_MarkdownDispatch(t *testing.T) {
	doc := ReadResource("readme.md", []byte("# Title\n\nBody"), "text/markdown")
	if !strings.Contains(doc.Text, "Title") {
		t.Errorf("markdown text lost: %q", doc.Text)
	}
	if doc.ContentType != "text/markdown" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
}

func TestReadResource_ContentTypeWithCharset(t *testing.T) {
	doc := ReadResource("page.html", []byte("<p>hello</p>"), "text/html; charset=utf-8")
	if doc.ContentType != "text/html" {
		t.Errorf("charset parameter should be stripped: got %q", doc.ContentType)
	}
}

func TestReadResource_MalformedContentType(t *testing.T) {
	// A malformed Content-Type header that mime.ParseMediaType cannot parse.
	doc := ReadResource("page.html", []byte("<p>hello</p>"), "text/html;;")
	if doc.ContentType != "text/html;;" {
		t.Errorf("raw Content-Type should be preserved on parse failure: got %q", doc.ContentType)
	}
	if doc.Note == "" {
		t.Error("expected a Note when Content-Type parse fails")
	}
}

func TestReadResource_UniqueIDs(t *testing.T) {
	doc1 := ReadResource("a.txt", []byte("a"), "text/plain")
	doc2 := ReadResource("b.txt", []byte("b"), "text/plain")
	if doc1.ID == doc2.ID {
		t.Error("each document should have a unique ID")
	}
}

func TestReadResource_XMLDispatch(t *testing.T) {
	doc := ReadResource("data.xml", []byte(`<root><item>value</item></root>`), "application/xml")
	if !strings.Contains(doc.Text, "<root>") {
		t.Errorf("XML content lost: %q", doc.Text)
	}
	if doc.ContentType != "application/xml" {
		t.Errorf("ContentType: got %q, want application/xml", doc.ContentType)
	}
}

func TestReadResource_UnknownContentTypeFallsBack(t *testing.T) {
	doc := ReadResource("data.bin", []byte("some text"), "application/octet-stream")
	if doc.Text == "" {
		t.Error("text extraction fallback should apply for unknown content types")
	}
}

func TestReadResource_CSVTextDispatch(t *testing.T) {
	doc := ReadResource("data.csv", []byte("a,b,c\n1,2,3"), "text/csv")
	if doc.Text != "a,b,c\n1,2,3" {
		t.Errorf("CSV content lost: %q", doc.Text)
	}
	if doc.ContentType != "text/csv" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
}

func TestReadResource_SVGImage(t *testing.T) {
	// SVG is image/svg+xml — should be handled as image.
	doc := ReadResource("icon.svg", []byte(`<svg></svg>`), "image/svg+xml")
	if doc.ContentType != "image/svg+xml" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
	if len(doc.Images) == 0 {
		t.Error("SVG should be treated as image, surface an ImageRef")
	}
	if doc.Layout != LayoutUnknown {
		t.Errorf("Layout: got %v, want unknown for images", doc.Layout)
	}
}

func TestReadResource_PDFDispatch(t *testing.T) {
	data := minimalTextPDF(t, "PDF Content Test")
	doc := ReadResource("doc.pdf", data, "application/pdf")
	if !strings.Contains(doc.Text, "PDF Content Test") {
		t.Errorf("PDF text not extracted: %q", doc.Text)
	}
	if doc.ContentType != "application/pdf" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
}

func TestReadResource_DOCXDispatch(t *testing.T) {
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
 <w:body>
  <w:p><w:r><w:t>DOCX dispatch test.</w:t></w:r></w:p>
 </w:body>
</w:document>`
	data := minimalDOCX(t, docXML)
	doc := ReadResource("doc.docx", data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if !strings.Contains(doc.Text, "DOCX dispatch test") {
		t.Errorf("DOCX text not extracted: %q", doc.Text)
	}
}

func TestReadResource_LayoutField(t *testing.T) {
	doc := ReadResource("file.txt", []byte("hello"), "text/plain")
	if doc.Layout != LayoutSingleColumn {
		t.Errorf("Layout: got %v, want single_column for plain text", doc.Layout)
	}

	imgDoc := ReadResource("photo.jpg", []byte{0xff}, "image/jpeg")
	if imgDoc.Layout != LayoutUnknown {
		t.Errorf("Layout: got %v, want unknown for images", imgDoc.Layout)
	}
}

func TestReadResource_TruncatedField(t *testing.T) {
	doc := ReadResource("file.txt", []byte("hello"), "text/plain")
	if doc.Truncated {
		t.Error("Truncated should default to false")
	}
}
