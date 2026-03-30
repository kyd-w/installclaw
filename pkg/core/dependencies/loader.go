package dependencies

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Loader handles loading dependency configurations from multiple sources
type Loader struct {
	builtin   *BuiltinRegistry
	memoryDir string // Directory for learned/user-defined dependencies
}

// NewLoader creates a new dependency loader
func NewLoader(memoryDir string) (*Loader, error) {
	// Load built-in registry
	builtin, err := LoadBuiltinRegistry()
	if err != nil {
		return nil, fmt.Errorf("failed to load builtin registry: %w", err)
	}

	// Set default memory directory
	if memoryDir == "" {
		memoryDir = ".installer/memory/dependencies"
	}

	return &Loader{
		builtin:   builtin,
		memoryDir: memoryDir,
	}, nil
}

// Load loads a dependency configuration with the following priority:
// 1. Memory directory (learned/user-defined)
// 2. Built-in knowledge base
// Returns nil if not found (triggers AI generation)
func (l *Loader) Load(id string) *DependencyNode {
	// 1. Try memory directory first (highest priority)
	if node := l.loadFromMemory(id); node != nil {
		return node
	}

	// 2. Try built-in knowledge base
	if node := l.builtin.Get(id); node != nil {
		return node
	}

	// 3. Not found, return nil to trigger AI generation
	return nil
}

// loadFromMemory loads a dependency from the memory directory
func (l *Loader) loadFromMemory(id string) *DependencyNode {
	// Try different file names
	filenames := []string{
		id + ".yaml",
		id + ".yml",
		strings.ReplaceAll(id, "-", "_") + ".yaml",
	}

	for _, filename := range filenames {
		path := filepath.Join(l.memoryDir, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var entry MemoryEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}

		// Update success tracking
		entry.SuccessCount++
		entry.LastUsed = time.Now()
		// Note: We don't write back here to avoid I/O on every load

		return &entry.Dependency
	}

	return nil
}

// SaveToMemory saves a dependency configuration to the memory directory
func (l *Loader) SaveToMemory(node *DependencyNode, source string) error {
	// Ensure directory exists
	if err := os.MkdirAll(l.memoryDir, 0755); err != nil {
		return fmt.Errorf("failed to create memory directory: %w", err)
	}

	entry := MemoryEntry{
		ID:           node.ID,
		LearnedAt:    time.Now(),
		Source:       source,
		Dependency:   *node,
		SuccessCount: 1,
		LastUsed:     time.Now(),
	}

	data, err := yaml.Marshal(&entry)
	if err != nil {
		return fmt.Errorf("failed to marshal dependency: %w", err)
	}

	filename := filepath.Join(l.memoryDir, node.ID+".yaml")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write memory file: %w", err)
	}

	return nil
}

// ListMemory returns all dependencies stored in memory
func (l *Loader) ListMemory() ([]*MemoryEntry, error) {
	entries := []*MemoryEntry{}

	files, err := filepath.Glob(filepath.Join(l.memoryDir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if files == nil {
		return entries, nil
	}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var entry MemoryEntry
		if err := yaml.Unmarshal(data, &entry); err != nil {
			continue
		}

		entries = append(entries, &entry)
	}

	return entries, nil
}

// DeleteFromMemory removes a dependency from memory
func (l *Loader) DeleteFromMemory(id string) error {
	filenames := []string{
		filepath.Join(l.memoryDir, id+".yaml"),
		filepath.Join(l.memoryDir, id+".yml"),
	}

	for _, path := range filenames {
		if err := os.Remove(path); err == nil {
			return nil
		}
	}

	return fmt.Errorf("dependency %s not found in memory", id)
}

// Has checks if a dependency exists in any source
func (l *Loader) Has(id string) bool {
	return l.Load(id) != nil
}

// ListBuiltins returns all built-in dependency IDs
func (l *Loader) ListBuiltins() []string {
	return l.builtin.List()
}

// SearchBuiltins searches built-in dependencies
func (l *Loader) SearchBuiltins(query string) []*DependencyNode {
	return l.builtin.Search(query)
}

// GetBuiltin returns the built-in registry for direct access
func (l *Loader) GetBuiltin() *BuiltinRegistry {
	return l.builtin
}

// SetMemoryDir changes the memory directory
func (l *Loader) SetMemoryDir(dir string) {
	l.memoryDir = dir
}

// GetMemoryDir returns the current memory directory
func (l *Loader) GetMemoryDir() string {
	return l.memoryDir
}
