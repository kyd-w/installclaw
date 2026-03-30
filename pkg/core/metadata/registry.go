package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Registry manages package metadata storage and retrieval
type Registry struct {
	mu          sync.RWMutex
	packages    map[string]*PackageMetadata
	configPaths []string // Paths to search for predefined configs
}

// NewRegistry creates a new package registry
func NewRegistry(configPaths ...string) *Registry {
	return &Registry{
		packages:    make(map[string]*PackageMetadata),
		configPaths: configPaths,
	}
}

// Register adds a package to the registry
func (r *Registry) Register(pkg *PackageMetadata) error {
	if pkg == nil {
		return fmt.Errorf("package cannot be nil")
	}
	if pkg.ID == "" {
		return fmt.Errorf("package ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.packages[strings.ToLower(pkg.ID)] = pkg
	return nil
}

// Get retrieves a package by ID
func (r *Registry) Get(id string) (*PackageMetadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pkg, ok := r.packages[strings.ToLower(id)]
	return pkg, ok
}

// Search searches for packages by name, tags, or keywords
func (r *Registry) Search(query string) []*PackageMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	query = strings.ToLower(query)
	var results []*PackageMetadata

	for _, pkg := range r.packages {
		// Match by ID
		if strings.Contains(strings.ToLower(pkg.ID), query) {
			results = append(results, pkg)
			continue
		}

		// Match by name
		if strings.Contains(strings.ToLower(pkg.Name), query) {
			results = append(results, pkg)
			continue
		}

		// Match by tags
		for _, tag := range pkg.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				results = append(results, pkg)
				break
			}
		}

		// Match by keywords
		for _, keyword := range pkg.Keywords {
			if strings.Contains(strings.ToLower(keyword), query) {
				results = append(results, pkg)
				break
			}
		}
	}

	return results
}

// List returns all registered packages
func (r *Registry) List() []*PackageMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	packages := make([]*PackageMetadata, 0, len(r.packages))
	for _, pkg := range r.packages {
		packages = append(packages, pkg)
	}
	return packages
}

// LoadPredefined loads predefined package configurations from config paths
func (r *Registry) LoadPredefined(ctx context.Context) error {
	for _, configPath := range r.configPaths {
		if err := r.loadFromPath(configPath); err != nil {
			// Log warning but continue
			fmt.Printf("Warning: failed to load packages from %s: %v\n", configPath, err)
		}
	}
	return nil
}

// loadFromPath loads package configurations from a directory
func (r *Registry) loadFromPath(path string) error {
	// Check if path exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Path doesn't exist, skip
	}

	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Only load .yaml, .yml, or .json files
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}

		// Read file
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filePath, err)
		}

		// Parse package metadata
		pkg := &PackageMetadata{}
		switch ext {
		case ".json":
			if err := json.Unmarshal(data, pkg); err != nil {
				return fmt.Errorf("failed to parse JSON %s: %w", filePath, err)
			}
		default: // .yaml, .yml
			if err := yaml.Unmarshal(data, pkg); err != nil {
				return fmt.Errorf("failed to parse YAML %s: %w", filePath, err)
			}
		}

		// Register package
		if err := r.Register(pkg); err != nil {
			return fmt.Errorf("failed to register %s: %w", filePath, err)
		}

		return nil
	})
}

// AddConfigPath adds a path to search for predefined configurations
func (r *Registry) AddConfigPath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configPaths = append(r.configPaths, path)
}

// GetByCategory returns packages by category
func (r *Registry) GetByCategory(category string) []*PackageMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []*PackageMetadata
	for _, pkg := range r.packages {
		if strings.EqualFold(pkg.Category, category) {
			results = append(results, pkg)
		}
	}
	return results
}

// GetDependencies returns all dependencies for a package (transitive)
func (r *Registry) GetDependencies(pkgID string) ([]*PackageMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	visited := make(map[string]bool)
	var deps []*PackageMetadata

	var resolve func(id string) error
	resolve = func(id string) error {
		if visited[id] {
			return nil // Already processed
		}
		visited[id] = true

		pkg, ok := r.packages[strings.ToLower(id)]
		if !ok {
			return fmt.Errorf("dependency not found: %s", id)
		}

		for _, dep := range pkg.Dependencies {
			if dep.Type == DependencyRequired {
				if err := resolve(dep.PackageID); err != nil {
					return err
				}
			}
		}

		deps = append(deps, pkg)
		return nil
	}

	pkg, ok := r.packages[strings.ToLower(pkgID)]
	if !ok {
		return nil, fmt.Errorf("package not found: %s", pkgID)
	}

	for _, dep := range pkg.Dependencies {
		if dep.Type == DependencyRequired {
			if err := resolve(dep.PackageID); err != nil {
				return nil, err
			}
		}
	}

	return deps, nil
}

// Stats returns registry statistics
func (r *Registry) Stats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := RegistryStats{
		TotalPackages: len(r.packages),
		ByCategory:    make(map[string]int),
	}

	for _, pkg := range r.packages {
		stats.ByCategory[pkg.Category]++
	}

	return stats
}

// RegistryStats contains registry statistics
type RegistryStats struct {
	TotalPackages int            `json:"totalPackages"`
	ByCategory    map[string]int `json:"byCategory"`
}
