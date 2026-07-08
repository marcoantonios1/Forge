package mcp

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// buildTestdataServer compiles internal/mcp/testdata/server into a temp
// binary and returns its path. The binary is never left in the source tree.
func buildTestdataServer(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	bin := filepath.Join(t.TempDir(), "mcp-testdata-server")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/server")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build testdata server: %v\n%s", err, out)
	}
	return bin
}

// TestStdioClientWithGoBinary exercises the stdio transport against a real
// compiled Go binary (rather than the Python-based fake server used by
// TestStdioClientConnectAndCall), giving coverage of a multi-tool
// tools/list response and an isError tools/call response.
func TestStdioClientWithGoBinary(t *testing.T) {
	bin := buildTestdataServer(t)

	srv := MCPServer{
		Name:      "gofake",
		Transport: TransportStdio,
		Command:   bin,
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
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %+v", len(tools), tools)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	if !names["echo"] || !names["add"] {
		t.Fatalf("expected tools echo and add, got: %+v", tools)
	}

	echoResult, err := client.CallTool(ctx, "echo", map[string]any{"value": "hello-go"})
	if err != nil {
		t.Fatalf("CallTool echo: %v", err)
	}
	if echoResult.IsError {
		t.Fatalf("unexpected IsError result: %+v", echoResult)
	}
	if got := echoResult.MCPCallResultText(); got != "hello-go" {
		t.Fatalf("echo: got %q, want %q", got, "hello-go")
	}

	addResult, err := client.CallTool(ctx, "add", map[string]any{"a": 2, "b": 3})
	if err != nil {
		t.Fatalf("CallTool add: %v", err)
	}
	if addResult.IsError {
		t.Fatalf("unexpected IsError result: %+v", addResult)
	}
	if got := addResult.MCPCallResultText(); got != "5" {
		t.Fatalf("add: got %q, want %q", got, "5")
	}

	missingResult, err := client.CallTool(ctx, "nonexistent", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool nonexistent: unexpected Go error: %v", err)
	}
	if !missingResult.IsError {
		t.Fatalf("expected IsError=true for nonexistent tool, got: %+v", missingResult)
	}
}
