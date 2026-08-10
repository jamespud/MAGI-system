package decision_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeRunCounter struct {
	acquired atomic.Int64
	released atomic.Int64
}

type countingOrchestrator struct{ calls atomic.Int64 }

func (c *countingOrchestrator) Orchestrate(ctx context.Context, case_ *entity.DecisionCase) (*entity.Resolution, error) {
	c.calls.Add(1)
	return &entity.Resolution{CaseID: case_.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func (f *fakeRunCounter) Acquire(ctx context.Context, userID int64, limit int) (bool, error) {
	f.acquired.Add(1)
	return true, nil
}

func (f *fakeRunCounter) Release(ctx context.Context, userID int64) error {
	f.released.Add(1)
	return nil
}

func TestRunManager_SlotAcquiredAndReleasedOnCompletion(t *testing.T) {
	orch := &countingOrchestrator{}
	counter := &fakeRunCounter{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		MaxConcurrentRunsPerUser: 2,
		RunCounter:               counter,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c-slot", UserID: 7}); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && rm.IsRunning("c-slot") {
		time.Sleep(5 * time.Millisecond)
	}
	if rm.IsRunning("c-slot") {
		t.Fatal("run should have completed")
	}
	if counter.acquired.Load() != 1 {
		t.Fatalf("expected one acquire, got %d", counter.acquired.Load())
	}
	if counter.released.Load() != 1 {
		t.Fatalf("expected one release, got %d", counter.released.Load())
	}
}

func TestRunManager_SlotReleasedOnAlreadyCompleted(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.DecisionJobModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	orch := &countingOrchestrator{}
	counter := &fakeRunCounter{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		MaxConcurrentRunsPerUser: 2,
		RunCounter:               counter,
		JobRepo:                  jobs,
		WorkerID:                 "worker-slot",
		MaxAttempts:              1,
		RetryBase:                time.Millisecond,
	})
	c := &entity.DecisionCase{ID: "c-slot2", UserID: 7}
	if err := rm.Start(context.Background(), c); err != nil {
		t.Fatalf("first start: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && rm.IsRunning(c.ID) {
		time.Sleep(5 * time.Millisecond)
	}
	// Second start for the same (now completed) case: the enqueue path finds
	// the succeeded job and must release the freshly acquired slot.
	if err := rm.Start(context.Background(), c); err != decision.ErrAlreadyCompleted {
		t.Fatalf("second start: want ErrAlreadyCompleted, got %v", err)
	}
	if counter.acquired.Load() != 2 || counter.released.Load() != 2 {
		t.Fatalf("acquire/release imbalance: acquired=%d released=%d", counter.acquired.Load(), counter.released.Load())
	}
}
