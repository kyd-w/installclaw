package providers

import (
	"github.com/kyd-w/installclaw/pkg/providers/openai_compat"
)

// HTTPProvider wraps openai_compat.Provider
type HTTPProvider struct {
	*openai_compat.Provider
	model string
}

// NewHTTPProvider creates a new HTTP provider
func NewHTTPProvider(apiKey, apiBase, proxy string) *HTTPProvider {
	return &HTTPProvider{
		Provider: openai_compat.NewProvider(apiKey, apiBase, proxy),
	}
}

// HTTPProviderConfig holds configuration for HTTP provider
type HTTPProviderConfig struct {
	APIKey  string
	APIBase string
	Proxy   string
	Model   string
}

// NewHTTPProviderWithConfig creates an HTTP provider with config
func NewHTTPProviderWithConfig(cfg *HTTPProviderConfig) *HTTPProvider {
	opts := []openai_compat.Option{}
	if cfg.Model != "" {
		opts = append(opts, openai_compat.WithModel(cfg.Model))
	}
	return &HTTPProvider{
		Provider: openai_compat.NewProvider(cfg.APIKey, cfg.APIBase, cfg.Proxy, opts...),
		model:    cfg.Model,
	}
}

// GetDefaultModel returns the default model
func (p *HTTPProvider) GetDefaultModel() string {
	return p.model
}

// IsAvailable checks if the provider is available
func (p *HTTPProvider) IsAvailable() bool {
	return p.Provider != nil
}

// Name returns the provider name
func (p *HTTPProvider) Name() string {
	return "http"
}
