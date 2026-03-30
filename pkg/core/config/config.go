// Package config provides configuration loading functionality
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kyd-w/installclaw/pkg/core/logger"
	"gopkg.in/yaml.v3"
)

// Config represents the main configuration
type Config struct {
	AI            AIConfig            `yaml:"ai"`
	Security      SecurityConfig      `yaml:"security"`
	Install       InstallConfig       `yaml:"install"`
	Cache         CacheConfig         `yaml:"cache"`
	Logging       LoggingConfig       `yaml:"logging"`
	TrustedDomains []string           `yaml:"trusted_domains"`
	WebSearch     WebSearchConfig     `yaml:"web_search"`
}

// AIConfig contains AI provider configuration
type AIConfig struct {
	Primary string              `yaml:"primary"`
	OpenAI  OpenAIConfig        `yaml:"openai"`
	Claude  ClaudeConfig        `yaml:"claude"`
	Ollama  OllamaConfig        `yaml:"ollama"`
}

// OpenAIConfig contains OpenAI configuration
type OpenAIConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
}

// ClaudeConfig contains Claude configuration
type ClaudeConfig struct {
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
	BaseURL string `yaml:"base_url"`
}

// OllamaConfig contains Ollama configuration
type OllamaConfig struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model"`
}

// SecurityConfig contains security configuration
type SecurityConfig struct {
	MinGitHubStars  int  `yaml:"min_github_stars"`
	AllowUntrusted  bool `yaml:"allow_untrusted"`
	VerifyChecksum  bool `yaml:"verify_checksum"`
	VerifySignature bool `yaml:"verify_signature"`
}

// InstallConfig contains installation configuration
type InstallConfig struct {
	InstallDir       string `yaml:"install_dir"`
	BinDir           string `yaml:"bin_dir"`
	ConfigDir        string `yaml:"config_dir"`
	CacheDir         string `yaml:"cache_dir"`
	Timeout          int    `yaml:"timeout"`
	SkipDependencies bool   `yaml:"skip_dependencies"`
	ForceReinstall   bool   `yaml:"force_reinstall"`
}

// CacheConfig contains cache configuration
type CacheConfig struct {
	Enabled bool `yaml:"enabled"`
	TTL     int  `yaml:"ttl"`
	MaxSize int  `yaml:"max_size"`
}

// LoggingConfig contains logging configuration
type LoggingConfig struct {
	Level string `yaml:"level"`
	File  string `yaml:"file"`
	Color bool   `yaml:"color"`
}

// WebSearchConfig contains web search configuration
type WebSearchConfig struct {
	Primary    string              `yaml:"primary"`     // "duckduckgo" or "tavily"
	Tavily     TavilyConfig        `yaml:"tavily"`
	DuckDuckGo DuckDuckGoConfig    `yaml:"duckduckgo"`
}

// TavilyConfig contains Tavily search configuration
type TavilyConfig struct {
	APIKey string `yaml:"api_key"`
}

// DuckDuckGoConfig contains DuckDuckGo search configuration
type DuckDuckGoConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		AI: AIConfig{
			Primary: "openai",
			OpenAI: OpenAIConfig{
				Model:   "gpt-4o-mini",
				BaseURL: "https://api.openai.com/v1",
			},
			Claude: ClaudeConfig{
				Model:   "claude-3-haiku-20240307",
				BaseURL: "https://api.anthropic.com/v1",
			},
			Ollama: OllamaConfig{
				BaseURL: "http://localhost:11434",
				Model:   "llama3",
			},
		},
		Security: SecurityConfig{
			MinGitHubStars:  200,
			AllowUntrusted:  false,
			VerifyChecksum:  true,
			VerifySignature: false,
		},
		Install: InstallConfig{
			InstallDir: "/usr/local",
			BinDir:     "/usr/local/bin",
			ConfigDir:  "~/.config",
			CacheDir:   "~/.cache/installer",
			Timeout:    30,
		},
		Cache: CacheConfig{
			Enabled: true,
			TTL:     24,
			MaxSize: 500,
		},
		Logging: LoggingConfig{
			Level: "info",
			File:  "",
			Color: true,
		},
		TrustedDomains: []string{},
		WebSearch: WebSearchConfig{
			Primary: "duckduckgo",
			Tavily: TavilyConfig{
				APIKey: "",
			},
			DuckDuckGo: DuckDuckGoConfig{
				Enabled: true,
			},
		},
	}
}

// Load loads configuration from file, with embedded defaults as fallback
func Load(path string) (*Config, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// 1. Load embedded default config first
	embeddedData, err := GetEmbeddedConfig()
	if err == nil {
		embeddedData = []byte(expandEnvVars(string(embeddedData)))
		if err := yaml.Unmarshal(embeddedData, cfg); err != nil {
			// Log warning but continue with hardcoded defaults
			// Note: In production, embedded config should always be valid
			fmt.Fprintf(os.Stderr, "Warning: failed to parse embedded config: %v\n", err)
		}
	}

	// 2. If no path specified, try default locations
	if path == "" {
		path = findConfigFile()
	}

	// 3. If external config exists, merge it (overwrites embedded values)
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}

		// Expand environment variables
		data = []byte(expandEnvVars(string(data)))

		// Parse YAML (merges with existing cfg)
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return cfg, nil
}

// findConfigFile finds the config file in standard locations
func findConfigFile() string {
	// Locations to check in order
	locations := []string{
		"./installer.yaml",
		"./installer.yml",
		"./config/installer.yaml",
	}

	// Also check executable directory
	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		locations = append(locations,
			filepath.Join(execDir, "installer.yaml"),
			filepath.Join(execDir, "installer.yml"),
		)
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return ""
}

// expandEnvVars expands environment variables in the config
func expandEnvVars(s string) string {
	// Handle ${VAR} and $VAR syntax
	result := s

	// Find all ${VAR} patterns
	for {
		start := strings.Index(result, "${")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		varName := result[start+2 : end]
		varValue := os.Getenv(varName)
		result = result[:start] + varValue + result[end+1:]
	}

	return result
}

// ToLoggerConfig converts LoggingConfig to logger.Config
func (lc *LoggingConfig) ToLoggerConfig() *logger.Config {
	return &logger.Config{
		Level: logger.ParseLevel(lc.Level),
		File:  lc.File,
		Color: lc.Color,
	}
}

// GetAPIKey returns the API key for the specified provider
func (c *Config) GetAPIKey(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return c.AI.OpenAI.APIKey
	case "claude", "anthropic":
		return c.AI.Claude.APIKey
	default:
		return ""
	}
}

// GetModel returns the model for the specified provider
func (c *Config) GetModel(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return c.AI.OpenAI.Model
	case "claude", "anthropic":
		return c.AI.Claude.Model
	case "ollama":
		return c.AI.Ollama.Model
	default:
		return ""
	}
}
