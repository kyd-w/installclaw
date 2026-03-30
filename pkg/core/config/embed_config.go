// Package config provides configuration loading functionality
package config

import (
	"embed"
)

//go:embed default.yaml
var defaultConfigFS embed.FS

// GetEmbeddedConfig returns the embedded default configuration
func GetEmbeddedConfig() ([]byte, error) {
	return defaultConfigFS.ReadFile("default.yaml")
}

// GetEmbeddedConfigString returns the embedded default configuration as string
func GetEmbeddedConfigString() string {
	data, err := GetEmbeddedConfig()
	if err != nil {
		return ""
	}
	return string(data)
}
