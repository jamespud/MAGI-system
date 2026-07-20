package decision_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

type blockingOrchestrator struct {
	mu       sync.Mutex
	started  chan struct{}
	cancelCh chan struct{}
	calls    int32
}

func newBlockingOrchestrator() *blockingOrchestrator {
	return &blockingOrchestrator{started: make(chan struct{}, 1), cancelCh: make(chan struct{})}
}

func (b *blockingOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	atomic.AddInt32(&b.calls, 1)
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.cancelCh:
		return nil, errors.New("cancelled")
	}
}

func TestRunManager_StartRunsAsync(t *testing.T) {
	orch := newBlockingOrchestrator()
	rm := decision.NewRunManager(orch)

	c := &entity.DecisionCase{ID: "c1", Question: "q"}
	if err := rm.Start(context.Background(), c); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !rm.IsRunning("c1") {
		t.Fatal("should be running")
	}
	if err := rm.Start(context.Background(), c); !errors.Is(err, decision.ErrAlreadyRunning) {
		t.Fatalf("expected ErrAlreadyRunning, got %v", err)
	}
	<-orch.started
	if err := rm.Cancel("c1"); !err {
		t.Fatal("cancel should return true for running case")
	}
}

func TestRunManager_CancelReturnsFalseWhenNotRunning(t *testing.T) {
	rm := decision.NewRunManager(newBlockingOrchestrator())
	if rm.Cancel("nope") {
		t.Fatal("cancel of non-running case should return false")
	}
}

func TestRunManager_GoroutineSurvivesRequestContext(t *testing.T) {
	orch := newBlockingOrchestrator()
	rm := decision.NewRunManager(orch)
	ctx, cancel := context.WithCancel(context.Background())
	c := &entity.DecisionCase{ID: "c1"}
	if err := rm.Start(ctx, c); err != nil {
		t.Fatalf("start: %v", err)
	}
	cancel() // request context dies
	<-orch.started
	if !rm.IsRunning("c1") {
		t.Fatal("run must survive request context cancellation")
	}
	rm.Cancel("c1")
	time.Sleep(50 * time.Millisecond)
	if rm.IsRunning("c1") {
		t.Fatal("should not be running after explicit cancel")
	}
}

func TestRunManager_CanRestartAfterCompletion(t *testing.T) {
	orch := newBlockingOrchestrator()
	rm := decision.NewRunManager(orch)
	c := &entity.DecisionCase{ID: "c1"}
	if err := rm.Start(context.Background(), c); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-orch.started
	rm.Cancel("c1")
	time.Sleep(50 * time.Millisecond)
	// After the goroutine exits (cancel), the case can be restarted.
	if err := rm.Start(context.Background(), c); err != nil {
		t.Fatalf("restart after completion: %v", err)
	}
	rm.Cancel("c1")
}
