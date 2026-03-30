// Package ai provides AI-powered package query capabilities
package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Provider defines the interface for AI providers
type Provider interface {
	// Query sends a prompt to the AI and returns the response
	Query(ctx context.Context, prompt string) (string, error)

	// IsAvailable checks if the provider is available and configured
	IsAvailable() bool

	// Name returns the provider name
	Name() string
}

// Manager manages multiple AI providers with fallback support
type Manager struct {
	mu        sync.RWMutex
	providers []Provider
	cache     *QueryCache
	config    *ManagerConfig
}

// ManagerConfig contains configuration for the AI manager
type ManagerConfig struct {
	CacheEnabled  bool          `json:"cacheEnabled"`
	CacheTTL      time.Duration `json:"cacheTTL"`
	QueryTimeout  time.Duration `json:"queryTimeout"`
	MaxRetries    int           `json:"maxRetries"`
	RetryDelay    time.Duration `json:"retryDelay"`
}

// DefaultManagerConfig returns the default manager configuration
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		CacheEnabled: true,
		CacheTTL:     24 * time.Hour,
		QueryTimeout: 30 * time.Second,
		MaxRetries:   2,
		RetryDelay:   1 * time.Second,
	}
}

// NewManager creates a new AI provider manager
func NewManager(cfg *ManagerConfig) *Manager {
	if cfg == nil {
		cfg = DefaultManagerConfig()
	}

	return &Manager{
		providers: make([]Provider, 0),
		cache:     NewQueryCache(cfg.CacheTTL),
		config:    cfg,
	}
}

// AddProvider adds an AI provider to the manager
func (m *Manager) AddProvider(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = append(m.providers, provider)
}

// Query queries all available providers until one succeeds
func (m *Manager) Query(ctx context.Context, prompt string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check cache first
	if m.config.CacheEnabled {
		if cached, ok := m.cache.Get(prompt); ok {
			return cached, nil
		}
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, m.config.QueryTimeout)
	defer cancel()

	var lastErr error
	for _, provider := range m.providers {
		if !provider.IsAvailable() {
			continue
		}

		// Try query with retries
		var result string
		for i := 0; i <= m.config.MaxRetries; i++ {
			result, lastErr = provider.Query(ctx, prompt)
			if lastErr == nil {
				// Cache successful result
				if m.config.CacheEnabled {
					m.cache.Set(prompt, result)
				}
				return result, nil
			}

			// Wait before retry
			if i < m.config.MaxRetries {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(m.config.RetryDelay):
				}
			}
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("all AI providers failed: %w", lastErr)
	}
	return "", fmt.Errorf("no AI providers available")
}

// QuerySpecific queries a specific provider by name
func (m *Manager) QuerySpecific(ctx context.Context, providerName, prompt string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, provider := range m.providers {
		if provider.Name() == providerName {
			if !provider.IsAvailable() {
				return "", fmt.Errorf("provider %s is not available", providerName)
			}
			return provider.Query(ctx, prompt)
		}
	}

	return "", fmt.Errorf("provider %s not found", providerName)
}

// GetAvailableProviders returns a list of available provider names
func (m *Manager) GetAvailableProviders() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var available []string
	for _, provider := range m.providers {
		if provider.IsAvailable() {
			available = append(available, provider.Name())
		}
	}
	return available
}

// ClearCache clears the query cache
func (m *Manager) ClearCache() {
	m.cache.Clear()
}

// buildPackageQueryPrompt builds a prompt for querying package information
func buildPackageQueryPrompt(software string) string {
	return fmt.Sprintf(`You are a software installation expert. I need to install "%s".

Please provide installation information in the following JSON format:
{
  "id": "package-id",
  "name": "Display Name",
  "version": "latest-stable-version",
  "description": "Brief description",
  "homepage": "https://official-website.com",
  "category": "runtime|dev-tool|ai-tool|database|etc",
  "sources": [
    {
      "type": "github|official|npm|pypi",
      "url": "https://...",
      "stars": 12345 (for GitHub)
    }
  ],
  "installMethods": [
    {
      "type": "binary|package|script",
      "name": "Method name",
      "commands": ["command1", "command2"],
      "verifyCmd": "command to verify installation",
      "platform": {
        "os": ["linux", "darwin", "windows"],
        "arch": ["amd64", "arm64"]
      }
    }
  ],
  "dependencies": [
    {"packageId": "dependency-id", "type": "required|optional"}
  ]
}

Important:
1. Only include sources from official websites or GitHub repositories with 200+ stars
2. Provide cross-platform installation methods when possible
3. Include the most common/recommended installation method first
4. Use package managers (brew, apt, winget) when appropriate`, software)
}

// parsePackageFromAIResponse parses AI response into package metadata
func parsePackageFromAIResponse(response string) (map[string]interface{}, error) {
	// Find JSON in response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("no valid JSON found in response")
	}

	// TODO: Parse JSON from response
	// This is a placeholder for the actual implementation
	_ = response[jsonStart : jsonEnd+1] // JSON string for future parsing

	return map[string]interface{}{}, nil
}
