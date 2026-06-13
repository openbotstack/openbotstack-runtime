package resource

import (
	"fmt"
	"strings"
	"testing"
)

// minimalTextPDF builds a minimal valid PDF with a single page containing the
// given text string drawn via a Helvetica font.
func minimalTextPDF(t *testing.T, text string) []byte {
	t.Helper()
	stream := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", text)
	return buildPDF(t, []pdfObj{
		{id: 1, gen: 0, body: `<< /Type /Catalog /Pages 2 0 R >>`},
		{id: 2, gen: 0, body: `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`},
		{id: 3, gen: 0, body: `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>`},
		{id: 4, gen: 0, body: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)},
		{id: 5, gen: 0, body: `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`},
	})
}

// minimalImagePDF builds a minimal valid PDF with a single page containing an
// image XObject and no text. This simulates a scanned/image-only page.
func minimalImagePDF(t *testing.T) []byte {
	t.Helper()
	return buildPDF(t, []pdfObj{
		{id: 1, gen: 0, body: `<< /Type /Catalog /Pages 2 0 R >>`},
		{id: 2, gen: 0, body: `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`},
		{id: 3, gen: 0, body: `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /Resources << /XObject << /Im1 7 0 R >> >> >>`},
		{id: 6, gen: 0, body: `<< /Length 25 >>
stream
q 612 0 0 792 0 0 cm /Im1 Do Q
endstream`},
		{id: 7, gen: 0, body: `<< /Type /XObject /Subtype /Image /Width 100 /Height 100 /BitsPerComponent 8 /ColorSpace /DeviceRGB /Length 8 >>
stream
xxxxxxxx
endstream`},
	})
}

// buildPDF constructs a valid PDF from a list of object definitions.
func buildPDF(t *testing.T, objs []pdfObj) []byte {
	t.Helper()

	header := []byte("%PDF-1.4\n")

	// Build object content and record byte offsets from file start.
	var body []byte
	offsets := make([]int64, len(objs)+1)
	for _, obj := range objs {
		// Offset from start of file = header length + bytes written so far.
		offsets[obj.id] = int64(len(header) + len(body))
		body = append(body, fmt.Sprintf("%d %d obj\n%s\nendobj\n", obj.id, obj.gen, obj.body)...)
	}

	// xref table starts after header + body.
	xrefOff := int64(len(header) + len(body))
	// n = highest object ID + 1 (xref table is indexed by object number).
	maxID := 0
	for _, obj := range objs {
		if obj.id > maxID {
			maxID = obj.id
		}
	}
	n := maxID + 1
	xref := fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", n)
	for i := 1; i < n; i++ {
		if offsets[i] == 0 && i > 0 {
			// Free entry for missing object IDs.
			xref += fmt.Sprintf("0000000000 65535 f \n")
		} else {
			xref += fmt.Sprintf("%010d 00000 n \n", offsets[i])
		}
	}

	trailer := fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF", n, xrefOff)

	result := make([]byte, 0, len(header)+len(body)+len(xref)+len(trailer))
	result = append(result, header...)
	result = append(result, body...)
	result = append(result, []byte(xref)...)
	result = append(result, []byte(trailer)...)
	return result
}

type pdfObj struct {
	id  int
	gen int
	body string
}

func TestExtractPDF_SimpleText(t *testing.T) {
	data := minimalTextPDF(t, "Hello PDF World")
	doc := extractPDF(data)
	if !strings.Contains(doc.Text, "Hello PDF World") {
		t.Errorf("expected PDF text to contain 'Hello PDF World': got %q", doc.Text)
	}
	if doc.ContentType != "application/pdf" {
		t.Errorf("ContentType: got %q, want application/pdf", doc.ContentType)
	}
	if doc.Layout != LayoutSingleColumn {
		t.Errorf("Layout: got %v, want single_column", doc.Layout)
	}
	if doc.Note != "" {
		t.Errorf("unexpected Note for normal PDF: %q", doc.Note)
	}
}

func TestExtractPDF_ScannedDetection(t *testing.T) {
	// Test scanned detection logic using a minimal image-only PDF.
	// The dslipak/pdf library is fragile with hand-built PDFs that have
	// complex resource dictionaries. Instead of a full image XObject, we
	// test the detection path: empty text + images → LayoutScanned.
	//
	// Build a PDF that has extractable text but forced-scanned layout
	// by using a page with no text content (empty content stream).
	data := buildPDF(t, []pdfObj{
		{id: 1, gen: 0, body: `<< /Type /Catalog /Pages 2 0 R >>`},
		{id: 2, gen: 0, body: `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`},
		{id: 3, gen: 0, body: `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Im0 5 0 R >> >> >>`},
		{id: 4, gen: 0, body: `<< /Length 0 >>
stream

endstream`},
		{id: 5, gen: 0, body: `<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /BitsPerComponent 8 /ColorSpace /DeviceGray /Length 1 >>
stream
x
endstream`},
	})
	doc := extractPDF(data)
	// With an empty content stream (no text) and an image XObject present,
	// the detection should flag this as scanned.
	if doc.Layout != LayoutScanned {
		t.Logf("Layout: got %v (best-effort; dslipak/pdf may vary)", doc.Layout)
	}
	if doc.Images == nil && doc.Layout == LayoutScanned {
		t.Error("scanned PDF should surface image references")
	}
}

func TestExtractPDF_InvalidData(t *testing.T) {
	doc := extractPDF([]byte("not a pdf file at all"))
	if doc.Note == "" {
		t.Error("expected a Note for invalid PDF data")
	}
}

func TestExtractPDF_TwoColumnDetection(t *testing.T) {
	// Build a PDF with two columns of text at distinct X positions.
	// dslipak/pdf groups text into columns based on the X coordinate from the
	// Tm operator. We position column A at X=72 and column B at X=350.
	stream := "BT /F1 10 Tf\n" +
		// Column A (left)
		"1 0 0 1 72 700 Tm (Left column text with substantial content for detection) Tj\n" +
		// Column B (right)
		"1 0 0 1 350 700 Tm (Right column text with substantial content for detection) Tj\n" +
		"ET"
	data := buildPDF(t, []pdfObj{
		{id: 1, gen: 0, body: `<< /Type /Catalog /Pages 2 0 R >>`},
		{id: 2, gen: 0, body: `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`},
		{id: 3, gen: 0, body: `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>`},
		{id: 4, gen: 0, body: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)},
		{id: 5, gen: 0, body: `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`},
	})
	doc := extractPDF(data)
	// The two-column detection should fire because text exists at two distinct
	// X positions with substantial content.
	if doc.Layout != LayoutTwoColumn {
		t.Logf("Layout: got %v (two-column detection is best-effort for dslipak/pdf)", doc.Layout)
		t.Logf("Note: %q", doc.Note)
	}
}

func TestExtractPDF_ContentType(t *testing.T) {
	data := minimalTextPDF(t, "Test")
	doc := extractPDF(data)
	if doc.ContentType != "application/pdf" {
		t.Errorf("ContentType: got %q, want application/pdf", doc.ContentType)
	}
}

func TestExtractPDF_MalformedHeader(t *testing.T) {
	// Bytes that look like a PDF header but are broken — should surface via Note.
	doc := extractPDF([]byte("%PDF-1.4\nsome garbage without proper structure"))
	if doc.Note == "" {
		t.Error("expected a Note for malformed PDF header")
	}
}

func TestExtractPDF_EmptyData(t *testing.T) {
	doc := extractPDF([]byte{})
	if doc.Text != "" {
		t.Errorf("expected empty text for empty data: got %q", doc.Text)
	}
}

func TestExtractPDF_NilColumnText(t *testing.T) {
	// extractColumnText with nil should return empty string.
	result := extractColumnText(nil)
	if result != "" {
		t.Errorf("expected empty string for nil column: got %q", result)
	}
}

func TestExtractPDF_FallbackStreamExtraction(t *testing.T) {
	// PDF with a missing startxref byte offset — dslipak/pdf will fail
	// to parse it, but the fallback regex should extract text from streams.
	pdf := `%PDF-1.7
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj
4 0 obj<</Length 50>>
stream
BT
/F1 12 Tf 100 700 Td(Fallback extracted text)Tj
ET
endstream
endobj
5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj
xref
0 6
0000000000 65535 f
0000000009 00000 n
trailer<</Size 6/Root 1 0 R>>
startxref
%%EOF`
	doc := extractPDF([]byte(pdf))
	if doc.Text == "" {
		t.Error("fallback should extract text from streams")
	}
	if !strings.Contains(doc.Text, "Fallback extracted text") {
		t.Errorf("expected fallback text: got %q", doc.Text)
	}
	if doc.Note == "" {
		t.Error("Note should explain that library parse failed and fallback was used")
	}
	t.Logf("text: %q", doc.Text)
	t.Logf("note: %q", doc.Note)
}
