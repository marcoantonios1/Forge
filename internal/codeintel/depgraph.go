package codeintel

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FileNode represents a single file and its import relationships.
type FileNode struct {
	Path       string   `json:"path"`
	Imports    []string `json:"imports"`     // resolved relative paths this file imports
	RawImports []string `json:"raw_imports"` // unresolved import strings as written in source
}

// Graph is the full import graph for a repository.
type Graph struct {
	Nodes map[string]*FileNode `json:"nodes"` // keyed by relative path
}

// ImportersOf returns relative paths of all files that import the given path.
func (g *Graph) ImportersOf(path string) []string {
	var importers []string
	for rel, node := range g.Nodes {
		if rel == path {
			continue
		}
		for _, imp := range node.Imports {
			if imp == path {
				importers = append(importers, rel)
				break
			}
		}
	}
	return importers
}

// ImportsOf returns the resolved imports for path, or nil if not in the graph.
func (g *Graph) ImportsOf(path string) []string {
	node, ok := g.Nodes[path]
	if !ok {
		return nil
	}
	return node.Imports
}

// BuildGraph walks root and extracts import relationships for Go, TypeScript,
// and Python files.
//
// TODO: invalidate or rebuild the dependency graph cache if patches are
// applied mid-task that add/remove/rename files the graph already covers.
func BuildGraph(root string) (*Graph, error) {
	allExts := map[string]bool{}
	for _, exts := range extsByLanguage {
		for ext := range exts {
			allExts[ext] = true
		}
	}

	files, err := WalkFiles(root, allExts)
	if err != nil {
		return nil, fmt.Errorf("dependency_graph: walk: %w", err)
	}

	modPath := goModulePath(root)
	graph := &Graph{Nodes: make(map[string]*FileNode, len(files))}

	for _, rel := range files {
		absPath := filepath.Join(root, rel)
		lang := DetectLanguage(rel)

		node := &FileNode{Path: rel}

		switch lang {
		case LangGo:
			node.Imports, node.RawImports = extractGoImports(absPath, rel, root, modPath)
		case LangTypeScript:
			node.Imports, node.RawImports = extractTSImports(absPath, rel)
		case LangPython:
			node.Imports, node.RawImports = extractPyImports(absPath, rel, root)
		}

		graph.Nodes[rel] = node
	}

	return graph, nil
}

// goModulePath reads the `module` line from root/go.mod, or "" if absent.
func goModulePath(root string) string {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func extractGoImports(absPath, relPath, root, modPath string) (resolved, raw []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
	if err != nil {
		return nil, nil
	}

	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := strings.Trim(imp.Path.Value, `"`)
		raw = append(raw, importPath)

		// Resolve only imports that share our module's prefix.
		if modPath != "" && strings.HasPrefix(importPath, modPath+"/") {
			suffix := strings.TrimPrefix(importPath, modPath+"/")
			candidate := filepath.FromSlash(suffix)
			// Map package dir to a .go file heuristically: look for any .go file there.
			dirAbs := filepath.Join(root, candidate)
			if info, err := os.Stat(dirAbs); err == nil && info.IsDir() {
				resolved = append(resolved, filepath.ToSlash(candidate))
			}
		}
	}
	return resolved, raw
}

var (
	tsImportRe  = regexp.MustCompile(`(?:import\s+[^'"]*from\s+|require\s*\(\s*)['"]([^'"]+)['"]`)
)

func extractTSImports(absPath, relPath string) (resolved, raw []string) {
	lines, err := readLines(absPath)
	if err != nil {
		return nil, nil
	}

	dir := filepath.Dir(relPath)

	for _, line := range lines {
		m := tsImportRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		spec := m[1]
		raw = append(raw, spec)

		// Relative imports only.
		if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") {
			candidate := filepath.ToSlash(filepath.Join(dir, spec))
			resolved = append(resolved, candidate)
		}
	}
	return resolved, raw
}

var (
	pyFromImportRe = regexp.MustCompile(`^\s*from\s+([\w.]+)\s+import`)
	pyImportRe     = regexp.MustCompile(`^\s*import\s+([\w.,\s]+)`)
)

func extractPyImports(absPath, relPath, root string) (resolved, raw []string) {
	lines, err := readLines(absPath)
	if err != nil {
		return nil, nil
	}

	for _, line := range lines {
		if m := pyFromImportRe.FindStringSubmatch(line); m != nil {
			mod := m[1]
			raw = append(raw, mod)
			if p := resolveModulePath(mod, root); p != "" {
				resolved = append(resolved, p)
			}
			continue
		}
		if m := pyImportRe.FindStringSubmatch(line); m != nil {
			for _, part := range strings.Split(m[1], ",") {
				mod := strings.TrimSpace(part)
				if mod == "" {
					continue
				}
				raw = append(raw, mod)
				if p := resolveModulePath(mod, root); p != "" {
					resolved = append(resolved, p)
				}
			}
		}
	}
	return resolved, raw
}

func resolveModulePath(mod, root string) string {
	rel := filepath.FromSlash(strings.ReplaceAll(mod, ".", "/")) + ".py"
	if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
		return filepath.ToSlash(rel)
	}
	return ""
}
