package providers

import (
	"fmt"
	"os"
	"strings"
)

// SimpleProviderConfig is a simplified provider configuration
type SimpleProviderConfig struct {
	APIKey  string
	APIBase string
	Proxy  string
	Model   string
}

// SimpleConfig is a simplified configuration for provider factory
type SimpleConfig struct {
	Provider string // Provider name: openai, anthropic, openrouter, etc.
	Model    string
	APIKey   string
	APIBase  string
	Proxy    string

	// Provider-specific configs
	OpenAI     SimpleProviderConfig
	Anthropic  SimpleProviderConfig
	OpenRouter SimpleProviderConfig
	Gemini    SimpleProviderConfig
	Zhipu     SimpleProviderConfig
	DeepSeek  SimpleProviderConfig
}

// NewProviderFromConfig creates a provider from simple config
func NewProviderFromConfig(cfg *SimpleConfig) (LLMProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	// Determine provider type
	provider := strings.ToLower(cfg.Provider)
	if provider == "" {
		provider = "openai" // default
	}

	// Get API key from config or environment
	apiKey := cfg.APIKey
	if apiKey == "" {
		// Try provider-specific env vars
		switch provider {
		case "openai", "gpt":
			apiKey = os.Getenv("OPENAI_API_KEY")
		case "anthropic", "claude":
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		case "openrouter":
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		case "gemini", "google":
			apiKey = os.Getenv("GOOGLE_API_KEY")
		case "zhipu", "glm":
			apiKey = os.Getenv("ZHIPU_API_KEY")
		case "deepseek":
			apiKey = os.Getenv("DEEPSEEK_API_KEY")
		}
	}

	if apiKey == "" {
		return nil, fmt.Errorf("no API key configured for provider: %s", provider)
	}

	// Get API base
	apiBase := cfg.APIBase
	if apiBase == "" {
		switch provider {
		case "openai", "gpt":
			apiBase = "https://api.openai.com/v1"
		case "anthropic", "claude":
			apiBase = "https://api.anthropic.com/v1"
		case "openrouter":
			apiBase = "https://openrouter.ai/api/v1"
		case "gemini", "google":
			apiBase = "https://generativelanguage.googleapis.com/v1beta"
		case "zhipu", "glm":
			apiBase = "https://open.bigmodel.cn/api/paas/v4"
		case "deepseek":
			apiBase = "https://api.deepseek.com/v1"
		default:
			apiBase = "https://api.openai.com/v1"
		}
	}

	// Get model
	model := cfg.Model
	if model == "" {
		switch provider {
		case "openai", "gpt":
			model = "gpt-4o-mini"
		case "anthropic", "claude":
			model = "claude-3-haiku-20240307"
		case "openrouter":
			model = "anthropic/claude-3-haiku"
		case "gemini", "google":
			model = "gemini-1.5-flash"
		case "zhipu", "glm":
			model = "glm-4-flash"
		case "deepseek":
			model = "deepseek-chat"
		default:
			model = "gpt-4o-mini"
		}
	}

	// Create HTTP-based provider
	return NewHTTPProviderWithConfig(&HTTPProviderConfig{
		APIKey:  apiKey,
		APIBase: apiBase,
		Model:   model,
		Proxy:   cfg.Proxy,
	}), nil
}

// NewProviderFromEnv creates a provider from environment variables
func NewProviderFromEnv() (LLMProvider, error) {
	cfg := &SimpleConfig{
		Provider: os.Getenv("AI_PROVIDER"),
		APIKey:   os.Getenv("AI_API_KEY"),
		APIBase:  os.Getenv("AI_API_BASE"),
		Model:    os.Getenv("AI_MODEL"),
		Proxy:    os.Getenv("AI_PROXY"),
	}

	// If no explicit provider, try to detect from available keys
	if cfg.Provider == "" {
		if os.Getenv("OPENAI_API_KEY") != "" {
			cfg.Provider = "openai"
			cfg.APIKey = os.Getenv("OPENAI_API_KEY")
		} else if os.Getenv("ANTHROPIC_API_KEY") != "" {
			cfg.Provider = "anthropic"
			cfg.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		} else if os.Getenv("OPENROUTER_API_KEY") != "" {
			cfg.Provider = "openrouter"
			cfg.APIKey = os.Getenv("OPENROUTER_API_KEY")
		} else {
			return nil, fmt.Errorf("no AI provider configured (set OPENAI_API_KEY, ANTHROPIC_API_KEY, or OPENROUTER_API_KEY)")
		}
	}

	return NewProviderFromConfig(cfg)
}
