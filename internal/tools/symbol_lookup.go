package tools

import (
	"context"
	"fmt"

	"github.com/marcoantonios1/Forge/internal/codeintel"
)

// SymbolLookupTool finds definitions and references for a named symbol.
type SymbolLookupTool struct{}

func (t *SymbolLookupTool) Name() string { return "symbol_lookup" }

func (t *SymbolLookupTool) Run(_ context.Context, args map[string]any) (any, error) {
	root, _ := args["root"].(string)
	if root == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: root"}
	}
	name, _ := args["name"].(string)
	if name == "" {
		return nil, &ToolError{Tool: t.Name(), Message: "missing required arg: name"}
	}

	result, err := codeintel.FindSymbol(root, name)
	if err != nil {
		return nil, &ToolError{Tool: t.Name(), Message: fmt.Sprintf("symbol lookup failed: %v", err)}
	}
	return result, nil
}
