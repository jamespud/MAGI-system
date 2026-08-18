// Package mcp implements ToolRegistryPort and ToolExecutorPort backed by
// external Model Context Protocol (MCP) servers over stdio or Streamable
// HTTP transports.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	transport "github.com/mark3labs/mcp-go/client/transport"
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
	Headers        map[string]string // http: extra headers (auth, tenant, etc.)
	TimeoutSeconds int
	RetryAttempts  int // reconnect attempts on a failed call (default 2)
}

// Adapter connects to configured MCP servers and exposes their tools through
// the MAGI tool ports. Activation is lazy and per-server: an unreachable
// server is skipped during List and surfaces an error on Execute.
type Adapter struct {
	mu      sync.RWMutex
	order   []string
	servers map[string]*server
	dial    func(ServerConfig) (*mcpclient.Client, error)
}

type server struct {
	mu      sync.Mutex
	cfg     ServerConfig
	dial    func(ServerConfig) (*mcpclient.Client, error)
	client  *mcpclient.Client
	tools   []port.ToolDefinition
	lastErr error
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
		var opts []transport.StreamableHTTPCOption
		if len(cfg.Headers) > 0 {
			opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
		}
		return mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
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

// Execute routes a tool call to the MCP server named in the binding. A
// connection-level failure triggers one reconnect + retry with backoff.
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
	res, err := s.call(ctx, mcpgo.CallToolRequest{
		Params: mcpgo.CallToolParams{Name: req.Binding.ToolName, Arguments: args},
		Header: s.requestHeaders(),
	})
	if err != nil {
		// Connection-level failure: reconnect once and retry the call.
		if rerr := s.reconnect(ctx); rerr == nil {
			res, err = s.call(ctx, mcpgo.CallToolRequest{
				Params: mcpgo.CallToolParams{Name: req.Binding.ToolName, Arguments: args},
				Header: s.requestHeaders(),
			})
		}
		if err != nil {
			return nil, fmt.Errorf("mcp server %q tool %q: %w", s.cfg.Name, req.Binding.ToolName, err)
		}
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

func (s *server) requestHeaders() http.Header {
	h := make(http.Header, len(s.cfg.Headers))
	for k, v := range s.cfg.Headers {
		h.Set(k, v)
	}
	return h
}

func (s *server) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *server) activate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return nil
	}
	if err := s.connectLocked(ctx); err != nil {
		s.lastErr = err
		return err
	}
	s.lastErr = nil
	return nil
}

// reconnect drops the current client and activates a fresh connection.
func (s *server) reconnect(ctx context.Context) error {
	s.mu.Lock()
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	s.tools = nil
	s.mu.Unlock()
	return s.activate(ctx)
}

func (s *server) connectLocked(ctx context.Context) error {
	attempts := s.cfg.RetryAttempts + 1
	if attempts <= 0 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			delay := 200 * time.Millisecond * time.Duration(1<<(i-1))
			if delay > 2*time.Second {
				delay = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		c, err := s.dial(s.cfg)
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := c.Initialize(ctx, mcpgo.InitializeRequest{
			Params: mcpgo.InitializeParams{
				ProtocolVersion: mcpgo.LATEST_PROTOCOL_VERSION,
				Capabilities:    mcpgo.ClientCapabilities{},
				ClientInfo:      mcpgo.Implementation{Name: "magi", Version: "1.0"},
			},
			Header: s.requestHeaders(),
		}); err != nil {
			_ = c.Close()
			lastErr = fmt.Errorf("initialize: %w", err)
			continue
		}
		list, err := c.ListTools(ctx, mcpgo.ListToolsRequest{Header: s.requestHeaders()})
		if err != nil {
			_ = c.Close()
			lastErr = fmt.Errorf("list tools: %w", err)
			continue
		}
		s.client = c
		s.tools = nil
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
	return lastErr
}

func (s *server) call(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("not connected")
	}
	callCtx := ctx
	var cancel context.CancelFunc
	if s.cfg.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	return client.CallTool(callCtx, req)
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
