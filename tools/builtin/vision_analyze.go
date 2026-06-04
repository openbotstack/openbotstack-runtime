package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// VisionAnalyzeTool analyzes images using a vision-capable LLM.
// It sends an image URL along with an instruction to the LLM and returns
// the analysis result.
type VisionAnalyzeTool struct {
	llmAccess LLMAccess
}

// SetLLMAccess injects the LLM access for vision analysis.
// Implements the LLMAwareTool optional interface.
func (t *VisionAnalyzeTool) SetLLMAccess(access LLMAccess) {
	t.llmAccess = access
}

func (t *VisionAnalyzeTool) Name() string { return "vision_analyze" }

func (t *VisionAnalyzeTool) Description() string {
	return "Analyzes an image using a vision-capable LLM. Provide an image URL and optional instruction."
}

func (t *VisionAnalyzeTool) Parameters() map[string]string {
	return map[string]string{
		"image_url":   "string",
		"instruction": "string",
	}
}

func (t *VisionAnalyzeTool) Required() []string {
	return []string{"image_url"}
}

func (t *VisionAnalyzeTool) Permissions() []string {
	return []string{"vision.analyze"}
}

func (t *VisionAnalyzeTool) Execute(ctx context.Context, input map[string]any) (map[string]any, error) {
	// Validate image_url is present.
	imageURL, _ := input["image_url"].(string)
	if strings.TrimSpace(imageURL) == "" {
		return nil, fmt.Errorf("vision_analyze: image_url is required")
	}

	// Validate URL format.
	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return nil, fmt.Errorf("vision_analyze: invalid image_url: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("vision_analyze: image_url must use http:// or https:// scheme")
	}

	// SSRF protection: block private/internal IPs.
	host := parsedURL.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("vision_analyze: DNS resolution failed for %q: %w", host, err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, fmt.Errorf("vision_analyze: access to private network addresses is blocked (%s)", ip.IP)
		}
	}

	// Check that LLM access is available.
	if t.llmAccess == nil {
		return nil, fmt.Errorf("vision_analyze: LLM access not configured")
	}

	// Default instruction.
	instruction := "Describe the image in detail."
	if instr, ok := input["instruction"].(string); ok && strings.TrimSpace(instr) != "" {
		instruction = instr
	}

	// Build the LLM request with text + image content blocks.
	req := LLMRequest{
		SystemPrompt: "You are a vision analysis assistant. Analyze the provided image and respond to the user's instruction.",
		Contents: []ContentBlock{
			NewTextBlock(instruction),
			NewImageBlock(imageURL),
		},
		MaxTokens:   1024,
		Temperature: 0.3,
	}

	resp, err := t.llmAccess.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("vision_analyze: LLM generation failed: %w", err)
	}

	result := map[string]any{
		"image_url":            imageURL,
		"description":          resp.Content,
		"model_used":           resp.ModelUsed,
		"tokens_used":          resp.Usage.TotalTokens,
		"analysis_duration_ms": resp.Latency.Milliseconds(),
	}

	// Attempt to parse structured JSON from the response.
	var structured map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &structured); err == nil {
		result["structured_data"] = structured
	}

	return result, nil
}
