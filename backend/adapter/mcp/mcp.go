// Package mcp implements ToolRegistryPort and ToolExecutorPort backed by
// external Model Context Protocol (MCP) servers over stdio or Streamable
// HTTP transports.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	transport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
	"github.com/jamespud/magi/backend/domain/port"
	"github.com/jamespud/magi/backend/domain/validation"
)

var (
	// errNotConnected is raised before anything is sent, so a call that fails
	// this way is safe to repeat regardless of the tool's effect class.
	errNotConnected = errors.New("not connected")

	baseRetryDelay = 200 * time.Millisecond
	maxRetryDelay  = 2 * time.Second
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
	// RetryAttempts bounds transport reconnects for one call. 0 disables the
	// retry; the call is still attempted once. It never authorises re-running a
	// tool whose effect class is not retry-safe.
	RetryAttempts int
	// EffectOverrides classifies individual tools by MCP tool name (not the
	// namespaced mcp_<server>_<tool> form) for servers that ship no annotations.
	// An override can classify an unannotated tool and can always be more
	// conservative, but it can never claim a tool is safer than the server's own
	// annotation says.
	EffectOverrides map[string]string
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

// List returns the tools exposed by reachable MCP servers, restricted to the
// servers and tool names the caller actually bound. Callers pass the user's
// resolved bindings; an empty binding set therefore yields no tools (fail
// closed) instead of every configured server's catalog.
func (a *Adapter) List(ctx context.Context, bindings []entity.ToolBinding) ([]port.ToolDefinition, error) {
	if a == nil {
		return nil, nil
	}
	wanted := make(map[string]map[string]bool)
	for _, b := range bindings {
		if b.Source != entity.ToolSourceMCP || b.Server == "" {
			continue
		}
		if wanted[b.Server] == nil {
			wanted[b.Server] = make(map[string]bool)
		}
		if b.ToolName != "" {
			wanted[b.Server][b.ToolName] = true
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	var out []port.ToolDefinition
	for _, name := range a.order {
		tools, ok := wanted[name]
		if !ok {
			continue
		}
		s := a.servers[name]
		if err := s.activate(ctx); err != nil {
			continue // unreachable server: skip its tools, error surfaces on Execute
		}
		for _, def := range s.tools {
			// An empty tool-name set means "the whole server".
			if len(tools) > 0 && !tools[def.Binding.ToolName] {
				continue
			}
			out = append(out, def)
		}
	}
	return out, nil
}

// Execute routes a tool call to the MCP server named in the binding.
//
// A transport failure is retried only when the tool's effect class makes a
// second invocation safe; see callWithTransportRetry. Everything else is left
// to the execution kernel, which owns retry semantics.
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
	res, err := s.callWithTransportRetry(ctx, req, args)
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
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay(i)):
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
			// Providers reject the shapes MCP servers (and mcp-go itself) can
			// emit, and a rejected definition fails the whole model call, so a
			// tool whose schema cannot be made valid is skipped rather than
			// forwarded.
			normalized, err := validation.NormalizeToolSchema(schema)
			if err != nil {
				log.Printf("mcp server %q tool %q: unusable input schema: %v", s.cfg.Name, t.Name, err)
				continue
			}
			if err := validation.CompileSchema(normalized); err != nil {
				log.Printf("mcp server %q tool %q: skipping tool with invalid input schema: %v", s.cfg.Name, t.Name, err)
				continue
			}
			s.tools = append(s.tools, port.ToolDefinition{
				Name:        ToolName(s.cfg.Name, t.Name),
				Desc:        t.Description,
				ArgsSchema:  normalized,
				Source:      entity.ToolSourceMCP,
				Binding:     entity.ToolBinding{Source: entity.ToolSourceMCP, Server: s.cfg.Name, ToolName: t.Name},
				EffectClass: s.effectClass(t.Name, t.Annotations),
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
		return nil, errNotConnected
	}
	callCtx := ctx
	var cancel context.CancelFunc
	if s.cfg.TimeoutSeconds > 0 {
		callCtx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.TimeoutSeconds)*time.Second)
		defer cancel()
	}
	return client.CallTool(callCtx, req)
}

// effectClass resolves one tool's side-effect semantics from the server's own
// annotations, then applies the operator override.
func (s *server) effectClass(tool string, annotations mcpgo.ToolAnnotation) port.ToolEffectClass {
	return mergeEffectClass(s.cfg.Name, tool, effectClassFromAnnotations(annotations), s.cfg.EffectOverrides[tool])
}

// effectClassFromAnnotations maps the MCP tool-hint annotations onto MAGI's
// effect classes. The protocol defaults are already pessimistic — an unannotated
// tool may be destructive and is not idempotent — so a hint has to be present
// and explicit before a call is treated as safe to repeat.
func effectClassFromAnnotations(a mcpgo.ToolAnnotation) port.ToolEffectClass {
	if a.ReadOnlyHint != nil && *a.ReadOnlyHint {
		return port.ToolEffectReadOnly
	}
	if a.DestructiveHint != nil && *a.DestructiveHint {
		return port.ToolEffectNonIdempotent
	}
	if a.IdempotentHint != nil && *a.IdempotentHint {
		return port.ToolEffectIdempotent
	}
	return port.ToolEffectUnknown
}

// mergeEffectClass applies a configured override. An override may classify a
// tool the server did not annotate, and may always be more conservative, but it
// can never claim a tool is safer than the server's own annotation says: a
// destructive tool stays destructive no matter what the config asks for.
func mergeEffectClass(server, tool string, annotation port.ToolEffectClass, override string) port.ToolEffectClass {
	if override == "" {
		return annotation
	}
	parsed, err := port.ParseToolEffectClass(override)
	if err != nil {
		log.Printf("mcp server %q tool %q: ignoring effect override: %v", server, tool, err)
		return annotation
	}
	if annotation == "" || annotation == port.ToolEffectUnknown {
		return parsed
	}
	if effectRank(parsed) < effectRank(annotation) {
		log.Printf("mcp server %q tool %q: effect override %q is less conservative than the server annotation %q; keeping %q",
			server, tool, parsed, annotation, annotation)
		return annotation
	}
	return parsed
}

// effectRank orders effect classes by how much re-invocation risk they carry.
func effectRank(effect port.ToolEffectClass) int {
	switch effect {
	case port.ToolEffectReadOnly:
		return 0
	case port.ToolEffectIdempotent:
		return 1
	default:
		return 2
	}
}

// callWithTransportRetry separates transport retry from semantic retry.
//
// A transport failure means no definitive response arrived: the server may or
// may not have executed the call. Re-issuing the call is therefore only allowed
// for tools that tolerate a repeat (read_only/idempotent). For every other tool
// the call is made exactly once and the failure is reported as
// execution.ErrExternalOutcomeUnknown, so the kernel records an UNKNOWN outcome
// instead of a plain failure it would happily re-execute later.
//
// A JSON-RPC error response is definitive and is never retried here: re-running
// it would be a semantic retry, which belongs to the kernel.
func (s *server) callWithTransportRetry(ctx context.Context, req port.ToolExecutionRequest, args map[string]any) (*mcpgo.CallToolResult, error) {
	attempts := s.cfg.RetryAttempts + 1
	if attempts < 1 {
		attempts = 1
	}
	if !retrySafe(req.EffectClass) {
		attempts = 1
	}
	call := func() (*mcpgo.CallToolResult, error) {
		return s.call(ctx, mcpgo.CallToolRequest{
			Params: mcpgo.CallToolParams{Name: req.Binding.ToolName, Arguments: args},
			Header: s.requestHeaders(),
		})
	}

	res, err := call()
	for attempt := 1; attempt < attempts && err != nil; attempt++ {
		if !isRetryableFailure(err) {
			break
		}
		if delay := retryDelay(attempt); delay > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		if rerr := s.reconnect(ctx); rerr != nil {
			break
		}
		res, err = call()
	}
	if err != nil && isTransportFailure(err) {
		return nil, fmt.Errorf("%w: %w", execution.ErrExternalOutcomeUnknown, err)
	}
	return res, err
}

// retrySafe reports whether a tool may be invoked a second time after a failure
// whose outcome could not be observed.
func retrySafe(effect port.ToolEffectClass) bool {
	return effect == port.ToolEffectReadOnly || effect == port.ToolEffectIdempotent
}

// isRetryableFailure reports whether re-issuing the call is a pure transport
// retry. A tool that was never sent (errNotConnected) cannot have run, and a
// transport failure may have run without answering; a JSON-RPC error response
// is a definitive result and is not retryable here.
func isRetryableFailure(err error) bool {
	return errors.Is(err, errNotConnected) || isTransportFailure(err)
}

// isTransportFailure reports whether the client failed below the protocol, i.e.
// without receiving a definitive response.
func isTransportFailure(err error) bool {
	var transportErr *transport.Error
	return errors.As(err, &transportErr)
}

// retryDelay is the exponential backoff shared by connection setup and call
// retries.
func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	if attempt > 16 {
		return maxRetryDelay
	}
	if delay := baseRetryDelay << (attempt - 1); delay < maxRetryDelay {
		return delay
	}
	return maxRetryDelay
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
