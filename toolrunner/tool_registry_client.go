package toolrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// RegistryClient communicates with the OpenBotStack Tool Registry.
type RegistryClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(baseURL string) *RegistryClient {
	return &RegistryClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Invoke calls a tool and returns the response bytes.
func (c *RegistryClient) Invoke(ctx *ToolContext, toolName string, arguments map[string]any) ([]byte, error) {
	// Build request body
	body := map[string]any{
		"tool":      toolName,
		"arguments": arguments,
		"meta": map[string]string{
			"tenant_id":  ctx.TenantID,
			"user_id":    ctx.UserID,
			"request_id": ctx.RequestID,
		},
	}
	
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tool request: %w", err)
	}
	
	url := fmt.Sprintf("%s/invoke", c.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tool invocation failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tool registry returned error: %s", resp.Status)
	}
	
	// Read response
	var result struct {
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode tool response: %w", err)
	}
	
	if result.Error != "" {
		return nil, fmt.Errorf("tool error: %s", result.Error)
	}
	
	return result.Output, nil
}
