// Package openai_compat provides an OpenAI-compatible API provider
package openai_compat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	protocoltypes "github.com/kyd-w/installclaw/pkg/providers/protocoltypes"
)

// Provider implements an OpenAI-compatible API provider
type Provider struct {
	apiKey         string
	apiBase        string
	proxy          string
	model          string
	maxTokensField string
	requestTimeout time.Duration
	httpClient     *http.Client
}

// Option configures the provider
type Option func(*Provider)

// WithMaxTokensField sets the field name for max tokens
func WithMaxTokensField(field string) Option {
	return func(p *Provider) {
		p.maxTokensField = field
	}
}

// WithRequestTimeout sets the request timeout
func WithRequestTimeout(timeout time.Duration) Option {
	return func(p *Provider) {
		p.requestTimeout = timeout
	}
}

// WithModel sets the default model
func WithModel(model string) Option {
	return func(p *Provider) {
		p.model = model
	}
}

// NewProvider creates a new OpenAI-compatible provider
func NewProvider(apiKey, apiBase, proxy string, opts ...Option) *Provider {
	p := &Provider{
		apiKey:         apiKey,
		apiBase:        apiBase,
		proxy:          proxy,
		requestTimeout: 60 * time.Second,
		httpClient:     &http.Client{},
	}

	for _, opt := range opts {
		opt(p)
	}

	// Configure proxy if specified
	if p.proxy != "" {
		proxyURL, err := url.Parse(p.proxy)
		if err == nil {
			p.httpClient.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return p
}

// Chat sends a chat request to the OpenAI-compatible API
func (p *Provider) Chat(
	ctx context.Context,
	messages []protocoltypes.Message,
	tools []protocoltypes.ToolDefinition,
	model string,
	options map[string]any,
) (*protocoltypes.LLMResponse, error) {
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// Build request body
	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
	}

	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	// Apply options
	for k, v := range options {
		reqBody[k] = v
	}

	// Marshal request
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	reqURL := strings.TrimSuffix(p.apiBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var openaiResp openAIChatResponse
	if err := json.Unmarshal(body, &openaiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to LLMResponse
	llmResp := &protocoltypes.LLMResponse{
		FinishReason: openaiResp.Choices[0].FinishReason,
	}

	if len(openaiResp.Choices) > 0 && openaiResp.Choices[0].Message.Content != "" {
		llmResp.Content = openaiResp.Choices[0].Message.Content
	}

	if len(openaiResp.Choices) > 0 && len(openaiResp.Choices[0].Message.ToolCalls) > 0 {
		llmResp.ToolCalls = make([]protocoltypes.ToolCall, len(openaiResp.Choices[0].Message.ToolCalls))
		for i, tc := range openaiResp.Choices[0].Message.ToolCalls {
			llmResp.ToolCalls[i] = protocoltypes.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: &protocoltypes.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	if openaiResp.Usage != nil {
		llmResp.Usage = &protocoltypes.UsageInfo{
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			TotalTokens:      openaiResp.Usage.TotalTokens,
		}
	}

	return llmResp, nil
}

// StreamChat sends a streaming chat request
func (p *Provider) StreamChat(
	ctx context.Context,
	messages []protocoltypes.Message,
	tools []protocoltypes.ToolDefinition,
	model string,
	options map[string]any,
	onChunk func(chunk string) error,
) error {
	if model == "" {
		model = p.model
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	// Build request body
	reqBody := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}

	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	for k, v := range options {
		reqBody[k] = v
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	reqURL := strings.TrimSuffix(p.apiBase, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			if err := onChunk(chunk.Choices[0].Delta.Content); err != nil {
				return err
			}
		}
	}

	return scanner.Err()
}

// OpenAI response types
type openAIChatResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int           `json:"index"`
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIMessage struct {
	Role      string            `json:"role"`
	Content   string            `json:"content"`
	ToolCalls []openAIToolCall  `json:"tool_calls,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIStreamChunk struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

type openAIDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
