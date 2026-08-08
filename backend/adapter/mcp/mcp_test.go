package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpgo_server "github.com/mark3labs/mcp-go/server"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func fakeServer() *mcpgo_server.MCPServer {
	s := mcpgo_server.NewMCPServer("fake", "1.0.0")
	s.AddTool(mcpgo.NewTool("echo",
		mcpgo.WithDescription("echo text"),
		mcpgo.WithString("text", mcpgo.Required(), mcpgo.Description("text to echo")),
	), func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		text, _ := req.GetArguments()["text"].(string)
		return &mcpgo.CallToolResult{
			Content: []mcpgo.Content{mcpgo.TextContent{Type: "text", Text: "echo:" + text}},
		}, nil
	})
	s.AddTool(mcpgo.NewTool("boom", mcpgo.WithDescription("always fails")),
		func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return &mcpgo.CallToolResult{
				IsError: true,
				Content: []mcpgo.Content{mcpgo.TextContent{Type: "text", Text: "kaboom"}},
			}, nil
		})
	return s
}

func inProcessDial(s *mcpgo_server.MCPServer) func(ServerConfig) (*mcpclient.Client, error) {
	return func(ServerConfig) (*mcpclient.Client, error) {
		return mcpclient.NewInProcessClient(s)
	}
}

func TestAdapter_ListAndExecute_InProcess(t *testing.T) {
	srv := fakeServer()
	a := newWithDial([]ServerConfig{{Name: "fake", Transport: "stdio", Command: "echo"}}, inProcessDial(srv))

	defs, err := a.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	var echoDef *port.ToolDefinition
	for i := range defs {
		if defs[i].Name == "mcp_fake_echo" {
			echoDef = &defs[i]
			break
		}
	}
	if echoDef == nil {
		t.Fatalf("echo tool not found in %d defs", len(defs))
	}
	if echoDef.Binding.Server != "fake" || echoDef.Binding.ToolName != "echo" {
		t.Fatalf("binding: %+v", echoDef.Binding)
	}
	var schema map[string]any
	if err := json.Unmarshal(echoDef.ArgsSchema, &schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type: %v", schema["type"])
	}

	res, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		ToolName:      "mcp_fake_echo",
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "fake", ToolName: "echo"},
		ArgumentsJSON: `{"text":"hi"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output != "echo:hi" {
		t.Fatalf("output: %q", res.Output)
	}

	_, err = a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "fake", ToolName: "boom"},
		ArgumentsJSON: `{}`,
	})
	if err == nil || !strings.Contains(err.Error(), "failed: kaboom") {
		t.Fatalf("expected tool error, got %v", err)
	}
}

func TestAdapter_UnknownServer(t *testing.T) {
	a := New(nil)
	_, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding: entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "nope", ToolName: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown server") {
		t.Fatalf("expected unknown server error, got %v", err)
	}
}

func TestAdapter_HTTPTransport(t *testing.T) {
	srv := fakeServer()
	hs := mcpgo_server.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(hs)
	defer ts.Close()

	a := New([]ServerConfig{{Name: "fake", Transport: "http", URL: ts.URL, TimeoutSeconds: 10}})
	defs, err := a.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(defs))
	}
	res, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "fake", ToolName: "echo"},
		ArgumentsJSON: `{"text":"hi"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.Output != "echo:hi" {
		t.Fatalf("output: %q", res.Output)
	}
}

func TestAdapter_UnreachableServerSkipped(t *testing.T) {
	ts := httptest.NewServer(mcpgo_server.NewStreamableHTTPServer(fakeServer()))
	url := ts.URL
	ts.Close()

	a := New([]ServerConfig{{Name: "down", Transport: "http", URL: url}})
	defs, err := a.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 tools from down server, got %d", len(defs))
	}
	_, err = a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding: entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "down", ToolName: "x"},
	})
	if err == nil {
		t.Fatal("expected execute error for down server")
	}
}

func TestToolName_Sanitizes(t *testing.T) {
	cases := []struct{ server, tool, want string }{
		{"My Server!", "Tool/Name", "mcp_my_server_tool_name"},
		{"foo", "bar", "mcp_foo_bar"},
		{"UPPER", "x y", "mcp_upper_x_y"},
	}
	for _, c := range cases {
		if got := ToolName(c.server, c.tool); got != c.want {
			t.Errorf("ToolName(%q,%q)=%q want %q", c.server, c.tool, got, c.want)
		}
	}
}

func TestAdapter_CloseNilSafe(t *testing.T) {
	var a *Adapter
	if err := a.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
