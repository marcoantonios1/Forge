package tools

import (
	"context"

	"github.com/marcoantonios1/Forge/internal/mcp"
)

// MCPToolBridge wraps one tool discovered from an MCP server and exposes it
// as a Forge tools.Tool, so it can be registered in agent.Registry and
// called via the same TOOL:/INTENT: protocol as native Forge tools.
type MCPToolBridge struct {
	serverName string
	tool       mcp.MCPTool
	client     mcp.Client
}

func NewMCPToolBridge(serverName string, tool mcp.MCPTool, client mcp.Client) *MCPToolBridge {
	return &MCPToolBridge{serverName: serverName, tool: tool, client: client}
}

// Name returns the tool name with the mcp__ prefix + server name + __ + tool name,
// e.g. "mcp__filesystem__read_file" for tool "read_file" from server "filesystem".
// The "mcp__" prefix lets the agent and the permission gate identify any MCP-
// bridged tool by a simple strings.HasPrefix check rather than requiring exact
// map entries per connected server.
func (b *MCPToolBridge) Name() string {
	return "mcp__" + b.serverName + "__" + b.tool.Name
}

func (b *MCPToolBridge) Run(ctx context.Context, args map[string]any) (any, error) {
	// Strip the "root" key that Forge always injects — MCP tools don't expect it.
	cleanArgs := make(map[string]any, len(args))
	for k, v := range args {
		if k != "root" {
			cleanArgs[k] = v
		}
	}
	result, err := b.client.CallTool(ctx, b.tool.Name, cleanArgs)
	if err != nil {
		return nil, &ToolError{Tool: b.Name(), Message: "mcp call failed", Err: err}
	}
	if result.IsError {
		return nil, &ToolError{Tool: b.Name(), Message: "mcp tool error: " + result.MCPCallResultText()}
	}
	return &MCPToolResult{
		ServerName: b.serverName,
		ToolName:   b.tool.Name,
		Content:    result.Content,
		Text:       result.MCPCallResultText(),
	}, nil
}

// MCPToolResult is what the agent sees when an MCPToolBridge call succeeds.
type MCPToolResult struct {
	ServerName string           `json:"server"`
	ToolName   string           `json:"tool"`
	Content    []mcp.MCPContent `json:"content"`
	Text       string           `json:"text"` // MCPCallResultText() for quick consumption
}

// Summary implements the interface tools.summarise() looks for, so tool.output
// events show a short line instead of a Go type name.
func (r *MCPToolResult) Summary() string {
	if len(r.Text) > 120 {
		return r.Text[:120] + "..."
	}
	return r.Text
}
