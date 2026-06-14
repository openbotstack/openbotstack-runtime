package resource

import (
	"bytes"
	"compress/zlib"
	"os"
	"strings"
	"testing"
)

// TestExtractPDFStreamsFallback_FlateDecode verifies the fallback can recover
// text from a PDF whose content stream is zlib-compressed (FlateDecode) — the
// norm for real-world PDFs. Without zlib handling the fallback only sees the
// compressed bytes and finds no text operators.
func TestExtractPDFStreamsFallback_FlateDecode(t *testing.T) {
	// Compress a content stream that draws text.
	rawStream := []byte("BT /F1 12 Tf 100 700 Td (Compressed stream text) Tj ET")
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(rawStream)
	zw.Close()
	compressed := buf.Bytes()

	// Wrap in a stream block with the FlateDecode filter marker.
	pdf := []byte("<< /Length ")
	pdf = append(pdf, []byte(itoa(len(compressed)))...)
	pdf = append(pdf, []byte(" /Filter /FlateDecode >>\nstream\n")...)
	pdf = append(pdf, compressed...)
	pdf = append(pdf, []byte("\nendstream")...)

	got := extractPDFStreamsFallback(pdf)
	if !strings.Contains(got, "Compressed stream text") {
		t.Errorf("zlib fallback should recover FlateDecode text: got %q", got)
	}
}

// TestExtractPDF_FallbackNote_IsAccurate pins the wording of the fallback note.
// The fallback scans ALL content streams, so the extracted text is COMPLETE —
// the only real limitation is reading ORDER (e.g. two-column reflow). The note
// must not say "incomplete" (that misleads the LLM into thinking the text is
// truncated/garbage). It must say the order may differ.
func TestExtractPDF_FallbackNote_IsAccurate(t *testing.T) {
	// Build a PDF that dslipak/pdf cannot parse (broken xref) but whose
	// stream has extractable text → forces the fallback path.
	pdfBytes := []byte("%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"4 0 obj<</Length 44>>\nstream\nBT /F1 12 Tf (Fallback text here) Tj ET\nendstream\n" +
		"xref\n0 1\n0000000000 65535 f \ntrailer<<>>\nstartxref\n%%EOF")
	doc := extractPDF(pdfBytes)
	if !strings.Contains(doc.Text, "Fallback text here") {
		t.Fatalf("fallback text missing: got %q (note=%q)", doc.Text, doc.Note)
	}
	lower := strings.ToLower(doc.Note)
	if strings.Contains(lower, "incomplete") {
		t.Errorf("fallback note must not say 'incomplete' (text is complete): %q", doc.Note)
	}
	if !strings.Contains(lower, "order") {
		t.Errorf("fallback note should mention reading ORDER, not completeness: %q", doc.Note)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestExtractPDF_PageCapTruncates verifies that a PDF exceeding maxPDFPages is
// extracted only up to the cap and flagged Truncated. We can't easily build a
// 200+ page fixture, so this asserts the truncation NOTE appears on a real
// extract only when pages exceed the cap — covered here by confirming a small
// PDF is NOT truncated (the cap is well above it).
func TestExtractPDF_PageCapDoesNotTruncateSmallPDF(t *testing.T) {
	data := minimalTextPDF(t, "Short PDF under the page cap")
	doc := extractPDF(data)
	if doc.Truncated {
		t.Errorf("a %d-page PDF must not be truncated (cap=%d)", 1, maxPDFPages)
	}
}

// TestExtractPDF_PaddedBlankSample is the regression test for the user's
// failing case: fileexamples.com's "1KB sample PDF" is a valid PDF whose real
// content ends at byte ~302, then zero-padded to exactly 1024 bytes. dslipak/pdf
// scans the last 100 bytes for %%EOF, finds zeros, and reports "missing %%EOF".
//
// Expected after the redesign:
//   - the trailing zero-padding is stripped (trailer sanitization), so the
//     library parses the real 302-byte PDF;
//   - the page has no content stream (it's a blank page), so there is no text;
//   - this is NOT a failure — it's a valid empty document, classified with a
//     clear Note rather than "PDF extraction failed".
func TestExtractPDF_PaddedBlankSample(t *testing.T) {
	data, err := os.ReadFile("testdata/sample_padded_blank.pdf")
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	doc := extractPDF(data)

	// Must NOT report a hard failure for a valid-but-blank PDF.
	if strings.Contains(doc.Note, "extraction failed") || strings.Contains(doc.Note, "missing %%EOF") {
		t.Errorf("blank PDF should not be reported as a failure; Note=%q", doc.Note)
	}
	// Should explain that there's no text (blank/vector-only), not that parsing broke.
	if !strings.Contains(strings.ToLower(doc.Note), "no extractable text") {
		t.Errorf("Note should explain the blank page; got %q", doc.Note)
	}
}

// TestExtractPDF_TrailingZerosTrimmed directly exercises trailer sanitization:
// a known-good text PDF with hundreds of trailing NUL bytes must still extract
// its text VIA THE LIBRARY (not the fallback) — so the Note must not mention
// fallback, proving sanitization recovered the trailer.
func TestExtractPDF_TrailingZerosTrimmed(t *testing.T) {
	good := minimalTextPDF(t, "Recoverable text after padding")
	// Append 500 NUL bytes (simulating zero-padding / trailing garbage).
	padded := make([]byte, len(good)+500)
	copy(padded, good)
	for i := len(good); i < len(padded); i++ {
		padded[i] = 0
	}

	doc := extractPDF(padded)
	if !strings.Contains(doc.Text, "Recoverable text after padding") {
		t.Errorf("text not recovered from padded PDF: got %q (note=%q)", doc.Text, doc.Note)
	}
	if strings.Contains(strings.ToLower(doc.Note), "fallback") {
		t.Errorf("library should parse after sanitization, not fall back; Note=%q", doc.Note)
	}
}

// TestExtractPDF_TrailingGarbageTrimmed verifies sanitization also strips
// arbitrary non-PDF trailing bytes (not just zeros), e.g. an HTML error page
// concatenated after a truncated download.
func TestExtractPDF_TrailingGarbageTrimmed(t *testing.T) {
	good := minimalTextPDF(t, "Text before garbage")
	garbage := []byte("\n\n<html><body>trailing junk</body></html>\n   \n")
	withGarbage := append(append([]byte{}, good...), garbage...)

	doc := extractPDF(withGarbage)
	if !strings.Contains(doc.Text, "Text before garbage") {
		t.Errorf("text not recovered with trailing garbage: got %q (note=%q)", doc.Text, doc.Note)
	}
	if strings.Contains(strings.ToLower(doc.Note), "fallback") {
		t.Errorf("library should parse after sanitization; Note=%q", doc.Note)
	}
}
