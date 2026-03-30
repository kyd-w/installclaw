package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenAIProvider implements AIProvider for OpenAI
type OpenAIProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// OpenAIConfig contains configuration for OpenAI provider
type OpenAIConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(config *OpenAIConfig) *OpenAIProvider {
	if config == nil {
		config = &OpenAIConfig{}
	}

	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = "gpt-4o-mini" // Default to cost-effective model
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// IsAvailable checks if the provider is configured
func (p *OpenAIProvider) IsAvailable() bool {
	return p.apiKey != ""
}

// Query sends a query to OpenAI
func (p *OpenAIProvider) Query(ctx context.Context, prompt string) (string, error) {
	if !p.IsAvailable() {
		return "", fmt.Errorf("openai: API key not configured")
	}

	reqBody := map[string]interface{}{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3, // Lower temperature for more consistent results
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("openai: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("openai: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("openai: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("openai: API returned status %d", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("openai: failed to decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: no response choices")
	}

	return result.Choices[0].Message.Content, nil
}

// ClaudeProvider implements AIProvider for Anthropic Claude
type ClaudeProvider struct {
	apiKey     string
	model      string
	httpClient *http.Client
	baseURL    string
}

// ClaudeConfig contains configuration for Claude provider
type ClaudeConfig struct {
	APIKey  string
	Model   string
	BaseURL string
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(config *ClaudeConfig) *ClaudeProvider {
	if config == nil {
		config = &ClaudeConfig{}
	}

	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	model := config.Model
	if model == "" {
		model = "claude-3-haiku-20240307" // Default to fast model
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}

	return &ClaudeProvider{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return "claude"
}

// IsAvailable checks if the provider is configured
func (p *ClaudeProvider) IsAvailable() bool {
	return p.apiKey != ""
}

// Query sends a query to Claude
func (p *ClaudeProvider) Query(ctx context.Context, prompt string) (string, error) {
	if !p.IsAvailable() {
		return "", fmt.Errorf("claude: API key not configured")
	}

	reqBody := map[string]interface{}{
		"model":      p.model,
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("claude: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("claude: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return "", fmt.Errorf("claude: %s", errResp.Error.Message)
		}
		return "", fmt.Errorf("claude: API returned status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("claude: failed to decode response: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("claude: no response content")
	}

	return result.Content[0].Text, nil
}

// OllamaProvider implements AIProvider for local Ollama
type OllamaProvider struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// OllamaConfig contains configuration for Ollama provider
type OllamaConfig struct {
	BaseURL string
	Model   string
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(config *OllamaConfig) *OllamaProvider {
	if config == nil {
		config = &OllamaConfig{}
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	model := config.Model
	if model == "" {
		model = "llama3"
	}

	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Local inference may be slower
		},
	}
}

// Name returns the provider name
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// IsAvailable checks if Ollama is running
func (p *OllamaProvider) IsAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// Query sends a query to Ollama
func (p *OllamaProvider) Query(ctx context.Context, prompt string) (string, error) {
	reqBody := map[string]interface{}{
		"model":  p.model,
		"prompt": prompt,
		"stream": false,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/api/generate", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("ollama: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama: API returned status %d", resp.StatusCode)
	}

	var result struct {
		Response string `json:"response"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("ollama: failed to decode response: %w", err)
	}

	return result.Response, nil
}
