package decision_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMultiDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.DecisionJobModel{}, &magi.CaseModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type remoteCancelOrchestrator struct {
	started   chan struct{}
	cancelled chan struct{}
}

type retryClaimCancellingRepo struct {
	port.DecisionJobRepository
	cancelAfterClaim func()
}

func (r *retryClaimCancellingRepo) Claim(ctx context.Context, jobID, workerID string, leaseUntil time.Time) (*entity.DecisionJob, bool, error) {
	job, ok, err := r.DecisionJobRepository.Claim(ctx, jobID, workerID, leaseUntil)
	if err == nil && ok && job.Attempt > 1 {
		r.cancelAfterClaim()
	}
	return job, ok, err
}

type retryResetOrchestrator struct{ calls atomic.Int32 }

func (o *retryResetOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	o.calls.Add(1)
	return &entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil
}

type failThenSucceedCaseOrchestrator struct {
	caseRepo port.CaseRepository
	calls    atomic.Int32
}

func (o *failThenSucceedCaseOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	if o.calls.Add(1) == 1 {
		writer, ok := o.caseRepo.(port.ConditionalCaseStatusWriter)
		if !ok {
			return nil, errors.New("case repository lacks conditional writer")
		}
		updated, err := writer.UpdateStatusIfCurrent(ctx, c.ID, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusFailed)
		if err != nil || !updated {
			return nil, errors.New("failed to persist failed case state")
		}
		c.Status = entity.CaseStatusFailed
		return nil, errors.New("transient orchestration failure")
	}
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func (o *remoteCancelOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	close(o.started)
	<-ctx.Done()
	close(o.cancelled)
	return nil, context.Cause(ctx)
}

func TestRunManager_RemoteCancelStopsWorkerAndFencesLateTerminalWrite(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-remote-cancel"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	lease := 30 * time.Millisecond
	orch := &remoteCancelOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rmB := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", LeaseDuration: lease, MaxAttempts: 1,
	})
	if err := rmB.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start replica B: %v", err)
	}
	defer rmB.Cancel(caseID)
	<-orch.started

	job, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if err := jobs.Cancel(context.Background(), job.ID); err != nil {
		t.Fatalf("replica A cancel job: %v", err)
	}
	if err := repo.CaseRepo().UpdateStatus(context.Background(), caseID, entity.CaseStatusCancelled); err != nil {
		t.Fatalf("replica A cancel case: %v", err)
	}

	select {
	case <-orch.cancelled:
	case <-time.After(5 * lease):
		t.Fatal("remote worker did not stop after lease/status loss")
	}

	writer, ok := repo.CaseRepo().(port.ConditionalCaseStatusWriter)
	if !ok {
		t.Fatal("production case repository must provide conditional status writes")
	}
	updated, err := writer.UpdateStatusIfCurrent(context.Background(), caseID,
		[]entity.CaseStatus{entity.CaseStatusEvaluating}, entity.CaseStatusResolved)
	if err != nil {
		t.Fatalf("late terminal write: %v", err)
	}
	if updated {
		t.Fatal("late terminal write must not overwrite CANCELLED")
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusCancelled {
		t.Fatalf("case after late write = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil || jobAfter.Status != entity.DecisionJobCancelled {
		t.Fatalf("job after late write = %+v err=%v", jobAfter, err)
	}
}

func TestRunManager_RetryResetDoesNotReviveRemoteCancelledCase(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-retry-reset-fenced"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	job, err := jobs.Enqueue(context.Background(), caseID, 2)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, ok, err := jobs.Claim(context.Background(), job.ID, "previous-worker", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("seed claim: ok=%v err=%v", ok, err)
	}
	retryAt := time.Now().Add(-time.Millisecond)
	if err := jobs.MarkFailed(context.Background(), job.ID, "previous-worker", "retry", &retryAt); err != nil {
		t.Fatalf("seed retry: %v", err)
	}

	decoratedJobs := &retryClaimCancellingRepo{
		DecisionJobRepository: jobs,
		cancelAfterClaim: func() {
			if err := jobs.Cancel(context.Background(), job.ID); err != nil {
				t.Errorf("remote cancel job: %v", err)
			}
			if err := repo.CaseRepo().UpdateStatus(context.Background(), caseID, entity.CaseStatusCancelled); err != nil {
				t.Errorf("remote cancel case: %v", err)
			}
		},
	}
	orch := &retryResetOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: decoratedJobs, CaseRepo: repo.CaseRepo(), WorkerID: "late-worker", MaxAttempts: 2,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start retry: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for rm.IsRunning(caseID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rm.IsRunning(caseID) {
		t.Fatal("retry worker did not stop after remote cancellation")
	}
	if calls := orch.calls.Load(); calls != 0 {
		t.Fatalf("orchestrator calls = %d, want 0 after reset fence", calls)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusCancelled {
		t.Fatalf("case after retry reset = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil || jobAfter.Status != entity.DecisionJobCancelled {
		t.Fatalf("job after retry reset = %+v err=%v", jobAfter, err)
	}
}

func TestRunManager_RetryResetsFailedCaseWithConditionalWriter(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-retry-from-failed"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	orch := &failThenSucceedCaseOrchestrator{caseRepo: repo.CaseRepo()}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "retry-failed", MaxAttempts: 2, RetryBase: time.Millisecond,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, caseID, entity.DecisionJobSucceeded)
	if job.Attempt != 2 || orch.calls.Load() != 2 {
		t.Fatalf("retry result: job=%+v calls=%d", job, orch.calls.Load())
	}
}

func TestRunManager_RetryResetDoesNotReviveRemotePausedCase(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-retry-reset-paused"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	job, err := jobs.Enqueue(context.Background(), caseID, 2)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, ok, err := jobs.Claim(context.Background(), job.ID, "previous-worker", time.Now().Add(time.Minute)); err != nil || !ok {
		t.Fatalf("seed claim: ok=%v err=%v", ok, err)
	}
	retryAt := time.Now().Add(-time.Millisecond)
	if err := jobs.MarkFailed(context.Background(), job.ID, "previous-worker", "retry", &retryAt); err != nil {
		t.Fatalf("seed retry: %v", err)
	}
	decoratedJobs := &retryClaimCancellingRepo{
		DecisionJobRepository: jobs,
		cancelAfterClaim: func() {
			if err := jobs.MarkPaused(context.Background(), job.ID); err != nil {
				t.Errorf("remote pause job: %v", err)
			}
			if err := repo.CaseRepo().UpdateStatus(context.Background(), caseID, entity.CaseStatusPaused); err != nil {
				t.Errorf("remote pause case: %v", err)
			}
		},
	}
	orch := &retryResetOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: decoratedJobs, CaseRepo: repo.CaseRepo(), WorkerID: "late-paused-worker", MaxAttempts: 2,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start retry: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for rm.IsRunning(caseID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rm.IsRunning(caseID) {
		t.Fatal("retry worker did not stop after remote pause")
	}
	if calls := orch.calls.Load(); calls != 0 {
		t.Fatalf("orchestrator calls = %d, want 0 after pause fence", calls)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.Status != entity.CaseStatusPaused {
		t.Fatalf("case after retry reset = %+v err=%v", caseAfter, err)
	}
	jobAfter, err := jobs.GetByCase(context.Background(), caseID)
	if err != nil || jobAfter.Status != entity.DecisionJobPaused {
		t.Fatalf("job after retry reset = %+v err=%v", jobAfter, err)
	}
}

func TestRunManager_DBLimitAcrossInstances(t *testing.T) {
	db := openMultiDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 1, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	orch := &blockingUserOrchestrator{started: make(chan struct{}), release: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-a", MaxAttempts: 1, RetryBase: time.Millisecond,
		MaxConcurrentRunsPerUser: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 1}); err != nil {
		t.Fatalf("first start: %v", err)
	}
	<-orch.started
	count, err := jobs.CountActiveByUser(context.Background(), 1)
	if err != nil || count != 1 {
		t.Fatalf("active count: %d err=%v", count, err)
	}
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "c2", UserID: 1, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case 2: %v", err)
	}
	// A second replica sees the same shared state and must reject.
	rm2 := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", MaxAttempts: 1, RetryBase: time.Millisecond,
		MaxConcurrentRunsPerUser: 1,
	})
	if err := rm2.Start(context.Background(), &entity.DecisionCase{ID: "c2", UserID: 1}); err == nil || err.Error() != decision.ErrRateLimited.Error() {
		t.Fatalf("second instance start: %v", err)
	}
	close(orch.release)
	rm.Cancel("c1")
}
