package mcp

import (
	"context"
	"fmt"
)

// Client is the interface all MCP transport implementations satisfy.
type Client interface {
	// Connect initialises the connection and performs a handshake / initial
	// tool list fetch. Must be called before ListTools or CallTool.
	// Returns an error if the transport cannot be established.
	Connect(ctx context.Context) error

	// ListTools returns the tools advertised by this MCP server.
	// Returns cached results if called after a successful Connect().
	ListTools() []MCPTool

	// CallTool calls a named tool with the given arguments.
	// Returns the structured result or an error.
	CallTool(ctx context.Context, name string, args map[string]any) (*MCPCallResult, error)

	// Close cleanly terminates the connection (kills subprocess for stdio,
	// no-op for http). Safe to call more than once.
	Close() error

	// ServerName returns the configured name of this server.
	ServerName() string
}

// New constructs the appropriate Client implementation based on the
// transport field in srv. Returns an error if the transport is unrecognised.
func New(srv MCPServer) (Client, error) {
	switch srv.Transport {
	case TransportStdio:
		return newStdioClient(srv), nil
	case TransportHTTP:
		return newHTTPClient(srv), nil
	default:
		return nil, fmt.Errorf("mcp: unknown transport %q for server %q", srv.Transport, srv.Name)
	}
}
