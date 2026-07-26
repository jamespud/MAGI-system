package rag

import (
	"context"
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
func (c *countingAdapter) Store(ctx context.Context, proj *entity.CaseMemoryProjection) error {
	atomic.AddInt32(&c.count, 1)
	return nil
}

func TestAsyncIndexerReturnsImmediately(t *testing.T) {
	inner := &countingAdapter{}
	idx := NewAsyncIndexer(inner, nil, 2)
	start := time.Now()
	_ = idx.Store(context.Background(), &entity.CaseMemoryProjection{CaseID: "c1"})
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
