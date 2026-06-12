package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/marcoantonios1/Forge/internal/confirm"
	"github.com/marcoantonios1/Forge/internal/events"
	"github.com/marcoantonios1/Forge/internal/tools"
)

// ToolCall aliases tools.ToolCall so existing agent code is unchanged.
type ToolCall = tools.ToolCall

type Registry struct {
	runners map[string]*tools.ToolRunner
	gate    *confirm.PermissionGate // nil = no gating (autonomous / headless)
}

func NewRegistry(root string, emitter events.Emitter, sessionID string, gate *confirm.PermissionGate) *Registry {
	reg := &Registry{
		runners: make(map[string]*tools.ToolRunner),
		gate:    gate,
	}

	register := func(t tools.Tool) {
		reg.runners[t.Name()] = tools.NewRunner(t, emitter, sessionID)
	}

	register(&tools.ReadFileTool{})
	register(&tools.ListFilesTool{})
	register(&tools.SearchCodeTool{})
	register(&tools.GitStatusTool{})
	register(&tools.GitDiffTool{})
	register(&tools.GitLogTool{})
	register(&tools.GitBranchTool{})
	register(&tools.GitCheckoutTool{})
	register(&tools.GitStashTool{})
	register(&tools.GitPullTool{})
	// git_commit and git_push are registered for direct dispatch from main.go;
	// they are intentionally omitted from the agent system prompt so the agent
	// cannot call them via TOOL: protocol.
	// TODO: allow agent to call git_commit directly when execution_policy=autonomous.
	register(&tools.GitCommitTool{})
	register(&tools.GitPushTool{})

	// Default root for all tools that accept it.
	_ = root // callers inject "root" into args per-call

	return reg
}

func (r *Registry) Dispatch(ctx context.Context, call ToolCall) (any, error) {
	if r.gate != nil {
		return r.gate.Dispatch(ctx, call, r)
	}
	return r.DispatchDirect(ctx, call)
}

// DispatchDirect bypasses the gate and runs the tool directly.
// Called by PermissionGate after the user approves a call.
func (r *Registry) DispatchDirect(ctx context.Context, call tools.ToolCall) (any, error) {
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
