package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ListFilesTool struct{}

func (t *ListFilesTool) Name() string { return "list_files" }

type ListFilesResult struct {
	Root  string   `json:"root"`
	Files []string `json:"files"`
	Count int      `json:"count"`
}

func (r *ListFilesResult) Summary() string {
	return fmt.Sprintf("listed %d files", r.Count)
}

func (t *ListFilesTool) Run(_ context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	pattern, _ := args["pattern"].(string)

	maxDepth := 5
	if v, ok := args["max_depth"]; ok {
		switch n := v.(type) {
		case int:
			maxDepth = n
		case float64:
			maxDepth = int(n)
		}
	}

	includeHidden := false
	if v, ok := args["include_hidden"].(bool); ok {
		includeHidden = v
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "invalid root", Err: err}
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ToolError{Tool: t.Name(), Message: "root not found: " + root, Err: err}
		}
		return nil, &ToolError{Tool: t.Name(), Message: "stat failed: " + root, Err: err}
	}
	if !info.IsDir() {
		return nil, &ToolError{Tool: t.Name(), Message: "root is not a directory: " + root}
	}

	ignorePatterns := loadGitignore(absRoot)

	var files []string
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}

		rel, _ := filepath.Rel(absRoot, path)
		if rel == "." {
			return nil
		}

		name := d.Name()

		// Hidden check.
		if !includeHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Depth check.
		if maxDepth > 0 {
			depth := strings.Count(rel, string(filepath.Separator))
			if d.IsDir() && depth >= maxDepth {
				return filepath.SkipDir
			}
		}

		// .gitignore check.
		if matchesGitignore(rel, d.IsDir(), ignorePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// Glob pattern filter against base name.
		if pattern != "" {
			matched, err := filepath.Match(pattern, name)
			if err != nil {
				return nil
			}
			if !matched {
				return nil
			}
		}

		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "walk failed", Err: err}
	}

	sort.Strings(files)
	return &ListFilesResult{Root: absRoot, Files: files, Count: len(files)}, nil
}

// loadGitignore reads ignore patterns from both .gitignore and .forgeignore
// at root, merging them into one pattern list. .forgeignore uses identical
// syntax to .gitignore and is checked IN ADDITION TO .gitignore, never
// instead of it — a path matching either file's patterns is excluded.
// TODO: if .gitignore negation patterns (!) are ever implemented, keep
// .forgeignore negation support in lockstep rather than letting one gain
// a feature the other lacks.
func loadGitignore(root string) []string {
	var patterns []string
	patterns = append(patterns, readIgnoreFile(filepath.Join(root, ".gitignore"))...)
	patterns = append(patterns, readIgnoreFile(filepath.Join(root, ".forgeignore"))...)
	return patterns
}

// readIgnoreFile parses one ignore-pattern file. Returns nil if the file
// doesn't exist — absence of either file is not an error.
func readIgnoreFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			// TODO: negation patterns are out of scope — skip silently,
			// consistent with .gitignore handling.
			continue
		}
		patterns = append(patterns, line)
	}
	if scanner.Err() != nil {
		return nil
	}
	return patterns
}

func matchesGitignore(rel string, isDir bool, patterns []string) bool {
	base := filepath.Base(rel)
	for _, p := range patterns {
		// Directory pattern ("vendor/").
		if strings.HasSuffix(p, "/") {
			dirPat := strings.TrimSuffix(p, "/")
			if isDir && (base == dirPat || rel == dirPat) {
				return true
			}
			// Also skip files under a matched directory.
			if strings.HasPrefix(rel, dirPat+string(filepath.Separator)) {
				return true
			}
			continue
		}
		// Glob match against base name.
		if matched, _ := filepath.Match(p, base); matched {
			return true
		}
		// Exact relative path match.
		if rel == p {
			return true
		}
	}
	return false
}
