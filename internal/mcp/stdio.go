package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type stdioClient struct {
	srv     MCPServer
	cmd     *exec.Cmd
	enc     *json.Encoder // writes to cmd.Stdin
	dec     *json.Decoder // reads from cmd.Stdout
	nextID  int
	tools   []MCPTool
	mu      sync.Mutex
	started bool
}

func newStdioClient(srv MCPServer) *stdioClient {
	return &stdioClient{srv: srv}
}

func (c *stdioClient) ServerName() string { return c.srv.Name }

func (c *stdioClient) Connect(ctx context.Context) error {
	// Split command into args if it contains spaces (simple split, not shell).
	parts := strings.Fields(c.srv.Command)
	if len(parts) == 0 {
		return fmt.Errorf("mcp: stdio server %q has empty command", c.srv.Name)
	}
	cmdName := parts[0]
	cmdArgs := append(parts[1:], c.srv.Args...)

	c.cmd = exec.CommandContext(ctx, cmdName, cmdArgs...)

	// Apply env overrides on top of the current process's environment.
	c.cmd.Env = os.Environ()
	for k, v := range c.srv.Env {
		c.cmd.Env = append(c.cmd.Env, k+"="+v)
	}

	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdio %q: stdin pipe: %w", c.srv.Name, err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdio %q: stdout pipe: %w", c.srv.Name, err)
	}
	c.cmd.Stderr = os.Stderr // forward subprocess stderr to Forge's stderr for visibility

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp: stdio %q: start: %w", c.srv.Name, err)
	}

	c.enc = json.NewEncoder(stdin)
	c.dec = json.NewDecoder(stdout)
	c.started = true

	// Fetch tool list immediately on connect.
	tools, err := c.fetchTools(ctx)
	if err != nil {
		c.cmd.Process.Kill() //nolint:errcheck
		return fmt.Errorf("mcp: stdio %q: list tools: %w", c.srv.Name, err)
	}
	c.tools = tools
	return nil
}

func (c *stdioClient) fetchTools(ctx context.Context) ([]MCPTool, error) {
	resp, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []MCPTool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools/list: %w", err)
	}
	return result.Tools, nil
}

func (c *stdioClient) ListTools() []MCPTool { return c.tools }

func (c *stdioClient) CallTool(ctx context.Context, name string, args map[string]any) (*MCPCallResult, error) {
	resp, err := c.call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	var result MCPCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("unmarshal tools/call: %w", err)
	}
	return &result, nil
}

func (c *stdioClient) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	req := JSONRPCRequest{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params}
	if err := c.enc.Encode(req); err != nil {
		return nil, fmt.Errorf("mcp encode: %w", err)
	}
	var resp JSONRPCResponse
	if err := c.dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("mcp decode: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return &resp, nil
}

// TODO: an MCP server health-check / reconnect loop could go here — detect
// when the subprocess has exited and either restart it or mark its bridged
// tools as unavailable, rather than letting every subsequent CallTool fail
// silently for the rest of the session.
func (c *stdioClient) Close() error {
	if c.cmd != nil && c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}
