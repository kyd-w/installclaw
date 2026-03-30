// Package resolver provides dependency resolution for packages
package resolver

import (
	"context"
	"fmt"
	"sort"

	"github.com/kyd-w/installclaw/pkg/core/metadata"
)

// Resolver resolves package dependencies
type Resolver struct {
	registry *metadata.Registry
}

// NewResolver creates a new dependency resolver
func NewResolver(registry *metadata.Registry) *Resolver {
	return &Resolver{
		registry: registry,
	}
}

// Resolve resolves all dependencies for a package and returns them in installation order
func (r *Resolver) Resolve(ctx context.Context, pkg *metadata.PackageMetadata) (*DependencyGraph, error) {
	graph := NewDependencyGraph()

	// Start resolution from the root package
	if err := r.resolveRecursive(ctx, pkg, graph, make(map[string]bool)); err != nil {
		return nil, err
	}

	// Topological sort to get installation order
	order, err := graph.TopologicalSort()
	if err != nil {
		return nil, fmt.Errorf("dependency cycle detected: %w", err)
	}

	graph.InstallOrder = order
	return graph, nil
}

// resolveRecursive recursively resolves dependencies
func (r *Resolver) resolveRecursive(ctx context.Context, pkg *metadata.PackageMetadata, graph *DependencyGraph, visited map[string]bool) error {
	pkgID := pkg.ID

	// Skip if already visited
	if visited[pkgID] {
		return nil
	}
	visited[pkgID] = true

	// Add node to graph
	graph.AddNode(pkgID, pkg)

	// Resolve each dependency
	for _, dep := range pkg.Dependencies {
		// Skip optional and recommended dependencies
		if dep.Type == metadata.DependencyOptional || dep.Type == metadata.DependencyRecommended {
			continue
		}

		// Get dependency package
		depPkg, ok := r.registry.Get(dep.PackageID)
		if !ok {
			return fmt.Errorf("dependency not found: %s (required by %s)", dep.PackageID, pkgID)
		}

		// Add edge to graph
		graph.AddEdge(pkgID, dep.PackageID)

		// Recursively resolve
		if err := r.resolveRecursive(ctx, depPkg, graph, visited); err != nil {
			return err
		}
	}

	// Check for conflicts in package definition
	for _, conflict := range pkg.Conflicts {
		if _, exists := graph.Nodes[conflict.PackageID]; exists {
			return fmt.Errorf("package conflict: %s conflicts with %s", pkgID, conflict.PackageID)
		}
	}

	return nil
}

// DependencyGraph represents a dependency graph
type DependencyGraph struct {
	Nodes        map[string]*DependencyNode `json:"nodes"`
	Edges        map[string][]string        `json:"edges"` // from -> []to
	InstallOrder []string                   `json:"installOrder"`
}

// DependencyNode represents a node in the dependency graph
type DependencyNode struct {
	PackageID string                  `json:"packageId"`
	Package   *metadata.PackageMetadata `json:"package"`
	Resolved  bool                    `json:"resolved"`
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		Nodes: make(map[string]*DependencyNode),
		Edges: make(map[string][]string),
	}
}

// AddNode adds a node to the graph
func (g *DependencyGraph) AddNode(id string, pkg *metadata.PackageMetadata) {
	if _, exists := g.Nodes[id]; !exists {
		g.Nodes[id] = &DependencyNode{
			PackageID: id,
			Package:   pkg,
			Resolved:  false,
		}
	}
}

// AddEdge adds a directed edge (dependency relationship)
func (g *DependencyGraph) AddEdge(from, to string) {
	g.Edges[from] = append(g.Edges[from], to)
}

// TopologicalSort performs topological sort on the graph
func (g *DependencyGraph) TopologicalSort() ([]string, error) {
	// Calculate in-degree for each node
	inDegree := make(map[string]int)
	for id := range g.Nodes {
		inDegree[id] = 0
	}

	for _, deps := range g.Edges {
		for _, dep := range deps {
			inDegree[dep]++
		}
	}

	// Find all nodes with in-degree 0 (no dependencies)
	queue := make([]string, 0)
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Sort queue for deterministic order
	sort.Strings(queue)

	var result []string

	for len(queue) > 0 {
		// Get first node
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Reduce in-degree for dependents
		for _, dep := range g.Edges[current] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}

		// Keep queue sorted for deterministic order
		sort.Strings(queue)
	}

	// Check for cycles
	if len(result) != len(g.Nodes) {
		return nil, fmt.Errorf("cycle detected in dependency graph")
	}

	return result, nil
}

// GetOrderedPackages returns packages in installation order
func (g *DependencyGraph) GetOrderedPackages() []*metadata.PackageMetadata {
	var packages []*metadata.PackageMetadata
	for _, id := range g.InstallOrder {
		if node, ok := g.Nodes[id]; ok {
			packages = append(packages, node.Package)
		}
	}
	return packages
}

// MarkResolved marks a package as resolved
func (g *DependencyGraph) MarkResolved(packageID string) {
	if node, ok := g.Nodes[packageID]; ok {
		node.Resolved = true
	}
}

// IsResolved checks if a package is resolved
func (g *DependencyGraph) IsResolved(packageID string) bool {
	if node, ok := g.Nodes[packageID]; ok {
		return node.Resolved
	}
	return false
}

// AllResolved checks if all dependencies are resolved
func (g *DependencyGraph) AllResolved() bool {
	for _, node := range g.Nodes {
		if !node.Resolved {
			return false
		}
	}
	return true
}

// GetUnresolved returns unresolved dependencies
func (g *DependencyGraph) GetUnresolved() []string {
	var unresolved []string
	for id, node := range g.Nodes {
		if !node.Resolved {
			unresolved = append(unresolved, id)
		}
	}
	return unresolved
}
