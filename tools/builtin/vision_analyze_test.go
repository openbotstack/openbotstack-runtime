package builtin

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"
)

// mockLLMAccess is a test double for the LLMAccess interface.
type mockLLMAccess struct {
	generateFn func(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

func (m *mockLLMAccess) Generate(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, req)
	}
	return nil, fmt.Errorf("mockLLMAccess: Generate not configured")
}

func TestVisionAnalyzeTool_Metadata(t *testing.T) {
	tool := &VisionAnalyzeTool{}

	if tool.Name() != "vision_analyze" {
		t.Errorf("Name() = %q, want %q", tool.Name(), "vision_analyze")
	}
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}

	params := tool.Parameters()
	if _, ok := params["image_url"]; !ok {
		t.Error("Parameters() missing image_url")
	}
	if _, ok := params["instruction"]; !ok {
		t.Error("Parameters() missing instruction")
	}

	required := tool.Required()
	found := false
	for _, r := range required {
		if r == "image_url" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Required() should contain image_url")
	}

	perms := tool.Permissions()
	if len(perms) != 1 || perms[0] != "vision.analyze" {
		t.Errorf("Permissions() = %v, want [vision.analyze]", perms)
	}
}

func TestVisionAnalyzeTool_MissingImageURL(t *testing.T) {
	tool := &VisionAnalyzeTool{}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when image_url is missing")
	}
}

func TestVisionAnalyzeTool_DefaultInstruction(t *testing.T) {
	tool := &VisionAnalyzeTool{}
	tool.SetLLMAccess(&mockLLMAccess{
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			// Verify the instruction text block was included with the default.
			if len(req.Contents) < 2 {
				t.Fatalf("expected at least 2 content blocks, got %d", len(req.Contents))
			}
			textBlock := req.Contents[0]
			if textBlock.Type != "text" {
				t.Errorf("first block type = %q, want %q", textBlock.Type, "text")
			}
			if textBlock.Text != "Describe the image in detail." {
				t.Errorf("default instruction = %q, want %q", textBlock.Text, "Describe the image in detail.")
			}
			return &LLMResponse{
				Content:   `{"description":"test"}`,
				Usage:     TokenUsage{TotalTokens: 10},
				ModelUsed: "test-model",
				Latency:   50 * time.Millisecond,
			}, nil
		},
	})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_url": "https://example.com/photo.png",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVisionAnalyzeTool_SuccessfulAnalysis(t *testing.T) {
	testURL := "https://example.com/test-image.png"
	expectedDescription := "A beautiful sunset over the ocean."

	tool := &VisionAnalyzeTool{}
	var capturedReq LLMRequest

	tool.SetLLMAccess(&mockLLMAccess{
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			capturedReq = req
			return &LLMResponse{
				Content:   expectedDescription,
				Usage:     TokenUsage{PromptTokens: 50, CompletionTokens: 20, TotalTokens: 70},
				ModelUsed: "vision-model",
				Latency:   100 * time.Millisecond,
			}, nil
		},
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_url":   testURL,
		"instruction": "What do you see in this image?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the LLM was called with correct content blocks.
	if len(capturedReq.Contents) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(capturedReq.Contents))
	}
	if capturedReq.Contents[0].Type != "text" {
		t.Errorf("first block type = %q, want text", capturedReq.Contents[0].Type)
	}
	if capturedReq.Contents[0].Text != "What do you see in this image?" {
		t.Errorf("instruction text = %q, want %q", capturedReq.Contents[0].Text, "What do you see in this image?")
	}
	if capturedReq.Contents[1].Type != "image" {
		t.Errorf("second block type = %q, want image", capturedReq.Contents[1].Type)
	}
	if capturedReq.Contents[1].ImageURL != testURL {
		t.Errorf("image URL = %q, want %q", capturedReq.Contents[1].ImageURL, testURL)
	}

	// Verify output fields.
	if result["description"] != expectedDescription {
		t.Errorf("description = %q, want %q", result["description"], expectedDescription)
	}
	if result["model_used"] != "vision-model" {
		t.Errorf("model_used = %q, want %q", result["model_used"], "vision-model")
	}
	if _, ok := result["image_url"]; !ok {
		t.Error("output missing image_url")
	}
	if _, ok := result["tokens_used"]; !ok {
		t.Error("output missing tokens_used")
	}
}

func TestVisionAnalyzeTool_InvalidURL(t *testing.T) {
	// Set up a mock LLM so valid URLs pass all validation and succeed.
	mock := &mockLLMAccess{
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			return &LLMResponse{
				Content:   "ok",
				Usage:     TokenUsage{TotalTokens: 5},
				ModelUsed: "test",
				Latency:   10 * time.Millisecond,
			}, nil
		},
	}

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty string", "", true},
		{"no scheme", "example.com/image.png", true},
		{"ftp scheme", "ftp://example.com/image.png", true},
		{"valid https", "https://example.com/image.png", false},
		{"valid http", "http://example.com/image.png", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &VisionAnalyzeTool{}
			tool.SetLLMAccess(mock)
			_, err := tool.Execute(context.Background(), map[string]any{
				"image_url": tt.url,
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("url=%q error=%v, wantErr=%v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestVisionAnalyzeTool_SetLLMAccess(t *testing.T) {
	tool := &VisionAnalyzeTool{}

	// Verify the tool implements LLMAwareTool interface.
	var _ LLMAwareTool = tool

	// Verify it starts nil.
	if tool.llmAccess != nil {
		t.Error("llmAccess should start nil")
	}

	// Verify SetLLMAccess works.
	mock := &mockLLMAccess{}
	tool.SetLLMAccess(mock)
	if tool.llmAccess == nil {
		t.Error("llmAccess should not be nil after SetLLMAccess")
	}
}

func TestVisionAnalyzeTool_NoLLMAccess(t *testing.T) {
	tool := &VisionAnalyzeTool{}
	// Do NOT call SetLLMAccess -- llmAccess should be nil.
	_, err := tool.Execute(context.Background(), map[string]any{
		"image_url": "https://example.com/photo.png",
	})
	if err == nil {
		t.Fatal("expected error when LLMAccess is not configured")
	}
}

func TestVisionAnalyzeTool_URLValidation(t *testing.T) {
	// Test that a valid URL parses correctly (internal helper).
	testURL := "https://example.com/photo.png"
	parsed, err := url.Parse(testURL)
	if err != nil {
		t.Fatalf("valid URL should parse: %v", err)
	}
	if parsed.Scheme != "https" {
		t.Errorf("scheme = %q, want https", parsed.Scheme)
	}
}

func TestVisionAnalyzeTool_CustomInstructionPassedToLLM(t *testing.T) {
	customInstruction := "Identify all objects and their colors in this image."
	var capturedReq LLMRequest

	tool := &VisionAnalyzeTool{}
	tool.SetLLMAccess(&mockLLMAccess{
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			capturedReq = req
			return &LLMResponse{
				Content:   "Objects found: red car, blue sky",
				Usage:     TokenUsage{TotalTokens: 30},
				ModelUsed: "test-model",
				Latency:   50 * time.Millisecond,
			}, nil
		},
	})

	_, err := tool.Execute(context.Background(), map[string]any{
		"image_url":   "https://example.com/test.jpg",
		"instruction": customInstruction,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedReq.Contents[0].Text != customInstruction {
		t.Errorf("instruction passed to LLM = %q, want %q", capturedReq.Contents[0].Text, customInstruction)
	}
}

// Ensure JSON response parsing works for structured output.
func TestVisionAnalyzeTool_JSONResponseParsing(t *testing.T) {
	jsonResponse := `{"objects": ["cat", "dog"], "scene": "living room"}`

	tool := &VisionAnalyzeTool{}
	tool.SetLLMAccess(&mockLLMAccess{
		generateFn: func(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
			return &LLMResponse{
				Content:   jsonResponse,
				Usage:     TokenUsage{TotalTokens: 40},
				ModelUsed: "test-model",
				Latency:   50 * time.Millisecond,
			}, nil
		},
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"image_url": "https://example.com/test.jpg",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the raw content is returned.
	if result["description"] != jsonResponse {
		t.Errorf("description = %q, want %q", result["description"], jsonResponse)
	}

	// Verify structured_data is parsed when response is valid JSON.
	structuredData, ok := result["structured_data"]
	if !ok {
		t.Fatal("expected structured_data in output for JSON response")
	}
	parsed, ok := structuredData.(map[string]any)
	if !ok {
		t.Fatalf("structured_data type = %T, want map[string]any", structuredData)
	}
	if parsed["scene"] != "living room" {
		t.Errorf("structured_data.scene = %v, want 'living room'", parsed["scene"])
	}
}
