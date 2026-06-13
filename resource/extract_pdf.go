package resource

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/dslipak/pdf"
)

// extractPDF parses a PDF file and extracts its text content.
// It also classifies the layout:
//   - LayoutScanned: text is nearly empty and the PDF appears image-heavy
//   - LayoutTwoColumn: text positions indicate multiple text columns
//   - LayoutSingleColumn: normal single-column text flow
//
// PDF limitations are explicitly surfaced via Document.Note:
//   - two-column → "Possible two-column PDF. Extraction order may not be perfect."
//   - scanned → "Scanned PDF. No extractable text. OCR/vision recommended."
func extractPDF(data []byte) Document {
	doc := Document{
		ContentType: ContentTypePDF,
		Layout:      LayoutSingleColumn,
	}

	// dslipak/pdf panics on malformed input. Recover and surface as Note.
	result := &doc
	func() {
		defer func() {
			if r := recover(); r != nil {
				result.Text = ""
				result.Layout = LayoutUnknown
				result.Note = fmt.Sprintf("PDF extraction panicked: %v", r)
			}
		}()

		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			result.Note = fmt.Sprintf("PDF extraction failed: %v", err)
			return
		}

		numPages := r.NumPage()
		if numPages == 0 {
			return
		}

		// Extract plain text from all pages.
		textReader, err := r.GetPlainText()
		if err != nil {
			result.Note = fmt.Sprintf("PDF text extraction failed: %v", err)
			return
		}
		var textBuf bytes.Buffer
		_, _ = textBuf.ReadFrom(textReader)
		text := strings.TrimSpace(textBuf.String())

		// Detect scanned PDF: little to no text, images present.
		imageCount := countPDFImages(r, numPages)
		if len(text) < 50 && imageCount > 0 {
			result.Layout = LayoutScanned
			result.Note = "Scanned PDF. No extractable text. OCR/vision recommended."
			// Surface page images as ImageRef for the planner.
			result.Images = make([]ImageRef, 0, imageCount)
			for i := 0; i < imageCount; i++ {
				result.Images = append(result.Images, ImageRef{Page: i + 1})
			}
			return
		}

		// Detect two-column layout by examining text columns on the first few pages.
		if detectTwoColumn(r, numPages) {
			result.Layout = LayoutTwoColumn
			result.Note = "Possible two-column PDF. Extraction order may not be perfect."
		}

		result.Text = text
	}()

	return doc
}

// countPDFImages counts how many image XObjects exist across all pages.
func countPDFImages(r *pdf.Reader, numPages int) int {
	count := 0
	for i := 1; i <= numPages; i++ {
		page := r.Page(i)
		resources := page.Resources()
		xobj := resources.Key("XObject")
		if xobj.Kind() == pdf.Dict {
			for _, key := range xobj.Keys() {
				obj := xobj.Key(key)
				if obj.Key("Subtype").Name() == "Image" {
					count++
				}
			}
		}
	}
	return count
}

// detectTwoColumn checks the first few pages for columnar text layout.
// A PDF is considered two-column if, on any checked page, GetTextByColumn
// returns 2+ columns each containing substantial text.
func detectTwoColumn(r *pdf.Reader, numPages int) bool {
	// Examine up to 3 pages.
	maxPages := 3
	if numPages < maxPages {
		maxPages = numPages
	}
	for i := 1; i <= maxPages; i++ {
		page := r.Page(i)
		columns, err := page.GetTextByColumn()
		if err != nil || len(columns) < 2 {
			continue
		}
		significantCols := 0
		for _, col := range columns {
			colText := extractColumnText(col)
			if len(colText) > 50 {
				significantCols++
			}
		}
		if significantCols >= 2 {
			return true
		}
	}
	return false
}

// extractColumnText concatenates Text items within a column in vertical order.
func extractColumnText(col *pdf.Column) string {
	if col == nil {
		return ""
	}
	var buf bytes.Buffer
	for _, t := range col.Content {
		buf.WriteString(t.S)
	}
	return buf.String()
}
