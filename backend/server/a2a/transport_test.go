package a2atransport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/a2a"
)

func emptyStream() iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}

// fakeHandler captures the authenticated principal and echoes a submitted task.
type fakeHandler struct {
	mu        sync.Mutex
	principal *auth.Principal
	sent      *a2a.SendMessageRequest
}

func (f *fakeHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.principal = auth.PrincipalFrom(ctx)
	f.sent = req
	return &a2a.Task{ID: "case-1", ContextID: "conv-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}, nil
}

// snapshot returns the last captured principal and request under the lock so
// race-detector runs can read them after an async HTTP round trip.
func (f *fakeHandler) snapshot() (*auth.Principal, *a2a.SendMessageRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.principal, f.sent
}
func (f *fakeHandler) GetTask(ctx context.Context, req *a2a.GetTaskRequest) (*a2a.Task, error) {
	return nil, a2a.ErrTaskNotFound
}
func (f *fakeHandler) ListTasks(ctx context.Context, req *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return &a2a.ListTasksResponse{}, nil
}
func (f *fakeHandler) CancelTask(ctx context.Context, req *a2a.CancelTaskRequest) (*a2a.Task, error) {
	return nil, a2a.ErrTaskNotCancelable
}
func (f *fakeHandler) SubscribeToTask(ctx context.Context, req *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return emptyStream()
}
func (f *fakeHandler) SendStreamingMessage(ctx context.Context, req *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return emptyStream()
}
func (f *fakeHandler) GetTaskPushConfig(ctx context.Context, req *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeHandler) ListTaskPushConfigs(ctx context.Context, req *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeHandler) CreateTaskPushConfig(ctx context.Context, req *a2a.PushConfig) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeHandler) DeleteTaskPushConfig(ctx context.Context, req *a2a.DeleteTaskPushConfigRequest) error {
	return a2a.ErrPushNotificationNotSupported
}
func (f *fakeHandler) GetExtendedAgentCard(ctx context.Context, req *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	return nil, a2a.ErrExtendedCardNotConfigured
}

var _ a2asrv.RequestHandler = (*fakeHandler)(nil)

func TestAgentCard_SnapshotIsStableAndLeakFree(t *testing.T) {
	card := a2atransport.AgentCard("https://magi.example/", "/a2a", "MAGI Decision", "evidence-driven decisions")
	if strings.TrimSpace(card.Version) == "" {
		t.Fatal("agent card version must not be empty")
	}
	if len(card.SupportedInterfaces) != 1 {
		t.Fatalf("interfaces = %d", len(card.SupportedInterfaces))
	}
	iface := card.SupportedInterfaces[0]
	if iface.URL != "https://magi.example/a2a" || iface.ProtocolBinding != a2a.TransportProtocolHTTPJSON {
		t.Fatalf("interface = %+v", iface)
	}
	if iface.ProtocolVersion != a2a.Version {
		t.Fatalf("protocol version = %s, want %s", iface.ProtocolVersion, a2a.Version)
	}
	if strings.Contains(iface.URL, "/v2") {
		t.Fatalf("module major version leaked into protocol URL: %s", iface.URL)
	}
	if !card.Capabilities.Streaming || card.Capabilities.PushNotifications {
		t.Fatalf("capabilities = %+v", card.Capabilities)
	}
	if strings.Join(card.DefaultInputModes, ",") != "text/plain" {
		t.Fatalf("input modes = %v", card.DefaultInputModes)
	}
	if strings.Join(card.DefaultOutputModes, ",") != "text/markdown,application/json" {
		t.Fatalf("output modes = %v", card.DefaultOutputModes)
	}
	if len(card.Skills) != 1 || card.Skills[0].ID != "evidence-driven-decision" {
		t.Fatalf("skills = %+v", card.Skills)
	}
	if _, ok := card.SecuritySchemes["bearer"]; !ok {
		t.Fatal("missing bearer scheme")
	}
	if _, ok := card.SecuritySchemes["apiKey"]; !ok {
		t.Fatal("missing apiKey scheme")
	}
	if len(card.SecurityRequirements) != 2 {
		t.Fatalf("security requirements = %+v", card.SecurityRequirements)
	}

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if v, _ := decoded["version"].(string); v == "" {
		t.Fatalf("agent card JSON missing non-empty version: %s", raw)
	}
	if _, ok := decoded["securitySchemes"].(map[string]any)["bearer"]; !ok {
		t.Fatalf("agent card JSON missing bearer scheme: %s", raw)
	}
	if _, ok := decoded["securitySchemes"].(map[string]any)["apiKey"]; !ok {
		t.Fatalf("agent card JSON missing apiKey scheme: %s", raw)
	}
	for _, leaked := range []string{"sk-", "gpt-", "127.0.0.1", "db.internal", "magi-mysql", "tenant-", "magi-db"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("card leaks %q: %s", leaked, raw)
		}
	}
}

func TestRESTHandler_StripsBasePathAndKeepsSDKPaths(t *testing.T) {
	fake := &fakeHandler{}
	rest := http.StripPrefix("/a2a", a2asrv.NewRESTHandler(fake))
	body := []byte(`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"hello"}]}}`)
	req, err := http.NewRequest(http.MethodPost, "https://magi.example/a2a/message:send", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	rest.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("send status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, sent := fake.snapshot(); sent == nil || sent.Message == nil || sent.Message.ID != "m-1" {
		t.Fatalf("fake received = %+v", sent)
	}
}

func TestMount_NotRegisteredReturns404(t *testing.T) {
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	if w := ut.PerformRequest(h.Engine, http.MethodGet, a2atransport.WellKnownAgentCardPath, nil); w.Code != http.StatusNotFound {
		t.Fatalf("well-known when disabled = %d, want 404", w.Code)
	}
	if w := ut.PerformRequest(h.Engine, http.MethodGet, "/a2a/tasks", nil); w.Code != http.StatusNotFound {
		t.Fatalf("/a2a when disabled = %d, want 404", w.Code)
	}
}

func TestMount_AuthBridgePropagatesPrincipal(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	fake := &fakeHandler{}
	baseURL := startA2AMount(t, svc, fake, "/a2a")
	body := []byte(`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"hello"}]}}`)

	if resp, err := http.Get(baseURL + a2atransport.WellKnownAgentCardPath); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("well-known without token = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	req, _ := http.NewRequest(http.MethodPost, baseURL+"/a2a/message:send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusUnauthorized {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		t.Fatalf("a2a without token = %d, want 401", resp.StatusCode)
	} else {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	req, _ = http.NewRequest(http.MethodPost, baseURL+"/a2a/message:send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "tok-1")
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("a2a with api key = %d body=%s", resp.StatusCode, data)
	} else {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	principal, sent := fake.snapshot()
	if sent == nil || principal == nil || principal.UserID != 7 {
		t.Fatalf("principal propagation failed: sent=%+v principal=%+v", sent, principal)
	}
}

func TestMount_RejectsWrongToken(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	baseURL := startA2AMount(t, svc, &fakeHandler{}, "/a2a")
	body := []byte(`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"hello"}]}}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/a2a/message:send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode != http.StatusUnauthorized {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		t.Fatalf("wrong token = %d, want 401", resp.StatusCode)
	} else {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// TestMount_RejectsOversizedRequestBody guards the MaxBytesHandler wiring: a
// request body larger than the configured transport cap is rejected before the
// SDK JSON parser runs, so huge metadata can never be decoded or hashed.
func TestMount_RejectsOversizedRequestBody(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.Use(server.Auth(svc))
	a2atransport.Mount(h, a2atransport.MountDeps{
		Handler: &fakeHandler{}, PublicURL: "http://" + addr, BasePath: "/a2a",
		Name: "MAGI", Description: "d", MaxRequestBytes: 128,
	})
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	baseURL := "http://" + addr
	body := []byte(`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"` + strings.Repeat("x", 512) + `"}]}}`)
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/a2a/message:send", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "tok-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("oversized body = %d, want 413 body=%s", resp.StatusCode, data)
	}
}

func TestMount_RejectsChunkedOversizedRequestBody(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.Use(server.Auth(svc))
	a2atransport.Mount(h, a2atransport.MountDeps{
		Handler: &fakeHandler{}, PublicURL: "http://" + addr, BasePath: "/a2a",
		Name: "MAGI", Description: "d", MaxRequestBytes: 128,
	})
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		if conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond); err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	body := `{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"` + strings.Repeat("x", 512) + `"}]}}`
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/a2a/message:send", io.LimitReader(strings.NewReader(body), int64(len(body))))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "tok-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("chunked oversized body = %d, want 413 body=%s", resp.StatusCode, data)
	}
}

func startA2AMount(t *testing.T, authSvc *auth.Service, fake a2asrv.RequestHandler, basePath string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.Use(server.Auth(authSvc))
	a2atransport.Mount(h, a2atransport.MountDeps{
		Handler: fake, PublicURL: "http://" + addr, BasePath: basePath,
		Name: "MAGI", Description: "d",
	})
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "http://" + addr
}
