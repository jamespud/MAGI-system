package server_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"

	"bytes"
	"github.com/jamespud/magi/backend/application/auth"
	"io"
	"iter"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/jamespud/magi/backend/application/audit"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
	"github.com/jamespud/magi/backend/server/a2a"
)

func whoamiRoute(h *hzserver.Hertz) {
	h.GET("/whoami", func(ctx context.Context, c *app.RequestContext) {
		p := auth.PrincipalFrom(ctx)
		if p == nil {
			c.JSON(401, map[string]any{"user": 0})
			return
		}
		c.JSON(200, map[string]any{"user": p.UserID})
	})
}

func TestAuthMiddleware_EnforcesToken(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "admin"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	whoamiRoute(h)

	w := ut.PerformRequest(h.Engine, "GET", "/whoami", nil)
	if w.Code != 401 {
		t.Fatalf("missing token: expected 401, got %d", w.Code)
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "Authorization", Value: "Bearer tok-1"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"user":7`) {
		t.Fatalf("valid token: code=%d body=%s", w.Code, w.Body.String())
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "Authorization", Value: "Bearer wrong"})
	if w.Code != 401 {
		t.Fatalf("wrong token: expected 401, got %d", w.Code)
	}
	w = ut.PerformRequest(h.Engine, "GET", "/whoami", nil, ut.Header{Key: "X-API-Key", Value: "tok-1"})
	if w.Code != 200 {
		t.Fatalf("X-API-Key header: expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_DisabledAllowsOpen(t *testing.T) {
	svc := auth.NewService(false, nil)
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/open", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})

	w := ut.PerformRequest(h.Engine, "GET", "/open", nil)
	if w.Code != 200 {
		t.Fatalf("disabled auth: expected 200, got %d", w.Code)
	}
}

func TestAuth_WellKnownA2APathIsPublic(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/.well-known/agent-card.json", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	w := ut.PerformRequest(h.Engine, "GET", "/.well-known/agent-card.json", nil)
	if w.Code != 200 {
		t.Fatalf("well-known path must be public, got %d", w.Code)
	}
}

func TestAuth_A2ABasePathRequiresToken(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/a2a/tasks", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	unauth := ut.PerformRequest(h.Engine, "GET", "/a2a/tasks", nil)
	if unauth.Code != 401 {
		t.Fatalf("/a2a without token = %d, want 401", unauth.Code)
	}
	assertA2AProtocolError(t, unauth.Body.Bytes(), 401, "UNAUTHENTICATED", "unauthorized")
	authed := ut.PerformRequest(h.Engine, "GET", "/a2a/tasks", nil, ut.Header{Key: "X-API-Key", Value: "tok-1"})
	if authed.Code != 200 {
		t.Fatalf("/a2a with token = %d, want 200", authed.Code)
	}
}

func TestAuth_NonA2AUnauthorizedKeepsLegacyDTO(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/api/v1/decision", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	w := ut.PerformRequest(h.Engine, "GET", "/api/v1/decision", nil)
	if w.Code != 401 {
		t.Fatalf("non-A2A 401 = %d", w.Code)
	}
	var legacy struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy body: %v", err)
	}
	if legacy.Error != "unauthorized" {
		t.Fatalf("legacy body error = %q, want unauthorized", legacy.Error)
	}
}

func assertA2AProtocolError(t *testing.T, body []byte, wantCode int, wantStatus, wantMessage string) {
	t.Helper()
	var parsed struct {
		Error struct {
			Code    int    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				Type   string `json:"@type"`
				Reason string `json:"reason"`
				Domain string `json:"domain"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode A2A protocol error: %v (body=%s)", err, body)
	}
	if parsed.Error.Code != wantCode || parsed.Error.Status != wantStatus || parsed.Error.Message != wantMessage {
		t.Fatalf("protocol error = code=%d status=%s message=%s", parsed.Error.Code, parsed.Error.Status, parsed.Error.Message)
	}
	if len(parsed.Error.Details) != 1 {
		t.Fatalf("details len = %d, want 1", len(parsed.Error.Details))
	}
	d := parsed.Error.Details[0]
	if d.Type != "type.googleapis.com/google.rpc.ErrorInfo" || d.Reason != wantStatus || d.Domain != "a2a-protocol.org" {
		t.Fatalf("error info = %+v", d)
	}
}

func TestRequireAnyRole_GrantsListedRolesAndRejectsOthers(t *testing.T) {
	svc := auth.NewService(true, []auth.KeySpec{
		{Name: "admin", Key: "tok-admin", UserID: 1, Role: "admin"},
		{Name: "ops", Key: "tok-ops", UserID: 2, Role: "operator"},
		{Name: "user", Key: "tok-user", UserID: 3, Role: "user"},
	})
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.Auth(svc))
	h.GET("/ops", server.RequireAnyRole("admin", "operator"), func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})

	for _, tc := range []struct {
		token string
		want  int
	}{
		{token: "tok-admin", want: 200},
		{token: "tok-ops", want: 200},
		{token: "tok-user", want: 403},
	} {
		w := ut.PerformRequest(h.Engine, "GET", "/ops", nil, ut.Header{Key: "Authorization", Value: "Bearer " + tc.token})
		if w.Code != tc.want {
			t.Fatalf("token %s: expected %d, got %d", tc.token, tc.want, w.Code)
		}
	}
}

// serverMemAuditRepo is an in-memory port.AuditRepository for route-level
// tests, mirroring the pattern from application/audit/service_test.go.
type serverMemAuditRepo struct {
	mu     sync.Mutex
	events []*entity.AuditEvent
}

func (m *serverMemAuditRepo) Record(_ context.Context, e *entity.AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *serverMemAuditRepo) List(_ context.Context, _, _ int) ([]*entity.AuditEvent, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]*entity.AuditEvent(nil), m.events...)
	return out, int64(len(out)), nil
}

func (m *serverMemAuditRepo) snapshot() []*entity.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*entity.AuditEvent(nil), m.events...)
}

// fakeA2AHandler implements a2asrv.RequestHandler and records the last
// submitted request so tests can assert principal propagation.
type fakeA2AHandler struct {
	principal *auth.Principal
	sent      *a2a.SendMessageRequest
}

func (f *fakeA2AHandler) SendMessage(ctx context.Context, req *a2a.SendMessageRequest) (a2a.SendMessageResult, error) {
	f.principal = auth.PrincipalFrom(ctx)
	f.sent = req
	return &a2a.Task{ID: "case-1", ContextID: "conv-1", Status: a2a.TaskStatus{State: a2a.TaskStateSubmitted}}, nil
}
func (f *fakeA2AHandler) GetTask(context.Context, *a2a.GetTaskRequest) (*a2a.Task, error) {
	return nil, a2a.ErrTaskNotFound
}
func (f *fakeA2AHandler) ListTasks(context.Context, *a2a.ListTasksRequest) (*a2a.ListTasksResponse, error) {
	return &a2a.ListTasksResponse{}, nil
}
func (f *fakeA2AHandler) CancelTask(context.Context, *a2a.CancelTaskRequest) (*a2a.Task, error) {
	return nil, a2a.ErrTaskNotCancelable
}
func (f *fakeA2AHandler) SubscribeToTask(context.Context, *a2a.SubscribeToTaskRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}
func (f *fakeA2AHandler) SendStreamingMessage(context.Context, *a2a.SendMessageRequest) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {}
}
func (f *fakeA2AHandler) GetTaskPushConfig(context.Context, *a2a.GetTaskPushConfigRequest) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeA2AHandler) ListTaskPushConfigs(context.Context, *a2a.ListTaskPushConfigRequest) (*a2a.ListTaskPushConfigResponse, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeA2AHandler) CreateTaskPushConfig(context.Context, *a2a.PushConfig) (*a2a.PushConfig, error) {
	return nil, a2a.ErrPushNotificationNotSupported
}
func (f *fakeA2AHandler) DeleteTaskPushConfig(context.Context, *a2a.DeleteTaskPushConfigRequest) error {
	return a2a.ErrPushNotificationNotSupported
}
func (f *fakeA2AHandler) GetExtendedAgentCard(context.Context, *a2a.GetExtendedAgentCardRequest) (*a2a.AgentCard, error) {
	return nil, a2a.ErrExtendedCardNotConfigured
}

var _ a2asrv.RequestHandler = (*fakeA2AHandler)(nil)

// startA2AAuditServer mirrors the production global chain
// (A2ATransportRejectionAudit before Auth) plus the mounted A2A transport,
// served over a real loopback listener so the net/http adapter used by the
// A2A transport can write responses (ut.PerformRequest cannot drive it).
func startA2AAuditServer(t *testing.T, authSvc *auth.Service, rateCfg server.RateLimitConfig) (string, *serverMemAuditRepo, *fakeA2AHandler) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	auditRepo := &serverMemAuditRepo{}
	auditSvc := audit.NewService(auditRepo)
	h := hzserver.Default(hzserver.WithHostPorts(addr))
	h.Use(server.RequestID(), server.Logger(), server.Recovery(), server.A2ATransportRejectionAudit(auditSvc), server.Auth(authSvc))
	fake := &fakeA2AHandler{}
	mws := []app.HandlerFunc{}
	if rateCfg.Enabled {
		mws = append(mws, server.RateLimit(rateCfg))
	}
	a2atransport.Mount(h, a2atransport.MountDeps{
		Handler: fake, PublicURL: "https://magi.example.com", BasePath: "/a2a",
		Name: "MAGI", Description: "d", MaxRequestBytes: 98304, Middlewares: mws,
	})
	go func() { h.Spin() }()
	t.Cleanup(func() { _ = h.Shutdown(context.Background()) })

	base := "http://" + addr
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
	return base, auditRepo, fake
}

func a2aSendPost(t *testing.T, url, token string) *http.Response {
	t.Helper()
	body := []byte(`{"message":{"messageId":"m-1","role":"ROLE_USER","parts":[{"text":"hello"}]}}`)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-API-Key", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func drainClose(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// TestA2ATransportRejectionAudit_Unauthenticated401 asserts that a rejected
// A2A request without credentials records exactly one a2a.transport.reject
// row with status 401 and no principal.
func TestA2ATransportRejectionAudit_Unauthenticated401(t *testing.T) {
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	base, auditRepo, _ := startA2AAuditServer(t, authSvc, server.RateLimitConfig{})
	resp := a2aSendPost(t, base+"/a2a/message:send", "")
	defer drainClose(resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	events := auditRepo.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Action != "a2a.transport.reject" || ev.Status != 401 || ev.Resource != "/a2a/message:send" || ev.UserID != 0 {
		t.Fatalf("rejection audit = %+v", ev)
	}
}

// TestA2ATransportRejectionAudit_WellKnownNotRejected asserts discovery is
// public and produces no rejection audit row.
func TestA2ATransportRejectionAudit_WellKnownNotRejected(t *testing.T) {
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	base, auditRepo, _ := startA2AAuditServer(t, authSvc, server.RateLimitConfig{})
	resp, err := http.Get(base + a2atransport.WellKnownAgentCardPath)
	if err != nil {
		t.Fatal(err)
	}
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("well-known status = %d, want 200", resp.StatusCode)
	}
	if events := auditRepo.snapshot(); len(events) != 0 {
		t.Fatalf("well-known must not produce rejection audit, got %d events", len(events))
	}
}

// TestA2ATransportRejectionAudit_SuccessNoRejectRow asserts a successful A2A
// handler response does not create a transport rejection audit row; the
// Handler owns the normal send audit instead.
func TestA2ATransportRejectionAudit_SuccessNoRejectRow(t *testing.T) {
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	base, auditRepo, fake := startA2AAuditServer(t, authSvc, server.RateLimitConfig{})
	resp := a2aSendPost(t, base+"/a2a/message:send", "tok-1")
	defer drainClose(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if fake.sent == nil || fake.principal == nil || fake.principal.UserID != 7 {
		t.Fatalf("principal propagation failed: sent=%+v principal=%+v", fake.sent, fake.principal)
	}
	if events := auditRepo.snapshot(); len(events) != 0 {
		t.Fatalf("successful request must not produce rejection audit, got %d events", len(events))
	}
}
