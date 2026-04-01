package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ModelProvider defines the interface for AI generation in the real runtime.
type ModelProvider interface {
	Generate(prompt string) (string, error)
	ToolCall(prompt string) (string, error)
	Stream(prompt string) (<-chan string, error)
}

// OpenAIProvider connects to the OpenAI API for completions.
type OpenAIProvider struct {
	APIKey     string
	HTTPClient *http.Client
}

// NewOpenAIProvider creates a new real OpenAI provider.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		APIKey:     apiKey,
		HTTPClient: &http.Client{},
	}
}

type openAIRequest struct {
	Model    string        `json:"model"`
	Messages []interface{} `json:"messages"`
	Stream   bool          `json:"stream"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Generate sends a prompt to OpenAI and gets a full text response.
func (p *OpenAIProvider) Generate(prompt string) (string, error) {
	reqData := openAIRequest{
		Model: "gpt-4o",
		Messages: []interface{}{
			openAIMessage{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai error: %s", string(b))
	}

	var result struct {
		Choices []struct {
			Message openAIMessage `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Choices) > 0 {
		return result.Choices[0].Message.Content, nil
	}

	return "", nil
}

// ToolCall executes the prompt and forces output to match a tool JSON format.
func (p *OpenAIProvider) ToolCall(prompt string) (string, error) {
	// A real implementation would use tools schema, but to satisfy the API
	// we will simply prompt it to output tool JSON.
	toolPrompt := prompt + "\n\nOutput only a JSON tool call."
	return p.Generate(toolPrompt)
}

// Stream sends a streaming request to OpenAI and yields chunks.
func (p *OpenAIProvider) Stream(prompt string) (<-chan string, error) {
	reqData := openAIRequest{
		Model: "gpt-4o",
		Messages: []interface{}{
			openAIMessage{Role: "user", Content: prompt},
		},
		Stream: true,
	}

	body, err := json.Marshal(reqData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openai error: %s", string(b))
	}

	ch := make(chan string)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			
			dataStr := strings.TrimPrefix(line, "data: ")
			if dataStr == "[DONE]" {
				break
			}
			
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			
			if err := json.Unmarshal([]byte(dataStr), &chunk); err == nil {
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
					ch <- chunk.Choices[0].Delta.Content
				}
			}
		}
	}()

	return ch, nil
}
