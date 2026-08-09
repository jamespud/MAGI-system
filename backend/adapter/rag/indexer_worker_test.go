package rag

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type countingAdapter struct {
	count int32
}

func (c *countingAdapter) Retrieve(ctx context.Context, req port.RetrieveRequest) (port.RetrieveResult, error) {
	return port.RetrieveResult{}, nil
}
func (c *countingAdapter) Store(ctx context.Context, proj *entity.CaseMemoryProjection) (port.StoreStats, error) {
	atomic.AddInt32(&c.count, 1)
	return port.StoreStats{Chunks300: 5, Chunks900: 2, Chunks1800: 1}, nil
}

func TestAsyncIndexerReturnsImmediately(t *testing.T) {
	inner := &countingAdapter{}
	idx := NewAsyncIndexer(inner, nil, 2)
	start := time.Now()
	_, _ = idx.Store(context.Background(), &entity.CaseMemoryProjection{CaseID: "c1"})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("Store took %v, should return immediately", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&inner.count) >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("worker did not process; count = %d", inner.count)
}

func TestAsyncIndexerPublishesIndexedEventWithStats(t *testing.T) {
	inner := &countingAdapter{}
	pub := &recordingPublisher{}
	idx := NewAsyncIndexer(inner, pub, 2)

	_, _ = idx.Store(context.Background(), &entity.CaseMemoryProjection{CaseID: "c1"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(pub.events) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(pub.events) != 1 {
		t.Fatalf("expected one published event, got %d", len(pub.events))
	}
	ev := pub.events[0]
	if ev.Type != entity.EventMemoryIndexed {
		t.Fatalf("event type = %s, want MEMORY_INDEXED", ev.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload["chunks_300"] != float64(5) || payload["chunks_900"] != float64(2) || payload["chunks_1800"] != float64(1) {
		t.Errorf("payload stats = %v, want 300=5 900=2 1800=1", payload)
	}
}
