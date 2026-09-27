package dataset_test
 
import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
 
	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
)
 
// awaitStub replays a scripted sequence of persisted run states, so AwaitRun's
// polling can be tested without a wall-clock wait or a real worker.
type awaitStub struct {
	*stubDatasetRepo
	mu       sync.Mutex
	states   []entity.BenchmarkRunStatus
	readings int
}
 
func (s *awaitStub) GetRun(_ context.Context, id string) (*entity.BenchmarkRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readings++
	idx := s.readings - 1
	if idx >= len(s.states) {
		idx = len(s.states) - 1
	}
	return &entity.BenchmarkRun{
		ID: id, Status: s.states[idx], Total: 4, Matched: 2,
		RegressionThreshold: 0.9, RegressionFailed: true,
	}, nil
}
 
// Issue #12: the lifecycle worker must read the persisted terminal verdict, not
// the still-queued object RunAutoRegression returns.
func TestService_AwaitRunReturnsTheTerminalVerdict(t *testing.T) {
	repo := &awaitStub{stubDatasetRepo: newStubDatasetRepo(), states: []entity.BenchmarkRunStatus{
		entity.BenchmarkRunQueued, entity.BenchmarkRunRunning, entity.BenchmarkRunFailed,
	}}
	svc := dataset.NewService(repo, nil, &stubOrch{}, 2, dataset.WithRunPollInterval(time.Millisecond))
 
	run, err := svc.AwaitRun(context.Background(), "run-await")
	if err != nil {
		t.Fatalf("await run: %v", err)
	}
	if run == nil || run.Status != entity.BenchmarkRunFailed {
		t.Fatalf("await must return the terminal run, got %+v", run)
	}
	if repo.readings < 3 {
		t.Fatalf("await returned before the run was terminal: %d reads", repo.readings)
	}
}
 
func TestService_AwaitRunReportsTimeoutWhenTheRunNeverFinishes(t *testing.T) {
	repo := &awaitStub{stubDatasetRepo: newStubDatasetRepo(), states: []entity.BenchmarkRunStatus{
		entity.BenchmarkRunRunning,
	}}
	svc := dataset.NewService(repo, nil, &stubOrch{}, 2, dataset.WithRunPollInterval(time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
 
	if _, err := svc.AwaitRun(ctx, "run-stuck"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await must surface the deadline, got %v", err)
	}
}
