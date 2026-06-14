package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResourceReadTool_Metadata(t *testing.T) {
	tool := &ResourceReadTool{}
	if tool.Name() != "resource_read" {
		t.Errorf("Name: got %q, want resource_read", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	params := tool.Parameters()
	if params["url"] != "string" {
		t.Errorf("expected url param, got %v", params)
	}
	req := tool.Required()
	if len(req) != 1 || req[0] != "url" {
		t.Errorf("Required: got %v, want [url]", req)
	}
	perms := tool.Permissions()
	if len(perms) != 1 || perms[0] != "http.fetch" {
		t.Errorf("Permissions: got %v, want [http.fetch]", perms)
	}
}

func TestResourceReadTool_MissingURL(t *testing.T) {
	tool := &ResourceReadTool{Timeout: time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing url")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Errorf("error: %v", err)
	}
}

func TestResourceReadTool_InvalidScheme(t *testing.T) {
	tool := &ResourceReadTool{Timeout: time.Second}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "ftp://example.com/doc.pdf"})
	if err == nil {
		t.Fatal("expected error for ftp scheme")
	}
	if !strings.Contains(err.Error(), "http://") {
		t.Errorf("error: %v", err)
	}
}

func TestResourceReadTool_FetchPlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Hello, resource_read!"))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["text"] != "Hello, resource_read!" {
		t.Errorf("text: got %q", result["text"])
	}
	if ct, _ := result["content_type"].(string); ct != "text/plain" {
		t.Errorf("content_type: got %q, want text/plain", ct)
	}
	if result["id"] == "" || result["id"] == nil {
		t.Error("id should not be empty")
	}
	if src, _ := result["source"].(string); src != srv.URL {
		t.Errorf("source: got %q, want %q", src, srv.URL)
	}
}

func TestResourceReadTool_FetchHTML(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Test Article</title></head>
<body><nav>Skip this</nav><article><h1>Headline</h1><p>Article body with readable content.</p></article></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, _ := result["text"].(string)
	if !strings.Contains(text, "Article body") {
		t.Errorf("expected article body in text: got %q", text)
	}
	// Navigation should be stripped.
	if strings.Contains(text, "Skip this") {
		t.Errorf("nav text should be stripped: %q", text)
	}
}

func TestResourceReadTool_SSRFProtection(t *testing.T) {
	// Create a test server on loopback but do NOT set allowPrivateIPs = true.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("should not reach"))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         2 * time.Second,
		MaxBytes:        1024,
		allowPrivateIPs: false, // SSRF protection ON
	}

	_, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err == nil {
		t.Fatal("expected SSRF block error")
	}
	if !strings.Contains(err.Error(), "private network") {
		t.Errorf("error should mention private network: %v", err)
	}
}

func TestResourceReadTool_SSRFProtection_ExplicitLocalhost(t *testing.T) {
	tool := &ResourceReadTool{
		Timeout:         2 * time.Second,
		MaxBytes:        1024,
		allowPrivateIPs: false,
	}
	_, err := tool.Execute(context.Background(), map[string]any{"url": "http://127.0.0.1:1/"})
	if err == nil {
		t.Fatal("expected SSRF block error for 127.0.0.1")
	}
}

func TestResourceReadTool_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         50 * time.Millisecond,
		MaxBytes:        1024,
		allowPrivateIPs: true,
	}

	_, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestResourceReadTool_MaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 2048))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024,
		allowPrivateIPs: true,
	}

	// Oversize body no longer errors out — instead, truncated=true propagates
	// to the Document so the caller knows content was cut.
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error (oversize should succeed with truncated=true): %v", err)
	}
	truncated, _ := result["truncated"].(bool)
	if !truncated {
		t.Error("truncated should be true when body exceeds MaxBytes")
	}
}

func TestResourceReadTool_NonUTF8Handling(t *testing.T) {
	// Serve bytes that are not valid UTF-8.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte{0xff, 0xfe, 0x00, 'a', 'b', 'c'})
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Non-UTF-8 text should be sanitized; Note should warn.
	note, _ := result["note"].(string)
	if note == "" {
		t.Error("expected non-empty Note for non-UTF-8 content")
	}
}

func TestResourceReadTool_DocumentToMap_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"key":"value","nested":{"a":1}}`))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Round-trip through JSON to verify the map is valid.
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("result not JSON-marshalable: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("result not JSON-unmarshalable: %v", err)
	}
	if back["source"] != srv.URL {
		t.Errorf("source lost in round-trip")
	}
}

func TestResourceReadTool_TruncatedFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("short body"))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024, // much larger than body
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	truncated, _ := result["truncated"].(bool)
	if truncated {
		t.Error("truncated should be false when body is under limit")
	}
}

func TestResourceReadTool_ImageContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// Minimal PNG header
		png := []byte{137, 80, 78, 71, 13, 10, 26, 10}
		w.Write(png)
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return empty text, image content type, and a Note about vision.
	if text, _ := result["text"].(string); text != "" {
		t.Errorf("text should be empty for images: got %q", text)
	}
	if ct, _ := result["content_type"].(string); ct != "image/png" {
		t.Errorf("content_type: got %q, want image/png", ct)
	}
	note, _ := result["note"].(string)
	if !strings.Contains(note, "vision") {
		t.Errorf("Note should mention vision: %q", note)
	}
	// Document.ToMap emits images as []map[string]any (preserves typed intent,
	// unlike the old JSON round-trip which produced []interface{}).
	images, ok := result["images"].([]map[string]any)
	if !ok {
		t.Fatalf("images should be []map[string]any, got %T", result["images"])
	}
	if len(images) == 0 {
		t.Error("images should not be empty for image documents")
	}
}

func TestResourceReadTool_DispatchRegistration(t *testing.T) {
	runner := NewBuiltinToolRunner()
	result, err := runner.Run(context.Background(), "resource_read", map[string]any{"url": "invalid"})
	// "invalid" doesn't start with http://, so it should error.
	if err == nil {
		t.Fatal("expected error for invalid URL through runner dispatch")
	}
	_ = result
}

func TestResourceReadTool_ContentTypeWithCharset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("content with charset param"))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Charset parameter should be stripped.
	if ct, _ := result["content_type"].(string); ct != "text/plain" {
		t.Errorf("content_type should strip charset: got %q", ct)
	}
}

func TestResourceReadTool_MarkdownPreserved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		fmt.Fprint(w, "# Heading\n\nParagraph text here.")
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text, _ := result["text"].(string)
	if !strings.Contains(text, "Heading") {
		t.Errorf("markdown heading lost: %q", text)
	}
	if !strings.Contains(text, "Paragraph") {
		t.Errorf("markdown paragraph lost: %q", text)
	}
}

func TestResourceReadTool_ContentAlias(t *testing.T) {
	// Verify the "content" alias is present and mirrors "text".
	// The planner uses {{builtin.resource_read.content}} — this must work.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("alias test content"))
	}))
	defer srv.Close()

	tool := &ResourceReadTool{
		Timeout:         5 * time.Second,
		MaxBytes:        1024 * 1024,
		allowPrivateIPs: true,
	}

	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text, _ := result["text"].(string)
	content, _ := result["content"].(string)
	if text != content {
		t.Errorf("content alias must mirror text: text=%q content=%q", text, content)
	}
	if content != "alias test content" {
		t.Errorf("content alias: got %q, want %q", content, "alias test content")
	}
}
