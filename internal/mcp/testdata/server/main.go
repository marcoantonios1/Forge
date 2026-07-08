// Command server is a minimal MCP stdio server used by internal/mcp's tests
// to exercise the client against a real Go binary (as opposed to the
// Python-based fake server used elsewhere in the test suite). It speaks
// newline-delimited JSON-RPC 2.0 over stdin/stdout and advertises two tools:
// "echo" (returns the "value" argument unchanged) and "add" (sums "a" and "b").
//
// TODO: extend with more complex behaviors (tool errors, large responses,
// slow responses) for regression coverage as the MCP client matures.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Result  any    `json:"result,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type mcpCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

var tools = []mcpTool{
	{Name: "echo", Description: "echoes the value argument back", InputSchema: map[string]any{"type": "object"}},
	{Name: "add", Description: "adds a and b and returns the sum", InputSchema: map[string]any{"type": "object"}},
}

func main() {
	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	for {
		var req request
		if err := dec.Decode(&req); err != nil {
			return // stdin closed — client disconnected
		}

		var result any
		switch req.Method {
		case "tools/list":
			result = struct {
				Tools []mcpTool `json:"tools"`
			}{Tools: tools}
		case "tools/call":
			var params callParams
			json.Unmarshal(req.Params, &params) //nolint:errcheck
			result = callTool(params)
		default:
			result = struct{}{}
		}

		enc.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: result}) //nolint:errcheck
	}
}

func callTool(params callParams) mcpCallResult {
	switch params.Name {
	case "echo":
		value, _ := params.Arguments["value"].(string)
		return mcpCallResult{Content: []mcpContent{{Type: "text", Text: value}}}
	case "add":
		a, _ := params.Arguments["a"].(float64)
		b, _ := params.Arguments["b"].(float64)
		return mcpCallResult{Content: []mcpContent{{Type: "text", Text: fmt.Sprintf("%v", a+b)}}}
	default:
		return mcpCallResult{
			Content: []mcpContent{{Type: "text", Text: "unknown tool: " + params.Name}},
			IsError: true,
		}
	}
}
