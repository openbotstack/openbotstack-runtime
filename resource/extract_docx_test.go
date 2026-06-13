package resource

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// minimalDOCX builds an in-memory .docx (an OPC zip with the minimum parts
// extractDocX reads): word/document.xml with paragraphs + a table.
func minimalDOCX(t *testing.T, documentXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	add("word/document.xml", documentXML)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractDOCX_ParagraphsAndTable(t *testing.T) {
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
 <w:body>
  <w:p><w:r><w:t>First paragraph.</w:t></w:r></w:p>
  <w:p><w:r><w:t>Second paragraph.</w:t></w:r></w:p>
  <w:tbl>
   <w:tr><w:tc><w:p><w:r><w:t>cell A</w:t></w:r></w:p></w:tc>
         <w:tc><w:p><w:r><w:t>cell B</w:t></w:r></w:p></w:tc></w:tr>
  </w:tbl>
 </w:body>
</w:document>`
	data := minimalDOCX(t, docXML)

	doc := extractDOCX(data)
	if !strings.Contains(doc.Text, "First paragraph.") {
		t.Errorf("missing paragraph 1: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "Second paragraph.") {
		t.Errorf("missing paragraph 2: %q", doc.Text)
	}
	// Tables are flattened: both cells' text must appear.
	if !strings.Contains(doc.Text, "cell A") || !strings.Contains(doc.Text, "cell B") {
		t.Errorf("table cells not flattened: %q", doc.Text)
	}
	if doc.ContentType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Errorf("ContentType: got %q", doc.ContentType)
	}
}

func TestExtractDOCX_IncludesHeaders(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string) {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	add("word/document.xml", `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>body text</w:t></w:r></w:p></w:body></w:document>`)
	add("word/header1.xml", `<?xml version="1.0"?><w:hdr xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:p><w:r><w:t>HEADER TEXT</w:t></w:r></w:p></w:hdr>`)
	_ = zw.Close()

	doc := extractDOCX(buf.Bytes())
	if !strings.Contains(doc.Text, "body text") {
		t.Errorf("body lost: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "HEADER TEXT") {
		t.Errorf("header not extracted: %q", doc.Text)
	}
}

func TestExtractDOCX_InvalidZip(t *testing.T) {
	doc := extractDOCX([]byte("not a zip"))
	if doc.Note == "" {
		t.Error("expected a Note for invalid zip, got none")
	}
}

func TestFindAndOpen_NotFound(t *testing.T) {
	// Build a minimal zip without word/document.xml — findAndOpen should fail.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// Only add an unrelated file, not the target.
	w, _ := zw.Create("unrelated.txt")
	w.Write([]byte("nothing"))
	zw.Close()

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	data, err := findAndOpen(zr, "word/document.xml")
	if err == nil {
		t.Error("expected error for missing file in zip")
	}
	if data != nil {
		t.Error("expected nil data for missing file")
	}
}
