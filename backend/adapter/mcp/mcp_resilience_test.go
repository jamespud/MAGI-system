package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpgo_server "github.com/mark3labs/mcp-go/server"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// authGate wraps the streamable HTTP server and requires an Authorization
// header on JSON-RPC method requests (notifications are fire-and-forget and
// carry no headers in mcp-go).
func authGate(hs http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(string(body)))
			var msg struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &msg)
			if !strings.HasPrefix(msg.Method, "notifications/") && r.Header.Get("Authorization") != token {
				println("DBG401", msg.Method, r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusUnauthorized)
				return
			} else if msg.Method == "tools/list" {
				println("DBGOK tools/list auth=", r.Header.Get("Authorization"))
			}
		}
		hs.ServeHTTP(w, r)
	})
}

func TestAdapter_HTTPAuthHeaders(t *testing.T) {
	srv := fakeServer()
	hs := mcpgo_server.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(authGate(hs, "Bearer test-token"))
	defer ts.Close()

	a := New([]ServerConfig{{
		Name: "auth", Transport: "http", URL: ts.URL, TimeoutSeconds: 10,
		Headers: map[string]string{"Authorization": "Bearer test-token"},
	}})
	defs, err := a.List(context.Background(), nil)
	if err != nil || len(defs) == 0 {
		t.Fatalf("list with headers: defs=%d err=%v", len(defs), err)
	}
	res, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "auth", ToolName: "echo"},
		ArgumentsJSON: `{"text":"hi"}`,
	})
	if err != nil || res.Output != "echo:hi" {
		t.Fatalf("execute with headers: out=%q err=%v", res.Output, err)
	}
}

// flakyDial returns a client whose first call fails, forcing a reconnect.
func flakyDial() func(ServerConfig) (*mcpclient.Client, error) {
	bad := mcpgo_server.NewMCPServer("bad", "1.0.0")
	bad.AddTool(mcpgo.NewTool("echo"), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return nil, errors.New("connection reset")
	})
	good := mcpgo_server.NewMCPServer("good", "1.0.0")
	good.AddTool(mcpgo.NewTool("echo", mcpgo.WithString("text", mcpgo.Required())),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return &mcpgo.CallToolResult{Content: []mcpgo.Content{mcpgo.TextContent{Type: "text", Text: "ok"}}}, nil
		})
	calls := 0
	return func(ServerConfig) (*mcpclient.Client, error) {
		calls++
		if calls == 1 {
			return mcpclient.NewInProcessClient(bad)
		}
		return mcpclient.NewInProcessClient(good)
	}
}

func TestAdapter_ReconnectsAfterConnectionFailure(t *testing.T) {
	a := newWithDial([]ServerConfig{{Name: "flaky", Transport: "stdio", Command: "x"}}, flakyDial())
	res, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "flaky", ToolName: "echo"},
		ArgumentsJSON: `{"text":"x"}`,
	})
	if err != nil || res.Output != "ok" {
		t.Fatalf("execute after reconnect: out=%q err=%v", res.Output, err)
	}
}
