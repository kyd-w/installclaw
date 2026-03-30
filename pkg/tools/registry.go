package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ToolRegistry manages available tools
type ToolRegistry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get retrieves a tool by name
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Execute runs a tool by name
func (r *ToolRegistry) Execute(ctx context.Context, name string, args map[string]any) *ToolResult {
	tool, ok := r.Get(name)
	if !ok {
		return ErrorResult(fmt.Sprintf("tool %q not found", name))
	}
	return tool.Execute(ctx, args)
}

// List returns all tool names
func (r *ToolRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetDefinitions returns tool schemas
func (r *ToolRegistry) GetDefinitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]map[string]any, 0, len(r.tools))
	for name, tool := range r.tools {
		definitions = append(definitions, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": tool.Description(),
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		})
	}
	return definitions
}

// Count returns the number of tools
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
