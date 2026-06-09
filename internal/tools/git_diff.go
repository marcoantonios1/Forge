package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

const defaultMaxDiffBytes = 512 * 1024 // 512KB

type GitDiffTool struct{}

func (t *GitDiffTool) Name() string { return "git_diff" }

type DiffFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type GitDiffResult struct {
	Diff      string     `json:"diff"`
	Files     []DiffFile `json:"files"`
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Truncated bool       `json:"truncated"`
}

func (r *GitDiffResult) Summary() string {
	if r.Truncated {
		return fmt.Sprintf("%d files changed, +%d -%d (truncated)", len(r.Files), r.Additions, r.Deletions)
	}
	return fmt.Sprintf("%d files changed, +%d -%d", len(r.Files), r.Additions, r.Deletions)
}

func (t *GitDiffTool) Run(ctx context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}

	staged := false
	if v, ok := args["staged"].(bool); ok {
		staged = v
	}

	maxBytes := defaultMaxDiffBytes
	if v, ok := args["max_bytes"]; ok {
		switch n := v.(type) {
		case int:
			maxBytes = n
		case float64:
			maxBytes = int(n)
		}
	}

	var paths []string
	if v, ok := args["paths"]; ok {
		switch p := v.(type) {
		case []string:
			paths = p
		case []any:
			for _, item := range p {
				if s, ok := item.(string); ok {
					paths = append(paths, s)
				}
			}
		}
	}

	diffArgs := []string{"diff", "--unified=3"}
	numstatArgs := []string{"diff", "--numstat"}
	if staged {
		diffArgs = append(diffArgs, "--cached")
		numstatArgs = append(numstatArgs, "--cached")
	}
	if len(paths) > 0 {
		diffArgs = append(diffArgs, "--")
		diffArgs = append(diffArgs, paths...)
		numstatArgs = append(numstatArgs, "--")
		numstatArgs = append(numstatArgs, paths...)
	}

	rawDiff, err := runGit(ctx, root, diffArgs...)
	if err != nil {
		if isNotGitRepo(err) {
			return nil, &ToolError{Tool: t.Name(), Message: "not a git repository: " + root}
		}
		return nil, &ToolError{Tool: t.Name(), Message: "git diff failed", Err: err}
	}

	truncated := false
	if maxBytes > 0 && len(rawDiff) > maxBytes {
		rawDiff = rawDiff[:maxBytes] + fmt.Sprintf("\n... diff truncated at %d bytes ...", maxBytes)
		truncated = true
	}

	numstatOut, err := runGit(ctx, root, numstatArgs...)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "git diff --numstat failed", Err: err}
	}

	var files []DiffFile
	totalAdd, totalDel := 0, 0
	for _, line := range strings.Split(numstatOut, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		add, _ := strconv.Atoi(parts[0])
		del, _ := strconv.Atoi(parts[1])
		files = append(files, DiffFile{Path: parts[2], Additions: add, Deletions: del})
		totalAdd += add
		totalDel += del
	}
	if files == nil {
		files = []DiffFile{}
	}

	return &GitDiffResult{
		Diff:      rawDiff,
		Files:     files,
		Additions: totalAdd,
		Deletions: totalDel,
		Truncated: truncated,
	}, nil
}
