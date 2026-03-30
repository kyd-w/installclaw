package dependencies

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed configs/*.yaml
var builtinConfigs embed.FS

// BuiltinRegistry holds all built-in dependency configurations
type BuiltinRegistry struct {
	dependencies map[string]*DependencyNode
}

// LoadBuiltinRegistry loads all built-in dependency configurations from embedded files
func LoadBuiltinRegistry() (*BuiltinRegistry, error) {
	registry := &BuiltinRegistry{
		dependencies: make(map[string]*DependencyNode),
	}

	// Read all config files from embedded FS
	entries, err := builtinConfigs.ReadDir("configs")
	if err != nil {
		return nil, fmt.Errorf("failed to read builtin configs: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		// Read file content
		data, err := builtinConfigs.ReadFile("configs/" + entry.Name())
		if err != nil {
			continue // Skip files that can't be read
		}

		// Parse YAML
		var node DependencyNode
		if err := yaml.Unmarshal(data, &node); err != nil {
			continue // Skip files that can't be parsed
		}

		// Add to registry
		if node.ID != "" {
			registry.dependencies[node.ID] = &node

			// Also add by provides aliases
			for _, alias := range node.Provides {
				if _, exists := registry.dependencies[alias]; !exists {
					registry.dependencies[alias] = &node
				}
			}
		}
	}

	return registry, nil
}

// Get retrieves a dependency by ID or alias
func (r *BuiltinRegistry) Get(id string) *DependencyNode {
	return r.dependencies[id]
}

// List returns all dependency IDs
func (r *BuiltinRegistry) List() []string {
	ids := make([]string, 0, len(r.dependencies))
	seen := make(map[string]bool)

	for _, dep := range r.dependencies {
		if !seen[dep.ID] {
			ids = append(ids, dep.ID)
			seen[dep.ID] = true
		}
	}

	return ids
}

// Has checks if a dependency exists
func (r *BuiltinRegistry) Has(id string) bool {
	_, exists := r.dependencies[id]
	return exists
}

// GetRawConfig returns the raw YAML content of a config file
func (r *BuiltinRegistry) GetRawConfig(filename string) ([]byte, error) {
	return builtinConfigs.ReadFile("configs/" + filename)
}

// WalkConfigs walks through all config files
func WalkConfigs(fn func(filename string, data []byte) error) error {
	entries, err := builtinConfigs.ReadDir("configs")
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := builtinConfigs.ReadFile("configs/" + entry.Name())
		if err != nil {
			continue
		}

		if err := fn(entry.Name(), data); err != nil {
			return err
		}
	}

	return nil
}

// GetConfigFS returns the embedded filesystem for external access
func GetConfigFS() embed.FS {
	return builtinConfigs
}

// Count returns the number of unique dependencies
func (r *BuiltinRegistry) Count() int {
	return len(r.List())
}

// Search searches for dependencies matching a query
func (r *BuiltinRegistry) Search(query string) []*DependencyNode {
	query = strings.ToLower(query)
	var results []*DependencyNode
	seen := make(map[string]bool)

	for _, dep := range r.dependencies {
		if seen[dep.ID] {
			continue
		}

		// Match against ID, name, and description
		if strings.Contains(strings.ToLower(dep.ID), query) ||
			strings.Contains(strings.ToLower(dep.Name), query) ||
			strings.Contains(strings.ToLower(dep.Description), query) {
			results = append(results, dep)
			seen[dep.ID] = true
		}
	}

	return results
}
