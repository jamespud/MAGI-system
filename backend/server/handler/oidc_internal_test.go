package handler

import (
	"strconv"
	"testing"
	"time"
)

func TestOIDCStateStore_PrunesExpired(t *testing.T) {
	s := &oidcStateStore{}
	now := time.Now()
	s.mu.Lock()
	s.states = map[string]time.Time{
		"expired": now.Add(-time.Minute),
		"live":    now.Add(time.Minute),
	}
	s.lastPrune = now.Add(-2 * oidcStatePruneGap)
	s.pruneLocked(now)
	s.mu.Unlock()

	if _, ok := s.states["expired"]; ok {
		t.Fatal("expired state should be pruned")
	}
	if _, ok := s.states["live"]; !ok {
		t.Fatal("live state should be kept")
	}
}

func TestOIDCStateStore_CapsEntries(t *testing.T) {
	s := &oidcStateStore{}
	now := time.Now()
	s.mu.Lock()
	s.states = make(map[string]time.Time, oidcStateMaxEntries)
	for i := 0; i < oidcStateMaxEntries; i++ {
		s.states[strconv.Itoa(i)] = now.Add(time.Minute)
	}
	// Make the next prune a no-op so the cap, not expiry, is what rejects.
	s.lastPrune = now.Add(oidcStatePruneGap)
	s.mu.Unlock()

	if _, err := s.issue(); err == nil {
		t.Fatal("issue must fail once the pending-state cap is reached")
	}
}

func TestOIDCStateStore_ConsumeIsSingleUse(t *testing.T) {
	s := &oidcStateStore{}
	state, err := s.issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !s.consume(state) {
		t.Fatal("first consume should succeed")
	}
	if s.consume(state) {
		t.Fatal("state must be single-use")
	}
	if s.consume("unknown") {
		t.Fatal("unknown state must not be accepted")
	}
}
