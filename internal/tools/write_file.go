package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type WriteFileResult struct {
	Path    string `json:"path"`
	Written int    `json:"bytes_written"`
	Created bool   `json:"created"` // true = new file, false = overwrote existing
}

func (r *WriteFileResult) Summary() string {
	action := "wrote"
	if r.Created {
		action = "created"
	}
	return fmt.Sprintf("%s %s (%d bytes)", action, r.Path, r.Written)
}

type WriteFileTool struct{}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Run(ctx context.Context, args map[string]any) (any, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	root, _ := args["root"].(string)

	if path == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: path"}
	}

	abs := filepath.Join(root, path)

	// Prevent path traversal outside root.
	cleanRoot := filepath.Clean(root)
	cleanAbs := filepath.Clean(abs)
	if len(cleanAbs) < len(cleanRoot) || cleanAbs[:len(cleanRoot)] != cleanRoot {
		return nil, &ToolError{Tool: t.Name(), Message: "path escapes root: " + path}
	}

	_, statErr := os.Stat(abs)
	created := os.IsNotExist(statErr)

	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "failed to create parent directories", Err: err}
	}

	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "failed to write file", Err: err}
	}

	return &WriteFileResult{Path: path, Written: len(content), Created: created}, nil
}
