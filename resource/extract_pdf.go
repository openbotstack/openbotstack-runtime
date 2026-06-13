package resource

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dslipak/pdf"
)

// pdfTextOpRe matches PDF text-showing operators: (string) Tj, (string) ',
// and array-of-strings TJ.
var pdfTextOpRe = regexp.MustCompile(`\(([^)]*)\)\s*Tj|\(([^)]*)\)\s*'`)

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

	// dslipak/pdf can panic or hang on malformed input. Run it in a
	// goroutine with a timeout; fall back to regex stream extraction
	// if the library fails to produce a result in time.
	result := &doc
	type pdfResult struct {
		text      string
		numPages  int
		reader    *pdf.Reader
		err       error
	}
	ch := make(chan pdfResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- pdfResult{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			ch <- pdfResult{err: err}
			return
		}
		numPages := r.NumPage()
		if numPages == 0 {
			ch <- pdfResult{reader: r}
			return
		}
		textReader, err := r.GetPlainText()
		if err != nil {
			ch <- pdfResult{err: fmt.Errorf("text extraction: %w", err), reader: r, numPages: numPages}
			return
		}
		var textBuf bytes.Buffer
		_, _ = textBuf.ReadFrom(textReader)
		ch <- pdfResult{
			text:     strings.TrimSpace(textBuf.String()),
			numPages: numPages,
			reader:   r,
		}
	}()

	select {
	case pr := <-ch:
		if pr.err != nil {
			// Library failed — fall back to regex stream extraction.
			text := extractPDFStreamsFallback(data)
			if text != "" {
				result.Text = text
				result.Note = fmt.Sprintf("PDF library parse failed (%v); used stream text extraction — output may be incomplete", pr.err)
			} else {
				result.Note = fmt.Sprintf("PDF extraction failed: %v", pr.err)
			}
			return doc
		}
		if pr.reader == nil || pr.numPages == 0 {
			return doc
		}

		// Detect scanned PDF: little to no text, images present.
		imageCount := countPDFImages(pr.reader, pr.numPages)
		if len(pr.text) < 50 && imageCount > 0 {
			result.Layout = LayoutScanned
			result.Note = "Scanned PDF. No extractable text. OCR/vision recommended."
			result.Images = make([]ImageRef, 0, imageCount)
			for i := 0; i < imageCount; i++ {
				result.Images = append(result.Images, ImageRef{Page: i + 1})
			}
			return doc
		}

		// Detect two-column layout.
		if detectTwoColumn(pr.reader, pr.numPages) {
			result.Layout = LayoutTwoColumn
			result.Note = "Possible two-column PDF. Extraction order may not be perfect."
		}
		result.Text = pr.text

	case <-time.After(10 * time.Second):
		// Library hung — fall back to regex extraction.
		text := extractPDFStreamsFallback(data)
		if text != "" {
			result.Text = text
			result.Note = "PDF library timed out after 10s; used stream text extraction — output may be incomplete"
		} else {
			result.Note = "PDF extraction timed out"
		}
	}

	return doc
}

// extractPDFStreamsFallback extracts text from PDF stream blocks using regex
// when the PDF library cannot parse the file. It handles Tj and ' operators.
// This is intentionally simple — it covers the common "malformed but readable"
// case without adding a second PDF parsing dependency.
func extractPDFStreamsFallback(data []byte) string {
	// Find stream…endstream blocks. (?s) makes . match newlines.
	streamRe := regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`)
	matches := streamRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}

	var lines []string
	for _, m := range matches {
		content := m[1]
		// Extract parenthesized strings from Tj and ' operators.
		for _, op := range pdfTextOpRe.FindAllSubmatch(content, -1) {
			// op[1] is from the Tj pattern, op[2] from the ' pattern.
			for _, g := range op[1:] {
				if len(g) > 0 {
					lines = append(lines, string(g))
				}
			}
		}
		// Also try TJ arrays: [(text1) -200 (text2)] TJ
		tjRe := regexp.MustCompile(`(?s)\[(.*?)\]\s*TJ`)
		for _, tj := range tjRe.FindAllSubmatch(content, -1) {
			strRe := regexp.MustCompile(`\(([^)]*)\)`)
			for _, s := range strRe.FindAllSubmatch(tj[1], -1) {
				lines = append(lines, string(s[1]))
			}
		}
	}
	return strings.Join(lines, "\n")
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
