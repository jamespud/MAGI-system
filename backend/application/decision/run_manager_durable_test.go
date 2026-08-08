package decision_test

import (
	"context"
	"errors"
	"sync"
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

type durableRetryOrchestrator struct {
	calls int32
}

func (o *durableRetryOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	if atomic.AddInt32(&o.calls, 1) == 1 {
		return nil, errors.New("transient model failure")
	}
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func openJobDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.DecisionJobModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func waitJobStatus(t *testing.T, repo interface {
	GetByCase(context.Context, string) (*entity.DecisionJob, error)
}, caseID string, want entity.DecisionJobStatus) *entity.DecisionJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetByCase(context.Background(), caseID)
		if err == nil && job.Status == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := repo.GetByCase(context.Background(), caseID)
	t.Fatalf("job %s did not reach %s: %+v", caseID, want, job)
	return nil
}

func TestRunManager_DurableRetry(t *testing.T) {
	db := openJobDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &durableRetryOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-test", MaxAttempts: 2, RetryBase: 10 * time.Millisecond,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-retry"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, "case-retry", entity.DecisionJobSucceeded)
	if job.Attempt != 2 || atomic.LoadInt32(&orch.calls) != 2 {
		t.Fatalf("retry result: job=%+v calls=%d", job, orch.calls)
	}
}

func TestRunManager_RecoverQueuedJob(t *testing.T) {
	db := openJobDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	if _, err := jobs.Enqueue(context.Background(), "case-recover", 2); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	orch := &durableRetryOrchestrator{}
	case_ := &entity.DecisionCase{ID: "case-recover"}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: &stubCaseRepo{case_: case_}, WorkerID: "worker-recover", MaxAttempts: 2, RetryBase: 10 * time.Millisecond,
	})
	if err := rm.Recover(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	job := waitJobStatus(t, jobs, "case-recover", entity.DecisionJobSucceeded)
	if job.Attempt != 2 || atomic.LoadInt32(&orch.calls) != 2 {
		t.Fatalf("recovery result: job=%+v calls=%d", job, orch.calls)
	}
}

type blockingUserOrchestrator struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (o *blockingUserOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	o.once.Do(func() { close(o.started) })
	<-o.release
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestRunManager_EnforcesPerUserConcurrencyLimit(t *testing.T) {
	orch := &blockingUserOrchestrator{started: make(chan struct{}), release: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{MaxConcurrentRunsPerUser: 1})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 1}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	<-orch.started
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c2", UserID: 1}); !errors.Is(err, decision.ErrRateLimited) {
		t.Fatalf("second start for same user: expected ErrRateLimited, got %v", err)
	}
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c3", UserID: 2}); err != nil {
		t.Fatalf("other user start: %v", err)
	}
	close(orch.release)
	rm.Cancel("c1")
	rm.Cancel("c3")
}
