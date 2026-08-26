package server_test

import (
	"testing"

	"net/http"

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
