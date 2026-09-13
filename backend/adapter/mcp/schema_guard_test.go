package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpgo_server "github.com/mark3labs/mcp-go/server"

	"github.com/jamespud/magi/backend/domain/entity"
)

// guardServer exposes one tool whose top-level type is missing (mcp-go then
// marshals `"type":""`) and one tool whose schema is genuinely broken.
func guardServer() *mcpgo_server.MCPServer {
	s := mcpgo_server.NewMCPServer("guard", "1.0.0")
	handler := func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		return &mcpgo.CallToolResult{Content: []mcpgo.Content{mcpgo.TextContent{Type: "text", Text: "ok"}}}, nil
	}

	noTopType := mcpgo.Tool{Name: "no_top_type"} // InputSchema.Type stays ""
	noTopType.InputSchema.Properties = map[string]any{"symbol": map[string]any{"type": "string"}}
	s.AddTool(noTopType, handler)

	broken := mcpgo.Tool{Name: "broken"}
	broken.InputSchema.Type = "object"
	broken.InputSchema.Properties = map[string]any{"x": map[string]any{"type": "definitely-not-a-type"}}
	s.AddTool(broken, handler)

	return s
}

func TestAdapter_List_NormalizesAndRejectsSchemas(t *testing.T) {
	a := newWithDial([]ServerConfig{{Name: "guard", Transport: "stdio", Command: "echo"}}, inProcessDial(guardServer()))

	defs, err := a.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceMCP, Server: "guard"},
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "mcp_guard_no_top_type" {
		t.Fatalf("defs = %+v, want only mcp_guard_no_top_type", defs)
	}

	var schema map[string]any
	if err := json.Unmarshal(defs[0].ArgsSchema, &schema); err != nil {
		t.Fatalf("args schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("args schema type = %v, want object", schema["type"])
	}
	if strings.Contains(string(defs[0].ArgsSchema), `"type":""`) {
		t.Fatalf("empty type survived normalization: %s", defs[0].ArgsSchema)
	}
}
