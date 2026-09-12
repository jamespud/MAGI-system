package server

import (
	"testing"
	"time"
)

// TestAllowWindow_RollsOverAtOneMinute pins the fixed-window boundary: a
// saturated window must reset exactly one minute after it opened, not two.
func TestAllowWindow_RollsOverAtOneMinute(t *testing.T) {
	m := map[int64]*rateWindow{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if ok, _ := allowWindow(m, int64(1), 1, start); !ok {
		t.Fatal("first call should be allowed")
	}
	if ok, _ := allowWindow(m, int64(1), 1, start.Add(30*time.Second)); ok {
		t.Fatal("second call inside the window must be denied")
	}
	// At exactly one minute the window must have rolled over.
	if ok, _ := allowWindow(m, int64(1), 1, start.Add(time.Minute)); !ok {
		t.Fatal("window must reset at the one-minute boundary, not two")
	}
	if w := m[int64(1)]; !w.reset.Equal(start.Add(2 * time.Minute)) {
		t.Fatalf("reset = %v, want %v", w.reset, start.Add(2*time.Minute))
	}
}

// TestAllowWindow_RetryAfterIsBounded ensures the Retry-After estimate never
// exceeds the window length after the reset fix.
func TestAllowWindow_RetryAfterIsBounded(t *testing.T) {
	m := map[string]*rateWindow{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if ok, _ := allowWindow(m, "ip", 1, start); !ok {
		t.Fatal("first call should be allowed")
	}
	// Deny 30s into the window: ~30s remain, so Retry-After must be ~31.
	ok, retryAfter := allowWindow(m, "ip", 1, start.Add(30*time.Second))
	if ok {
		t.Fatal("second call should be denied")
	}
	if retryAfter < 1 || retryAfter > 31 {
		t.Fatalf("retryAfter = %d, want 1..31", retryAfter)
	}
}
