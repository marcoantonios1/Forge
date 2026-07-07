// Package mcp implements a client for the Model Context Protocol (MCP):
// connecting to MCP servers over stdio or HTTP, discovering their tools, and
// invoking them via JSON-RPC 2.0.
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONRPCRequest is a single JSON-RPC 2.0 call.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"` // always "2.0"
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// JSONRPCResponse is the response envelope.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *JSONRPCError) Error() string {
	return fmt.Sprintf("mcp rpc error %d: %s", e.Code, e.Message)
}

// MCPTool is one tool advertised by an MCP server.
type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"` // JSON Schema object
}

// MCPContent is one content block in a tools/call response.
type MCPContent struct {
	Type string `json:"type"` // "text" | "image" | ...
	Text string `json:"text,omitempty"`
}

// MCPCallResult wraps the parsed tools/call response.
type MCPCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPCallResultText returns the concatenated text from all content blocks.
func (r *MCPCallResult) MCPCallResultText() string {
	var parts []string
	for _, c := range r.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
