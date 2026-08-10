package server_test

import (
	"context"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server"
)

// TestEventBroker_TracksDroppedEvents guards SSE reliability: when a slow
// subscriber's channel is full, events must not be silently lost without a
// signal. The broker counts drops so operators/frontends can detect gaps
// (the frontend already refetches on sequence gaps).
func TestEventBroker_TracksDroppedEvents(t *testing.T) {
	b := server.NewEventBrokerWithBuffer(2)
	ch := b.Subscribe("c1")
	defer b.Unsubscribe("c1", ch)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := b.Publish(ctx, entity.NewEvent("c1", "", nil, entity.EventVoteSubmitted, map[string]any{"n": i})); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	// Subscriber never reads: exactly 2 fit in the buffer, 3 are dropped.
	if got := b.Dropped(); got != 3 {
		t.Fatalf("expected 3 dropped events, got %d", got)
	}
}

// TestEventBroker_NoDropWhenConsuming guards the happy path: an actively
// consuming subscriber must not lose events.
func TestEventBroker_NoDropWhenConsuming(t *testing.T) {
	b := server.NewEventBrokerWithBuffer(4)
	ch := b.Subscribe("c1")
	defer b.Unsubscribe("c1", ch)

	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			<-ch
		}
	}()
	for i := 0; i < 3; i++ {
		_ = b.Publish(ctx, entity.NewEvent("c1", "", nil, entity.EventVoteSubmitted, nil))
	}
	<-done
	if got := b.Dropped(); got != 0 {
		t.Fatalf("expected 0 dropped events with active consumer, got %d", got)
	}
}
