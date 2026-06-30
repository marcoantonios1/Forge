package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/marcoantonios1/Forge/internal/codeintel"
)

// DependencyGraphResult is the response returned by dependency_graph.
type DependencyGraphResult struct {
	File      string   `json:"file,omitempty"`            // queried file, or "" for repo-wide summary
	Imports   []string `json:"imports,omitempty"`         // files this file imports
	Importers []string `json:"importers,omitempty"`       // files that import this file
	AllFiles  int      `json:"all_files,omitempty"`       // total nodes, when no file is queried
}

// DependencyGraphTool maps import relationships between files.
type DependencyGraphTool struct {
	mu    sync.Mutex
	cache map[string]*codeintel.Graph // keyed by absolute root path
}

// NewDependencyGraphTool creates a DependencyGraphTool with an empty cache.
func NewDependencyGraphTool() *DependencyGraphTool {
	return &DependencyGraphTool{
		cache: make(map[string]*codeintel.Graph),
	}
}

func (t *DependencyGraphTool) Name() string { return "dependency_graph" }

func (t *DependencyGraphTool) Run(_ context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	graph, err := t.getGraph(root)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: fmt.Sprintf("build graph: %v", err)}
	}

	file, _ := args["file"].(string)
	if file == "" {
		// No specific file — return bounded repo-wide summary (count only, not full node list).
		return &DependencyGraphResult{AllFiles: len(graph.Nodes)}, nil
	}

	imports := graph.ImportsOf(file)
	if imports == nil {
		imports = []string{}
	}
	importers := graph.ImportersOf(file)
	if importers == nil {
		importers = []string{}
	}

	return &DependencyGraphResult{
		File:      file,
		Imports:   imports,
		Importers: importers,
	}, nil
}

// getGraph returns the cached graph for root, building it if necessary.
// The graph is built once per root per tool instance lifetime.
func (t *DependencyGraphTool) getGraph(root string) (*codeintel.Graph, error) {
	t.mu.Lock()
	if g, ok := t.cache[root]; ok {
		t.mu.Unlock()
		return g, nil
	}
	t.mu.Unlock()

	g, err := codeintel.BuildGraph(root)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.cache[root] = g
	t.mu.Unlock()

	return g, nil
}
