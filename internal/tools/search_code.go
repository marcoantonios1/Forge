package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type SearchCodeTool struct{}

func (t *SearchCodeTool) Name() string { return "search_code" }

type SearchMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

type SearchCodeResult struct {
	Pattern   string        `json:"pattern"`
	Matches   []SearchMatch `json:"matches"`
	Count     int           `json:"count"`
	Truncated bool          `json:"truncated"`
}

func (r *SearchCodeResult) Summary() string {
	if r.Truncated {
		return fmt.Sprintf("found %d+ matches (truncated)", r.Count)
	}
	return fmt.Sprintf("found %d matches", r.Count)
}

func (t *SearchCodeTool) Run(_ context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: pattern"}
	}

	useRegex := false
	if v, ok := args["regex"].(bool); ok {
		useRegex = v
	}

	ignoreCase := false
	if v, ok := args["ignore_case"].(bool); ok {
		ignoreCase = v
	}

	fileGlob, _ := args["file_glob"].(string)

	maxResults := 200
	if v, ok := args["max_results"]; ok {
		switch n := v.(type) {
		case int:
			maxResults = n
		case float64:
			maxResults = int(n)
		}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "invalid root", Err: err}
	}

	// Compile regex once before the walk.
	var re *regexp.Regexp
	if useRegex {
		pat := pattern
		if ignoreCase {
			pat = "(?i)" + pat
		}
		re, err = regexp.Compile(pat)
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "invalid regex: " + err.Error(), Err: err}
		}
	}

	ignorePatterns := loadGitignore(absRoot)

	var matches []SearchMatch
	truncated := false

	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(absRoot, path)
		if rel == "." {
			return nil
		}

		name := d.Name()

		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if matchesGitignore(rel, d.IsDir(), ignorePatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		// File glob filter.
		if fileGlob != "" {
			matched, _ := filepath.Match(fileGlob, name)
			if !matched {
				return nil
			}
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			text := scanner.Text()

			var matched bool
			if useRegex {
				matched = re.MatchString(text)
			} else if ignoreCase {
				matched = strings.Contains(strings.ToLower(text), strings.ToLower(pattern))
			} else {
				matched = strings.Contains(text, pattern)
			}

			if matched {
				if len(matches) >= maxResults {
					truncated = true
					return filepath.SkipAll
				}
				matches = append(matches, SearchMatch{
					File: rel,
					Line: lineNum,
					Text: strings.TrimSpace(text),
				})
			}
		}
		return nil
	})
	if err != nil && !truncated {
		return nil, &ToolError{Tool: t.Name(), Message: "walk failed", Err: err}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].File != matches[j].File {
			return matches[i].File < matches[j].File
		}
		return matches[i].Line < matches[j].Line
	})

	return &SearchCodeResult{
		Pattern:   pattern,
		Matches:   matches,
		Count:     len(matches),
		Truncated: truncated,
	}, nil
}
