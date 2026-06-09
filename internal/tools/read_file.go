package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultMaxLines = 2000

type ReadFileTool struct{}

func (t *ReadFileTool) Name() string { return "read_file" }

type ReadFileResult struct {
	Path      string   `json:"path"`
	Lines     []string `json:"lines"`
	LineCount int      `json:"line_count"`
	SizeBytes int64    `json:"size_bytes"`
}

func (r *ReadFileResult) Format() string {
	var sb strings.Builder
	for i, line := range r.Lines {
		fmt.Fprintf(&sb, "%4d  %s\n", i+1, line)
	}
	return sb.String()
}

func (r *ReadFileResult) Summary() string {
	return fmt.Sprintf("read %d lines", r.LineCount)
}

func (t *ReadFileTool) Run(_ context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: path"}
	}

	root, _ := args["root"].(string)
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "could not determine working directory", Err: err}
		}
	}

	maxLines := defaultMaxLines
	if v, ok := args["max_lines"]; ok {
		switch n := v.(type) {
		case int:
			maxLines = n
		case float64:
			maxLines = int(n)
		}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "invalid root", Err: err}
	}

	absPath := filepath.Join(absRoot, path)
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(absRoot, path)
	} else {
		absPath, err = filepath.Abs(path)
		if err != nil {
			return nil, &ToolError{Tool: t.Name(), Message: "invalid path", Err: err}
		}
	}

	// Enforce root boundary.
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return nil, &ToolError{Tool: t.Name(), Message: "path traversal not allowed"}
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &ToolError{Tool: t.Name(), Message: "file not found: " + path, Err: err}
		}
		return nil, &ToolError{Tool: t.Name(), Message: "stat failed: " + path, Err: err}
	}
	if info.IsDir() {
		return nil, &ToolError{Tool: t.Name(), Message: "path is a directory: " + path}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "read failed: " + path, Err: err}
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	if maxLines > 0 && totalLines > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, fmt.Sprintf("... truncated: showing %d of %d lines", maxLines, totalLines))
	}

	return &ReadFileResult{
		Path:      path,
		Lines:     lines,
		LineCount: len(lines),
		SizeBytes: info.Size(),
	}, nil
}
