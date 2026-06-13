package resource

import (
	"bytes"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/dslipak/pdf"
)

// pdfTextOpRe matched PDF text-showing operators; retained for reference but
// the fallback now uses a balanced-paren scanner (extractPDFStrings) that
// correctly handles escaped parens — see extractPDFStreamsFallback.

// extractPDF parses a PDF file and extracts its text content.
// It also classifies the layout:
//   - LayoutScanned: text is nearly empty and the PDF appears image-heavy
//   - LayoutTwoColumn: text positions indicate multiple text columns
//   - LayoutSingleColumn: normal single-column text flow
//
// PDF limitations are explicitly surfaced via Document.Note:
//   - two-column → "Possible two-column PDF. Extraction order may not be perfect."
//   - scanned → "Scanned PDF. No extractable text. OCR/vision recommended."
// pdfExtractionTimeout bounds how long the dslipak/pdf library may run. If it
// exceeds this, we fall back to regex stream extraction.
const pdfExtractionTimeout = 10 * time.Second

func extractPDF(data []byte) Document {
	doc := Document{
		ContentType: ContentTypePDF,
		Layout:      LayoutSingleColumn,
	}

	slog.Debug("pdf: extracting", "size_bytes", len(data))

	// dslipak/pdf can panic or hang on malformed input. Run it in a goroutine
	// with a timeout; fall back to regex stream extraction if it fails.
	//
	// Goroutine-leak note: dslipak/pdf takes an io.ReaderAt and has no context
	// support, so on timeout the goroutine cannot be cancelled directly. To
	// bound it, we wrap the bytes in a deadlineReaderAt: after the deadline,
	// every ReadAt returns an error, which forces the library's internal reads
	// to fail and the goroutine to exit — rather than spinning forever on a
	// malformed file. Worst case is a brief tail until the next ReadAt fires.
	deadline := time.Now().Add(pdfExtractionTimeout)
	src := &deadlineReaderAt{data: data, deadline: deadline}

	type pdfResult struct {
		text     string
		numPages int
		reader   *pdf.Reader
		err      error
	}
	ch := make(chan pdfResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- pdfResult{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		r, err := pdf.NewReader(src, int64(len(data)))
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
			slog.Warn("pdf: library failed, trying fallback", "error", pr.err, "size_bytes", len(data))
			text := extractPDFStreamsFallback(data)
			if text != "" {
				doc.Text = text
				doc.Note = fmt.Sprintf("PDF library parse failed (%v); used stream text extraction — output may be incomplete", pr.err)
				slog.Info("pdf: fallback succeeded", "text_len", len(text))
			} else {
				doc.Note = fmt.Sprintf("PDF extraction failed: %v", pr.err)
				slog.Warn("pdf: fallback also failed", "error", pr.err)
			}
			return doc
		}
		if pr.reader == nil || pr.numPages == 0 {
			slog.Debug("pdf: empty document", "pages", pr.numPages)
			return doc
		}

		slog.Debug("pdf: library extracted",
			"pages", pr.numPages,
			"text_len", len(pr.text),
		)

		// Detect scanned PDF: little to no text, images present.
		imageCount := countPDFImages(pr.reader, pr.numPages)
		if len(pr.text) < 50 && imageCount > 0 {
			doc.Layout = LayoutScanned
			doc.Note = "Scanned PDF. No extractable text. OCR/vision recommended."
			doc.Images = make([]ImageRef, 0, imageCount)
			for i := 0; i < imageCount; i++ {
				doc.Images = append(doc.Images, ImageRef{Page: i + 1})
			}
			slog.Info("pdf: detected scanned", "pages", pr.numPages, "images", imageCount)
			return doc
		}

		// Detect two-column layout.
		if detectTwoColumn(pr.reader, pr.numPages) {
			doc.Layout = LayoutTwoColumn
			doc.Note = "Possible two-column PDF. Extraction order may not be perfect."
			slog.Info("pdf: detected two-column layout", "pages", pr.numPages)
		}
		doc.Text = pr.text

	case <-time.After(pdfExtractionTimeout):
		slog.Warn("pdf: library timed out, trying fallback", "size_bytes", len(data))
		text := extractPDFStreamsFallback(data)
		if text != "" {
			doc.Text = text
			doc.Note = "PDF library timed out after 10s; used stream text extraction — output may be incomplete"
			slog.Info("pdf: fallback succeeded after timeout", "text_len", len(text))
		} else {
			doc.Note = "PDF extraction timed out"
			slog.Warn("pdf: fallback also failed after timeout")
		}
	}

	return doc
}

// deadlineReaderAt is an io.ReaderAt over a byte slice that starts returning
// errors once the deadline passes. Used to bound the lifetime of a hung PDF
// extraction goroutine (dslipak/pdf has no context support): after the
// deadline, the library's ReadAt calls fail and the goroutine exits naturally.
type deadlineReaderAt struct {
	data     []byte
	deadline time.Time
}

func (d *deadlineReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if time.Now().After(d.deadline) {
		return 0, fmt.Errorf("pdf: extraction deadline exceeded")
	}
	if off >= int64(len(d.data)) {
		return 0, fmt.Errorf("pdf: read past end of data")
	}
	n := copy(p, d.data[off:])
	if n < len(p) {
		return n, fmt.Errorf("pdf: short read (end of data)")
	}
	return n, nil
}

// streamRe finds PDF stream blocks: "stream\n...\nendstream".
var streamRe = regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`)

// extractPDFStreamsFallback extracts text from PDF content streams using a
// balanced-paren scanner when the PDF library cannot parse the file. Unlike a
// naive regex, it correctly handles escaped parens (\( \)) and backslashes
// inside PDF literal strings, and decodes the standard escape sequences.
func extractPDFStreamsFallback(data []byte) string {
	matches := streamRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	var lines []string
	for _, m := range matches {
		lines = append(lines, extractPDFStrings(m[1])...)
	}
	return strings.Join(lines, "\n")
}

// extractPDFStrings scans content for PDF literal strings "( ... )" and returns
// each decoded. Parens balance via escape tracking, so "a (b\) c) d" yields one
// string "b) c". Strings spanning the whole content stream are collected.
func extractPDFStrings(content []byte) []string {
	var out []string
	var buf []byte
	inStr := false
	depth := 0
	for i := 0; i < len(content); i++ {
		c := content[i]
		if !inStr {
			if c == '(' {
				inStr = true
				depth = 1
				buf = buf[:0]
			}
			continue
		}
		// Inside a string.
		if c == '\\' && i+1 < len(content) {
			next := content[i+1]
			switch next {
			case 'n':
				buf = append(buf, '\n')
				i++
			case 'r':
				buf = append(buf, '\r')
				i++
			case 't':
				buf = append(buf, '\t')
				i++
			case 'b':
				buf = append(buf, 0x08)
				i++
			case 'f':
				buf = append(buf, 0x0C)
				i++
			case '(':
				buf = append(buf, '(')
				i++
			case ')':
				buf = append(buf, ')')
				i++
			case '\\':
				buf = append(buf, '\\')
				i++
			case '\n':
				// Line continuation: skip both backslash and newline.
				i++
			default:
				// Octal escape \ddd (1-3 octal digits).
				if next >= '0' && next <= '7' {
				 octal := []byte{next}
				 j := i + 2
				 for len(octal) < 3 && j < len(content) && content[j] >= '0' && content[j] <= '7' {
				 	octal = append(octal, content[j])
				 	j++
				 }
				 var v byte
				 for _, d := range octal {
				 	v = v*8 + (d - '0')
				 }
				 buf = append(buf, v)
				 i = j - 1
				} else {
				 // Unknown escape — keep the char literally.
				 buf = append(buf, next)
				 i++
				}
			}
			continue
		}
		if c == '(' {
			depth++
			buf = append(buf, c)
			continue
		}
		if c == ')' {
			depth--
			if depth == 0 {
				out = append(out, string(buf))
				inStr = false
				continue
			}
			buf = append(buf, c)
			continue
		}
		buf = append(buf, c)
	}
	return out
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
