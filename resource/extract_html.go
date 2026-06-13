package resource

import (
	"bytes"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
)

// extractHTML parses an HTML page and returns only the readable article text
// (not the raw DOM). It uses go-readability to strip nav/script/footer
// boilerplate and recover title + byline metadata.
func extractHTML(data []byte) Document {
	doc := Document{
		ContentType: ContentTypeHTML,
		Layout:      LayoutSingleColumn,
	}

	// pageURL is required by FromReader but we only need it for relative-link
	// resolution; a placeholder is fine when the source URL isn't known here.
	article, err := readability.FromReader(bytes.NewReader(data), &url.URL{})
	if err != nil {
		// If readability fails, fall back to a naive tag strip so the caller
		// still gets something usable rather than an empty Document.
		doc.Text = stripHTMLTags(string(data))
		doc.Note = "HTML readability parse failed; used naive tag stripping"
		return doc
	}

	var buf bytes.Buffer
	_ = article.RenderText(&buf)
	doc.Text = strings.TrimSpace(buf.String())
	doc.Title = article.Title()
	if byline := article.Byline(); byline != "" {
		if doc.Metadata == nil {
			doc.Metadata = map[string]string{}
		}
		doc.Metadata["byline"] = byline
	}
	if lang := article.Language(); lang != "" {
		doc.Language = lang
	}

	// If readability extracted nothing useful (e.g. the page is mostly app
	// shell), fall back to tag stripping and warn.
	if doc.Text == "" {
		doc.Text = stripHTMLTags(string(data))
		doc.Note = "readability extracted no article text; used naive tag stripping"
	}
	return doc
}

// stripHTMLTags is a minimal fallback that removes <...> sequences and collapses
// whitespace. It is intentionally crude — readability is the primary path.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
