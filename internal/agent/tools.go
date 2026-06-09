package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/tools"
)

type ToolCall struct {
	Name string
	Args map[string]any
}

type Registry struct {
	runners map[string]*tools.ToolRunner
}

func NewRegistry(root string, emitter events.Emitter, sessionID string) *Registry {
	reg := &Registry{runners: make(map[string]*tools.ToolRunner)}

	register := func(t tools.Tool) {
		reg.runners[t.Name()] = tools.NewRunner(t, emitter, sessionID)
	}

	register(&tools.ReadFileTool{})
	register(&tools.ListFilesTool{})
	register(&tools.SearchCodeTool{})
	register(&tools.GitStatusTool{})
	register(&tools.GitDiffTool{})
	register(&tools.GitLogTool{})

	// Default root for all tools that accept it.
	_ = root // callers inject "root" into args per-call

	return reg
}

func (r *Registry) Dispatch(ctx context.Context, call ToolCall) (any, error) {
	runner, ok := r.runners[call.Name]
	if !ok {
		return nil, &tools.ToolError{Tool: call.Name, Message: "unknown tool: " + call.Name}
	}
	return runner.Run(ctx, call.Args)
}

// ParseToolCall parses:
//
//	TOOL: <name>
//	ARGS: <JSON object>
//
// TODO: hook in native OpenAI function-calling JSON here when targeting GPT/Claude
// native tool-use mode instead of the text protocol.
func ParseToolCall(response string) (ToolCall, bool) {
	var name string
	var argsLine string

	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TOOL:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "TOOL:"))
		} else if strings.HasPrefix(line, "ARGS:") {
			argsLine = strings.TrimSpace(strings.TrimPrefix(line, "ARGS:"))
		}
	}

	if name == "" {
		return ToolCall{}, false
	}

	args := make(map[string]any)
	if argsLine != "" {
		if err := json.Unmarshal([]byte(argsLine), &args); err != nil {
			return ToolCall{}, false
		}
	}

	return ToolCall{Name: name, Args: args}, true
}
