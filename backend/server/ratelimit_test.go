package server_test

import (
	"testing"

	"context"
	"encoding/json"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	hzserver "github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/server"
)

func TestRateLimiter_AllowsWithinBudget(t *testing.T) {
	lim := server.NewRateLimiter(2, 5)
	for i := 0; i < 2; i++ {
		ok, _ := lim.Allow(1, "1.2.3.4")
		if !ok {
			t.Fatalf("call %d within per-user budget should be allowed", i+1)
		}
	}
	if ok, _ := lim.Allow(1, "1.2.3.4"); ok {
		t.Fatal("third call for user 1 must be denied")
	}
	// A different user shares no budget.
	if ok, _ := lim.Allow(2, "1.2.3.4"); !ok {
		t.Fatal("a different user must have its own budget")
	}
}

func TestRateLimiter_IPFallback(t *testing.T) {
	// per-user disabled, per-IP enforced
	lim := server.NewRateLimiter(0, 2)
	if ok, _ := lim.Allow(0, "9.9.9.9"); !ok {
		t.Fatal("first ip call should be allowed")
	}
	if ok, _ := lim.Allow(0, "9.9.9.9"); !ok {
		t.Fatal("second ip call should be allowed")
	}
	if ok, _ := lim.Allow(0, "9.9.9.9"); ok {
		t.Fatal("third ip call must be denied")
	}
	if ok, _ := lim.Allow(0, "8.8.8.8"); !ok {
		t.Fatal("a different ip must be allowed")
	}
}

func TestRateLimit_A2A429UsesProtocolEnvelope(t *testing.T) {
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.RateLimit(server.RateLimitConfig{Enabled: true, PerIPPerMinute: 1}))
	h.GET("/a2a/tasks", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	h.GET("/api/v1/decision", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	ip := ut.Header{Key: "X-Forwarded-For", Value: "5.5.5.5"}

	if w := ut.PerformRequest(h.Engine, "GET", "/a2a/tasks", nil, ip); w.Code != 200 {
		t.Fatalf("first /a2a = %d, want 200", w.Code)
	}
	w := ut.PerformRequest(h.Engine, "GET", "/a2a/tasks", nil, ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second /a2a = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatal("429 must carry Retry-After")
	}
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
	if err := json.Unmarshal(w.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if parsed.Error.Code != 429 || parsed.Error.Status != "RESOURCE_EXHAUSTED" || parsed.Error.Message != "rate limit exceeded" {
		t.Fatalf("429 protocol error = %+v", parsed.Error)
	}
	if len(parsed.Error.Details) != 1 || parsed.Error.Details[0].Domain != "a2a-protocol.org" {
		t.Fatalf("429 details = %+v", parsed.Error.Details)
	}
}

func TestRateLimit_NonA2A429KeepsLegacyDTO(t *testing.T) {
	h := hzserver.Default(hzserver.WithHostPorts("127.0.0.1:0"))
	h.Use(server.RateLimit(server.RateLimitConfig{Enabled: true, PerIPPerMinute: 1}))
	h.GET("/api/v1/decision", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(200, map[string]any{"ok": true})
	})
	ip := ut.Header{Key: "X-Forwarded-For", Value: "6.6.6.6"}
	if w := ut.PerformRequest(h.Engine, "GET", "/api/v1/decision", nil, ip); w.Code != 200 {
		t.Fatalf("first = %d", w.Code)
	}
	w := ut.PerformRequest(h.Engine, "GET", "/api/v1/decision", nil, ip)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second = %d, want 429", w.Code)
	}
	var legacy struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &legacy); err != nil {
		t.Fatalf("decode legacy 429: %v", err)
	}
	if legacy.Error != "rate limit exceeded" {
		t.Fatalf("legacy 429 error = %q", legacy.Error)
	}
}

func TestRateLimiter_DisabledIsAlwaysAllowed(t *testing.T) {
	lim := server.NewRateLimiter(0, 0)
	for i := 0; i < 100; i++ {
		if ok, _ := lim.Allow(1, "1.1.1.1"); !ok {
			t.Fatal("disabled limiter must always allow")
		}
	}
}

// TestA2ATransportRejectionAudit_RateLimited429 asserts that an authenticated
// request over the A2A rate budget records one a2a.transport.reject row with
// status 429 while retaining the authenticated principal.
func TestA2ATransportRejectionAudit_RateLimited429(t *testing.T) {
	authSvc := auth.NewService(true, []auth.KeySpec{{Name: "a", Key: "tok-1", UserID: 7, Role: "user"}})
	base, auditRepo, _ := startA2AAuditServer(t, authSvc, server.RateLimitConfig{Enabled: true, PerUserPerMinute: 1})

	first := a2aSendPost(t, base+"/a2a/message:send", "tok-1")
	if first.StatusCode != http.StatusOK {
		drainClose(first)
		t.Fatalf("first request = %d, want 200", first.StatusCode)
	}
	drainClose(first)

	second := a2aSendPost(t, base+"/a2a/message:send", "tok-1")
	if second.StatusCode != http.StatusTooManyRequests {
		drainClose(second)
		t.Fatalf("second request = %d, want 429", second.StatusCode)
	}
	drainClose(second)

	events := auditRepo.snapshot()
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Action != "a2a.transport.reject" || ev.Status != http.StatusTooManyRequests || ev.Resource != "/a2a/message:send" {
		t.Fatalf("rate-limited rejection audit = %+v", ev)
	}
	if ev.UserID != 7 || ev.Username == "" || ev.Role == "" {
		t.Fatalf("rate-limited rejection audit must retain principal: %+v", ev)
	}
}
