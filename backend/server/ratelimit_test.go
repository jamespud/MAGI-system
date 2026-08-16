package server_test

import (
	"testing"

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
