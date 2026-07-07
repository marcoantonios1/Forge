package mcp

import (
	"reflect"
	"testing"
)

func TestParseMCPServers(t *testing.T) {
	raw := `# Project conventions

[mcp]
[[mcp.servers]]
name = "filesystem"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem"]
env.SOME_VAR = "value"

[[mcp.servers]]
name = "remote-server"
transport = "http"
url = "http://localhost:8081"
`
	got, err := ParseMCPServers(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []MCPServer{
		{
			Name:      "filesystem",
			Transport: TransportStdio,
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-filesystem"},
			Env:       map[string]string{"SOME_VAR": "value"},
		},
		{
			Name:      "remote-server",
			Transport: TransportHTTP,
			URL:       "http://localhost:8081",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseMCPServersNoSection(t *testing.T) {
	got, err := ParseMCPServers("# just a normal forge.md\nbuild: make build\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no servers, got %+v", got)
	}
}

func TestParseMCPServersMalformedArgs(t *testing.T) {
	raw := `[[mcp.servers]]
name = "broken"
args = [oops
`
	_, err := ParseMCPServers(raw)
	if err == nil {
		t.Fatal("expected an error for malformed args, got nil")
	}
}
