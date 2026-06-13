package resource

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// DOCX namespace used in word/document.xml, word/header*.xml, word/footer*.xml.
const wmlNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// extractDOCX parses a .docx file (OPC zip) and extracts paragraph + table text
// from word/document.xml, word/header*.xml, and word/footer*.xml.
// Implementation uses only archive/zip and encoding/xml — no third-party deps.
func extractDOCX(data []byte) Document {
	doc := Document{
		ContentType: ContentTypeDOCX,
		Layout:      LayoutSingleColumn,
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		doc.Note = fmt.Sprintf("DOCX extraction failed: %v", err)
		return doc
	}

	var parts []string
	// Always include the main document body first.
	parts = append(parts, "word/document.xml")
	// Then headers and footers, if present.
	for _, f := range zr.File {
		name := f.Name
		if strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml") {
			parts = append(parts, name)
		}
		if strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml") {
			parts = append(parts, name)
		}
	}

	var buf bytes.Buffer
	for _, name := range parts {
		f, err := findAndOpen(zr, name)
		if err != nil {
			continue
		}
		extractWMLText(f, &buf)
	}
	doc.Text = strings.TrimSpace(buf.String())
	return doc
}

// findAndOpen locates a file by name inside a zip.Reader and returns its contents.
func findAndOpen(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("not found: %s", name)
}

// extractWMLText decodes WordprocessingML XML and writes the text content
// of <w:t> elements to buf. Paragraphs are separated by newlines.
// Tables (<w:tbl>) are flattened: cell text is emitted in order.
func extractWMLText(data []byte, buf *bytes.Buffer) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if matchWMLStart(t, "p") {
				// paragraph — extract text from child w:t elements
				text := extractParagraphText(dec)
				if text != "" {
					if buf.Len() > 0 {
						buf.WriteByte('\n')
					}
					buf.WriteString(text)
				}
			}
		}
	}
}

// extractParagraphText reads until the matching </w:p> and concatenates
// all <w:t> text content found along the way.
func extractParagraphText(dec *xml.Decoder) string {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if matchWMLStart(t, "p") || matchWMLStart(t, "tbl") || matchWMLStart(t, "tr") || matchWMLStart(t, "tc") {
				depth++
			}
			if matchWMLStart(t, "t") {
				// <w:t> — accumulate its character data.
				var inner xml.Token
				inner, err = dec.Token()
				if err != nil {
					break
				}
				if cd, ok := inner.(xml.CharData); ok {
					text.Write(cd)
				}
			}
		case xml.EndElement:
			if matchWMLEnd(t, "p") || matchWMLEnd(t, "tbl") || matchWMLEnd(t, "tr") || matchWMLEnd(t, "tc") {
				depth--
			}
		}
	}
	return text.String()
}

// matchWMLStart reports whether e is a WordprocessingML start element with
// the given local name (e.g. "p", "t", "tbl").
func matchWMLStart(e xml.StartElement, local string) bool {
	return e.Name.Local == local && (e.Name.Space == wmlNS || e.Name.Space == "")
}

// matchWMLEnd reports whether e is a WordprocessingML end element with
// the given local name.
func matchWMLEnd(e xml.EndElement, local string) bool {
	return e.Name.Local == local && (e.Name.Space == wmlNS || e.Name.Space == "")
}
