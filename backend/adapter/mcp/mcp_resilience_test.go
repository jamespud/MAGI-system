package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	transport "github.com/mark3labs/mcp-go/client/transport"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpgo_server "github.com/mark3labs/mcp-go/server"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/execution"
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
				w.WriteHeader(http.StatusUnauthorized)
				return
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
	defs, err := a.List(context.Background(), []entity.ToolBinding{
		{Source: entity.ToolSourceMCP, Server: "auth", ToolName: "echo"},
	})
	if err != nil || len(defs) != 1 {
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

// stubTransport answers the handshake and then fails tools/call below the
// protocol. That is the only way to model the case the retry policy is about:
// the request may have reached the server, but no definitive response came
// back. A tool handler that returns an error is NOT that case — the server
// answered, which makes the failure definitive.
type stubTransport struct {
	mu    sync.Mutex
	calls int
	fail  error
}

func (t *stubTransport) Start(context.Context) error                            { return nil }
func (t *stubTransport) Close() error                                           { return nil }
func (t *stubTransport) GetSessionId() string                                   { return "stub-session" }
func (t *stubTransport) SetNotificationHandler(func(mcpgo.JSONRPCNotification)) {}
func (t *stubTransport) SendNotification(context.Context, mcpgo.JSONRPCNotification) error {
	return nil
}

func (t *stubTransport) SendRequest(_ context.Context, req transport.JSONRPCRequest) (*transport.JSONRPCResponse, error) {
	switch req.Method {
	case "tools/call":
		t.mu.Lock()
		t.calls++
		t.mu.Unlock()
		return nil, t.fail
	case "tools/list":
		return &transport.JSONRPCResponse{JSONRPC: mcpgo.JSONRPC_VERSION, ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)}, nil
	default:
		result := fmt.Sprintf(`{"protocolVersion":%q,"capabilities":{},"serverInfo":{"name":"stub","version":"1.0.0"}}`,
			mcpgo.LATEST_PROTOCOL_VERSION)
		return &transport.JSONRPCResponse{JSONRPC: mcpgo.JSONRPC_VERSION, ID: req.ID, Result: json.RawMessage(result)}, nil
	}
}

func (t *stubTransport) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// transportFailureServer hands out clients whose tools/call never returns a
// response, and counts the attempts that reached the transport.
func transportFailureServer() (*stubTransport, func(ServerConfig) (*mcpclient.Client, error)) {
	stub := &stubTransport{fail: errors.New("connection reset by peer")}
	return stub, func(ServerConfig) (*mcpclient.Client, error) {
		return mcpclient.NewClient(stub), nil
	}
}

// The regression the retry policy exists for: an unsafe tool whose outcome is
// unknown must not be re-invoked, and must not be reported as a plain failure
// either — the kernel has to see the ambiguity and fence a later attempt.
func TestAdapter_UnsafeToolTransportFailureIsAmbiguousAndNotRetried(t *testing.T) {
	stub, dial := transportFailureServer()
	a := newWithDial([]ServerConfig{{
		Name: "flaky", Transport: "stdio", Command: "x", RetryAttempts: 3,
	}}, dial)

	_, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "flaky", ToolName: "delete_repo"},
		ArgumentsJSON: `{"id":"1"}`,
		EffectClass:   port.ToolEffectNonIdempotent,
	})
	if !errors.Is(err, execution.ErrExternalOutcomeUnknown) {
		t.Fatalf("error = %v, want it to wrap ErrExternalOutcomeUnknown", err)
	}
	if got := stub.callCount(); got != 1 {
		t.Fatalf("tools/call attempts = %d, want 1: an unsafe tool must never be re-invoked", got)
	}
}

// An unclassified tool is treated the same as an unsafe one: fail closed.
func TestAdapter_UnclassifiedToolIsNotRetried(t *testing.T) {
	stub, dial := transportFailureServer()
	a := newWithDial([]ServerConfig{{
		Name: "flaky", Transport: "stdio", Command: "x", RetryAttempts: 3,
	}}, dial)

	_, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "flaky", ToolName: "plain"},
		ArgumentsJSON: `{}`,
	})
	if !errors.Is(err, execution.ErrExternalOutcomeUnknown) {
		t.Fatalf("error = %v, want it to wrap ErrExternalOutcomeUnknown", err)
	}
	if got := stub.callCount(); got != 1 {
		t.Fatalf("tools/call attempts = %d, want 1 for a tool with no effect class", got)
	}
}

// RetryAttempts bounds transport retries for tools that tolerate a repeat. It
// is honoured — and it is still not an authorisation to re-run anything else.
func TestAdapter_RetriesRetrySafeToolUpToRetryAttempts(t *testing.T) {
	stub, dial := transportFailureServer()
	a := newWithDial([]ServerConfig{{
		Name: "flaky", Transport: "stdio", Command: "x", RetryAttempts: 2,
	}}, dial)

	_, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "flaky", ToolName: "search"},
		ArgumentsJSON: `{"q":"x"}`,
		EffectClass:   port.ToolEffectReadOnly,
	})
	if !errors.Is(err, execution.ErrExternalOutcomeUnknown) {
		t.Fatalf("error = %v, want it to wrap ErrExternalOutcomeUnknown", err)
	}
	if got := stub.callCount(); got != 3 {
		t.Fatalf("tools/call attempts = %d, want 3 (retry_attempts=2 plus the first call)", got)
	}
}

// A JSON-RPC error response is definitive: the server ran the call and refused
// it. Re-issuing it here would be a semantic retry, which belongs to the kernel
// even for a retry-safe tool.
func TestAdapter_DoesNotRetryDefinitiveServerError(t *testing.T) {
	srv := mcpgo_server.NewMCPServer("failing", "1.0.0")
	var mu sync.Mutex
	calls := 0
	srv.AddTool(mcpgo.NewTool("delete_repo"), func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, errors.New("repo is protected")
	})
	a := newWithDial([]ServerConfig{{
		Name: "failing", Transport: "stdio", Command: "x", RetryAttempts: 3,
	}}, inProcessDial(srv))

	_, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:     entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "failing", ToolName: "delete_repo"},
		EffectClass: port.ToolEffectReadOnly,
	})
	if err == nil {
		t.Fatal("execute returned nil error, want the server error")
	}
	if errors.Is(err, execution.ErrExternalOutcomeUnknown) {
		t.Fatalf("definitive server error was reported as an unknown outcome: %v", err)
	}
	if calls != 1 {
		t.Fatalf("tool handler calls = %d, want 1", calls)
	}
}

// A retry-safe tool survives a transport failure: reconnecting and re-issuing
// the call is exactly what RetryAttempts is for.
func TestAdapter_ReconnectsRetrySafeToolAfterTransportFailure(t *testing.T) {
	good := mcpgo_server.NewMCPServer("good", "1.0.0")
	good.AddTool(mcpgo.NewTool("echo", mcpgo.WithString("text", mcpgo.Required())),
		func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			text, _ := req.GetArguments()["text"].(string)
			return &mcpgo.CallToolResult{
				Content: []mcpgo.Content{mcpgo.TextContent{Type: "text", Text: "ok:" + text}},
			}, nil
		})
	stub, _ := transportFailureServer()
	var mu sync.Mutex
	dials := 0
	dial := func(ServerConfig) (*mcpclient.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		dials++
		if dials == 1 {
			return mcpclient.NewClient(stub), nil
		}
		return mcpclient.NewInProcessClient(good)
	}
	a := newWithDial([]ServerConfig{{
		Name: "flaky", Transport: "stdio", Command: "x", RetryAttempts: 1,
	}}, dial)

	res, err := a.Execute(context.Background(), port.ToolExecutionRequest{
		Binding:       entity.ToolBinding{Source: entity.ToolSourceMCP, Server: "flaky", ToolName: "echo"},
		ArgumentsJSON: `{"text":"x"}`,
		EffectClass:   port.ToolEffectReadOnly,
	})
	if err != nil {
		t.Fatalf("execute after reconnect: %v", err)
	}
	if res == nil || res.Output != "ok:x" {
		t.Fatalf("execute after reconnect: res=%+v", res)
	}
	if got := stub.callCount(); got != 1 {
		t.Fatalf("attempts against the failing transport = %d, want 1", got)
	}
}
