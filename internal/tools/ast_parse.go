package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/marcoantonios1/Forge/internal/codeintel"
)

// ASTParseTool returns top-level declarations in a source file.
type ASTParseTool struct{}

func (t *ASTParseTool) Name() string { return "ast_parse" }

func (t *ASTParseTool) Run(_ context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	path, _ := args["path"].(string)
	if path == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: path"}
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: "invalid root", Err: err}
	}
	absPath := filepath.Join(absRoot, path)

	// Enforce root boundary (mirrors read_file.go's traversal guard).
	if !strings.HasPrefix(absPath, absRoot+string(filepath.Separator)) && absPath != absRoot {
		return nil, &ToolError{Tool: t.Name(), Message: "path traversal not allowed"}
	}

	summary, err := codeintel.ParseFile(absPath)
	if err != nil {
		if errors.Is(err, codeintel.ErrUnsupportedLanguage) {
			ext := filepath.Ext(path)
			return nil, &ToolError{Tool: t.Name(), Message: fmt.Sprintf("unsupported language: %s", ext)}
		}
		return nil, &ToolError{Tool: t.Name(), Message: fmt.Sprintf("parse failed: %v", err)}
	}

	// Return a copy with a relative path for cleaner output.
	summary.Path = path
	return summary, nil
}
