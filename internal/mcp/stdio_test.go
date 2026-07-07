package mcp

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// fakeServerScript is a minimal JSON-RPC 2.0 stdio server used to exercise the
// stdio transport without depending on a real MCP server binary. It answers
// tools/list with one tool and tools/call by echoing back the "value" argument.
const fakeServerScript = `
import json, sys

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    method = req.get("method")
    if method == "tools/list":
        result = {"tools": [{"name": "echo", "description": "echoes value", "inputSchema": {}}]}
    elif method == "tools/call":
        args = req.get("params", {}).get("arguments", {})
        result = {"content": [{"type": "text", "text": str(args.get("value", ""))}]}
    else:
        result = {}
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": result}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`

func TestStdioClientConnectAndCall(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	srv := MCPServer{
		Name:      "fake",
		Transport: TransportStdio,
		Command:   "python3",
		Args:      []string{"-c", fakeServerScript},
	}

	client, err := New(srv)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	tools := client.ListTools()
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	result, err := client.CallTool(ctx, "echo", map[string]any{"value": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected IsError result: %+v", result)
	}
	if got := result.MCPCallResultText(); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}
