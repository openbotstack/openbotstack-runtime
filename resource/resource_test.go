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
