package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type httpClient struct {
	srv    MCPServer
	http   *http.Client
	tools  []MCPTool
	nextID int
	mu     sync.Mutex
}

func newHTTPClient(srv MCPServer) *httpClient {
	return &httpClient{
		srv:  srv,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *httpClient) ServerName() string { return c.srv.Name }

func (c *httpClient) Connect(ctx context.Context) error {
	// Fetch tool list via POST to <url>/mcp with method "tools/list".
	// TODO: support SSE (Server-Sent Events) for streaming tool-call responses —
	// this would open a persistent GET connection to <url>/mcp/events (or similar)
	// and multiplex tool-call results over it, rather than each call being a
	// single synchronous POST/response round trip as implemented here.
	// For v1, all communication is synchronous POST JSON-RPC.
	tools, err := c.fetchTools(ctx)
	if err != nil {
		return fmt.Errorf("mcp: http %q: list tools: %w", c.srv.Name, err)
	}
	c.tools = tools
	return nil
}

func (c *httpClient) fetchTools(ctx context.Context) ([]MCPTool, error) {
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

func (c *httpClient) ListTools() []MCPTool { return c.tools }

func (c *httpClient) CallTool(ctx context.Context, name string, args map[string]any) (*MCPCallResult, error) {
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

func (c *httpClient) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	req := JSONRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp http encode: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.srv.URL+"/mcp", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mcp http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp http do: %w", err)
	}
	defer httpResp.Body.Close()
	var resp JSONRPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("mcp http decode: %w", err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return &resp, nil
}

func (c *httpClient) Close() error { return nil } // HTTP is stateless
