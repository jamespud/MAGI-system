// Package mcp implements ToolRegistryPort and ToolExecutorPort backed by
// external Model Context Protocol (MCP) servers over stdio or Streamable
// HTTP transports.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ServerConfig describes one external MCP server to connect to.
type ServerConfig struct {
	Name           string
	Transport      string // "stdio" or "http"
	Command        string // stdio: executable to spawn
	Args           []string
	URL            string // http: base URL of the MCP endpoint
	Env            map[string]string
	TimeoutSeconds int
}

// Adapter connects to configured MCP servers and exposes their tools through
// the MAGI tool ports. Activation is lazy and per-server: an unreachable
// server is skipped during List and surfaces an error on Execute, so MAGI
// startup does not fail because of a downstream MCP server.
type Adapter struct {
	mu      sync.RWMutex
	order   []string
	servers map[string]*server
	dial    func(ServerConfig) (*mcpclient.Client, error)
}

type server struct {
	cfg    ServerConfig
	dial   func(ServerConfig) (*mcpclient.Client, error)
	client *mcpclient.Client
	tools  []port.ToolDefinition
	err    error
	once   sync.Once
}

var _ port.ToolRegistryPort = (*Adapter)(nil)
var _ port.ToolExecutorPort = (*Adapter)(nil)

// New builds an Adapter for the given server configs. Config validation
// (unique non-empty names, valid transports) happens in bootstrap.Validate.
func New(cfgs []ServerConfig) *Adapter {
	return newWithDial(cfgs, dial)
}

func newWithDial(cfgs []ServerConfig, dial func(ServerConfig) (*mcpclient.Client, error)) *Adapter {
	a := &Adapter{servers: make(map[string]*server, len(cfgs)), dial: dial}
	for _, c := range cfgs {
		if _, ok := a.servers[c.Name]; ok {
			continue
		}
		a.servers[c.Name] = &server{cfg: c, dial: dial}
		a.order = append(a.order, c.Name)
	}
	return a
}

func dial(cfg ServerConfig) (*mcpclient.Client, error) {
	switch cfg.Transport {
	case "stdio":
		env := make([]string, 0, len(cfg.Env))
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		return mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
	case "http":
		return mcpclient.NewStreamableHttpClient(cfg.URL)
	default:
		return nil, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
}

// List returns every tool exposed by reachable MCP servers.
func (a *Adapter) List(ctx context.Context, _ []entity.ToolBinding) ([]port.ToolDefinition, error) {
	if a == nil {
		return nil, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []port.ToolDefinition
	for _, name := range a.order {
		s := a.servers[name]
		if err := s.activate(ctx); err != nil {
			continue // unreachable server: skip its tools, error surfaces on Execute
		}
		out = append(out, s.tools...)
	}
	return out, nil
}

// Execute routes a tool call to the MCP server named in the binding.
func (a *Adapter) Execute(ctx context.Context, req port.ToolExecutionRequest) (*port.ToolExecutionResult, error) {
	if a == nil {
		return nil, fmt.Errorf("mcp: adapter not configured")
	}
	a.mu.RLock()
	s := a.servers[req.Binding.Server]
	a.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("mcp: unknown server %q", req.Binding.Server)
	}
	if err := s.activate(ctx); err != nil {
		return nil, fmt.Errorf("mcp server %q: %w", s.cfg.Name, err)
	}
	var args map[string]any
	if req.ArgumentsJSON != "" {
		if err := json.Unmarshal([]byte(req.ArgumentsJSON), &args); err != nil {
			return nil, fmt.Errorf("mcp server %q: parse arguments: %w", s.cfg.Name, err)
		}
	}
	callCtx := ctx
	var cancel context.CancelFunc
	if s.cfg.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	res, err := s.client.CallTool(callCtx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: req.Binding.ToolName, Arguments: args},
	})
	if err != nil {
		return nil, fmt.Errorf("mcp server %q tool %q: %w", s.cfg.Name, req.Binding.ToolName, err)
	}
	out, err := renderResult(res)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q tool %q: %w", s.cfg.Name, req.Binding.ToolName, err)
	}
	if res.IsError {
		return nil, fmt.Errorf("mcp server %q tool %q failed: %s", s.cfg.Name, req.Binding.ToolName, out)
	}
	return &port.ToolExecutionResult{Output: out, Structured: res.StructuredContent, Raw: res}, nil
}

// Close shuts down every connected MCP client (terminating stdio subprocesses).
func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	var firstErr error
	for _, name := range a.order {
		if err := a.servers[name].close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ToolName returns the MAGI tool name for an MCP server/tool pair.
func ToolName(server, tool string) string {
	return "mcp_" + sanitize(server) + "_" + sanitize(tool)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func (s *server) close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *server) activate(ctx context.Context) error {
	s.once.Do(func() {
		if err := s.connect(ctx); err != nil {
			s.err = err
		}
	})
	return s.err
}

func (s *server) connect(ctx context.Context) error {
	c, err := s.dial(s.cfg)
	if err != nil {
		return err
	}
	if _, err := c.Initialize(ctx, mcpgo.InitializeRequest{
		Params: mcpgo.InitializeParams{
			ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcpgo.ClientCapabilities{},
			ClientInfo:      mcpgo.Implementation{Name: "magi", Version: "1.0"},
		},
	}); err != nil {
		_ = c.Close()
		return fmt.Errorf("initialize: %w", err)
	}
	list, err := c.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		_ = c.Close()
		return fmt.Errorf("list tools: %w", err)
	}
	s.client = c
	for _, t := range list.Tools {
		schema, err := json.Marshal(t.InputSchema)
		if err != nil {
			_ = c.Close()
			return fmt.Errorf("tool %q: encode input schema: %w", t.Name, err)
		}
		s.tools = append(s.tools, port.ToolDefinition{
			Name:       ToolName(s.cfg.Name, t.Name),
			Desc:       t.Description,
			ArgsSchema: schema,
			Source:     entity.ToolSourceMCP,
			Binding:    entity.ToolBinding{Source: entity.ToolSourceMCP, Server: s.cfg.Name, ToolName: t.Name},
		})
	}
	return nil
}

func renderResult(res *mcpgo.CallToolResult) (string, error) {
	if res == nil {
		return "", nil
	}
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case mcpgo.TextContent:
			parts = append(parts, v.Text)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				continue
			}
			parts = append(parts, string(b))
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return strings.Join(parts, "\n"), nil
}
