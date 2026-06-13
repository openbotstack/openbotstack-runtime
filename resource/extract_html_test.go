package resource

import (
	"strings"
	"testing"
)

func TestExtractHTML_ArticleText(t *testing.T) {
	// A minimal HTML page with nav/boilerplate that must be stripped, leaving
	// only the article text + a parsed title.
	html := `<!DOCTYPE html><html><head><title>Real Title</title></head>
<body><nav>Home About</nav><article><h1>Heading</h1><p>This is the article body text.</p></article></body></html>`

	doc := extractHTML([]byte(html))
	if !strings.Contains(doc.Text, "article body text") {
		t.Errorf("article text not extracted; got: %q", doc.Text)
	}
	if strings.Contains(doc.Text, "Home About") {
		t.Errorf("nav boilerplate leaked into text: %q", doc.Text)
	}
	if doc.Title != "Real Title" && doc.Title != "Heading" {
		t.Errorf("title not parsed; got %q", doc.Title)
	}
	if doc.ContentType != "text/html" {
		t.Errorf("ContentType: got %q, want text/html", doc.ContentType)
	}
}

func TestExtractHTML_BoilerplateStripped(t *testing.T) {
	html := `<html><head><title>T</title><script>alert(1)</script></head>
<body><script>x()</script><p>visible content</p><footer>copyright</footer></body></html>`
	doc := extractHTML([]byte(html))
	if strings.Contains(doc.Text, "alert") || strings.Contains(doc.Text, "x()") {
		t.Errorf("script content leaked: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "visible content") {
		t.Errorf("visible content lost: %q", doc.Text)
	}
}

func TestExtractHTML_BadHTMLFallback(t *testing.T) {
	// HTML so broken that readability can't parse it — must fall back to naive stripping.
	html := `<html><body><p>fallback text</p>`
	doc := extractHTML([]byte(html))
	if doc.Text == "" {
		t.Error("expected fallback text even with broken HTML")
	}
	if !strings.Contains(doc.Text, "fallback text") {
		t.Errorf("fallback lost content: %q", doc.Text)
	}
}

func TestExtractHTML_EmptyArticleFallback(t *testing.T) {
	// Empty page — readability should extract nothing, fallback applies.
	html := `<html><head><title>Empty</title></head><body></body></html>`
	doc := extractHTML([]byte(html))
	if doc.Note == "" {
		t.Error("expected a Note when readability extracts no article text")
	}
}

func TestExtractHTML_WithBylineAndLanguage(t *testing.T) {
	// HTML with byline and language metadata.
	html := `<!DOCTYPE html><html lang="zh-CN"><head><title>Article Title</title></head>
<body><article><address>Author Name</address><p>Content with byline and language metadata.</p></article></body></html>`
	doc := extractHTML([]byte(html))
	if !strings.Contains(doc.Text, "Content with byline") {
		t.Errorf("article content lost: %q", doc.Text)
	}
	if doc.Title == "" {
		t.Error("title should be extracted")
	}
}

func TestExtractHTML_GarbledInput(t *testing.T) {
	// Completely garbled input — readability will likely fail.
	doc := extractHTML([]byte("not html at all just random text"))
	if doc.Text == "" {
		t.Error("fallback should produce text even from garbled input")
	}
}
