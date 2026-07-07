package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Transport is the connection type for an MCP server.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
)

// MCPServer describes one MCP server from forge.md's [mcp] section.
type MCPServer struct {
	Name      string            `json:"name"`
	Transport Transport         `json:"transport"` // "stdio" or "http"
	Command   string            `json:"command"`   // for stdio: the executable to spawn
	Args      []string          `json:"args"`      // for stdio: args to pass to Command
	URL       string            `json:"url"`       // for http: the server's base URL
	Env       map[string]string `json:"env"`       // environment overrides for the subprocess
}

// ParseMCPServers extracts the [mcp] section from raw forge.md content.
// The format is TOML-like but kept deliberately simple — no full TOML parser
// is needed; only two patterns are recognised:
//
//	[mcp]
//	[[mcp.servers]]
//	name = "my-server"
//	transport = "stdio"
//	command = "npx"
//	args = ["-y", "@modelcontextprotocol/server-filesystem"]
//	env.SOME_VAR = "value"
//
//	[[mcp.servers]]
//	name = "remote-server"
//	transport = "http"
//	url = "http://localhost:8081"
//
// Returns an empty slice (not an error) if no [mcp] section or no [[mcp.servers]]
// entries are found — absence is the common case.
//
// ParseMCPServers is intentionally a line-by-line parser of the subset of TOML
// relevant to this schema (array-of-tables under [[mcp.servers]]). It does NOT
// attempt to parse arbitrary TOML. For v1 this is sufficient and avoids adding
// any external dependency.
func ParseMCPServers(raw string) ([]MCPServer, error) {
	var servers []MCPServer
	var cur *MCPServer

	flush := func() {
		if cur != nil {
			servers = append(servers, *cur)
			cur = nil
		}
	}

	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if line == "[[mcp.servers]]" {
			flush()
			cur = &MCPServer{}
			continue
		}

		if cur == nil {
			// Outside any [[mcp.servers]] block — skip (includes "[mcp]" header
			// and any other unrelated forge.md content).
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch {
		case key == "name":
			cur.Name = unquote(value)
		case key == "transport":
			cur.Transport = Transport(unquote(value))
		case key == "command":
			cur.Command = unquote(value)
		case key == "url":
			cur.URL = unquote(value)
		case key == "args":
			var args []string
			if err := json.Unmarshal([]byte(value), &args); err != nil {
				return nil, fmt.Errorf("mcp: parsing args for server %q: %w", cur.Name, err)
			}
			cur.Args = args
		case strings.HasPrefix(key, "env."):
			envKey := strings.TrimPrefix(key, "env.")
			if cur.Env == nil {
				cur.Env = make(map[string]string)
			}
			cur.Env[envKey] = unquote(value)
		}
	}
	flush()

	return servers, nil
}

// unquote strips surrounding double quotes from a TOML-style string value.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
