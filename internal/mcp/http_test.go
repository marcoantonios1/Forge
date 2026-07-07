package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPClientConnectAndCall(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("server: decode request: %v", err)
		}

		var result any
		switch req.Method {
		case "tools/list":
			result = map[string]any{
				"tools": []MCPTool{{Name: "ping", Description: "pings back"}},
			}
		case "tools/call":
			result = MCPCallResult{Content: []MCPContent{{Type: "text", Text: "pong"}}}
		}

		resultBytes, _ := json.Marshal(result)
		resp := JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: resultBytes}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	srv := MCPServer{Name: "fake-http", Transport: TransportHTTP, URL: ts.URL}
	client, err := New(srv)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	tools := client.ListTools()
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("unexpected tools: %+v", tools)
	}

	result, err := client.CallTool(ctx, "ping", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := result.MCPCallResultText(); got != "pong" {
		t.Fatalf("got %q, want %q", got, "pong")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
