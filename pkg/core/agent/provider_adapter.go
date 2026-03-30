package agent

import (
	"context"
	"fmt"

	providers "github.com/kyd-w/installclaw/pkg/providers"
	protocoltypes "github.com/kyd-w/installclaw/pkg/providers/protocoltypes"
)

// ProviderAdapter adapts pkg/providers.LLMProvider to agent.AIProvider
type ProviderAdapter struct {
	provider providers.LLMProvider
	model    string
}

// NewProviderAdapter creates a new provider adapter
func NewProviderAdapter(p providers.LLMProvider, model string) *ProviderAdapter {
	if model == "" && p != nil {
		model = p.GetDefaultModel()
	}
	return &ProviderAdapter{
		provider: p,
		model:    model,
	}
}

// Query sends a simple prompt
func (a *ProviderAdapter) Query(ctx context.Context, prompt string) (string, error) {
	messages := []Message{
		{Role: "user", Content: prompt},
	}
	return a.QueryWithHistory(ctx, messages)
}

// QueryWithHistory sends a prompt with conversation history
func (a *ProviderAdapter) QueryWithHistory(ctx context.Context, messages []Message) (string, error) {
	if a.provider == nil {
		return "", fmt.Errorf("provider not available")
	}

	// Convert agent.Message to protocoltypes.Message
	protoMessages := make([]protocoltypes.Message, len(messages))
	for i, msg := range messages {
		protoMsg := protocoltypes.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle tool calls from assistant
		if len(msg.ToolCalls) > 0 {
			protoMsg.ToolCalls = make([]protocoltypes.ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				protoMsg.ToolCalls[j] = protocoltypes.ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: &protocoltypes.FunctionCall{
						Name:      tc.Name,
						Arguments: fmt.Sprintf("%v", tc.Arguments),
					},
				}
			}
		}

		// Handle tool result (role: tool)
		if msg.ToolResult != nil {
			protoMsg.ToolCallID = msg.ToolResult.ToolCallID
			if msg.ToolResult.Success {
				protoMsg.Content = msg.ToolResult.Output
			} else {
				protoMsg.Content = "Error: " + msg.ToolResult.Error
			}
		}

		protoMessages[i] = protoMsg
	}

	resp, err := a.provider.Chat(ctx, protoMessages, nil, a.model, nil)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// IsAvailable checks if the provider is ready
func (a *ProviderAdapter) IsAvailable() bool {
	return a.provider != nil
}

// Name returns the provider name
func (a *ProviderAdapter) Name() string {
	return "llm"
}

// GetModel returns the current model
func (a *ProviderAdapter) GetModel() string {
	return a.model
}
