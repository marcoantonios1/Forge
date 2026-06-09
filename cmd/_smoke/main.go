package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/tools"
)

func run(name string, t tools.Tool, args map[string]any) {
	emitter := &events.LogEmitter{Debug: true}
	runner := tools.NewRunner(t, emitter, "smoke-session")
	result, err := runner.Run(context.Background(), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n\n", err)
		return
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("--- %s ---\n%s\n\n", name, b)
}

func main() {
	root := "/Users/marcoantonios/Documents/Forge"

	run("read_file", &tools.ReadFileTool{}, map[string]any{
		"root": root, "path": "go.mod",
	})

	run("list_files", &tools.ListFilesTool{}, map[string]any{
		"root": root, "pattern": "*.go", "max_depth": 3,
	})

	run("search_code", &tools.SearchCodeTool{}, map[string]any{
		"root": root, "pattern": "package", "file_glob": "*.go", "max_results": 5,
	})

	_, err := (&tools.ReadFileTool{}).Run(context.Background(), map[string]any{
		"root": root, "path": "../../etc/passwd",
	})
	fmt.Printf("traversal blocked: %v\n", err)
	fmt.Printf("recoverable:       %v\n", tools.IsRecoverable(err))
}
