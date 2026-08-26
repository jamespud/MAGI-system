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
	"github.com/jamespud/magi/backend/application/metrics"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type durableRetryOrchestrator struct {
	calls int32
}

type terminalCaseErrorOrchestrator struct{}

func (terminalCaseErrorOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	return nil, errors.New("late failure after terminal case state")
}

type blockingHeartbeatRepo struct {
	port.DecisionJobRepository
	started chan struct{}
	release chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (r *blockingHeartbeatRepo) Heartbeat(ctx context.Context, jobID, workerID string, leaseUntil time.Time) error {
	r.once.Do(func() { close(r.started) })
	<-r.release
	close(r.done)
	return nil
}

type markSucceededErrorRepo struct {
	port.DecisionJobRepository
	err         error
	markFaileds atomic.Int32
}

func (r *markSucceededErrorRepo) MarkSucceeded(context.Context, string, string) error { return r.err }

func (r *markSucceededErrorRepo) MarkFailed(ctx context.Context, jobID, workerID, lastError string, retryAt *time.Time) error {
	r.markFaileds.Add(1)
	return r.DecisionJobRepository.MarkFailed(ctx, jobID, workerID, lastError, retryAt)
}

type leaseLossBeforeSuccessRepo struct {
	port.DecisionJobRepository
	markSucceededs atomic.Int32
}

func (r *leaseLossBeforeSuccessRepo) Heartbeat(context.Context, string, string, time.Time) error {
	return port.ErrLeaseLost
}

func (r *leaseLossBeforeSuccessRepo) MarkSucceeded(ctx context.Context, jobID, workerID string) error {
	r.markSucceededs.Add(1)
	return r.DecisionJobRepository.MarkSucceeded(ctx, jobID, workerID)
}

type successThenObserveCancellationOrchestrator struct {
	observed chan struct{}
	release  chan struct{}
}

func (o *successThenObserveCancellationOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	go func() {
		select {
		case <-ctx.Done():
			close(o.observed)
		case <-o.release:
		}
	}()
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

type lateSuccessAfterLeaseLossOrchestrator struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (o *lateSuccessAfterLeaseLossOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	close(o.started)
	<-ctx.Done()
	close(o.cancelled)
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
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
	if err := db.AutoMigrate(&magi.DecisionJobModel{}, &magi.CaseModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestRunManager_TerminalCaseFailureSettlesRunningJob(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	jobs := magi.NewDecisionJobRepository(db)
	caseID := "case-terminal-job-settlement"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusResolved}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	rm := decision.NewRunManager(terminalCaseErrorOrchestrator{}, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "terminal-settler", MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusResolved}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, caseID, entity.DecisionJobSucceeded)
	if job.Status == entity.DecisionJobRunning {
		t.Fatalf("terminal case left job running: %+v", job)
	}
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

func TestRunManager_BlockingHeartbeatCancelsAttemptAtLeaseExpiry(t *testing.T) {
	db := openJobDB(t)
	baseJobs := magi.NewDecisionJobRepository(db)
	jobs := &blockingHeartbeatRepo{
		DecisionJobRepository: baseJobs,
		started:               make(chan struct{}),
		release:               make(chan struct{}),
		done:                  make(chan struct{}),
	}
	defer func() {
		select {
		case <-jobs.release:
		default:
			close(jobs.release)
		}
	}()
	lease := 30 * time.Millisecond
	orch := &remoteCancelOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "heartbeat-blocked", LeaseDuration: lease, MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-heartbeat-blocked"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer rm.Cancel("case-heartbeat-blocked")
	<-orch.started
	<-jobs.started
	select {
	case <-orch.cancelled:
	case <-time.After(5 * lease):
		t.Fatal("blocked heartbeat did not cancel the attempt at lease expiry")
	}
	close(jobs.release)
	select {
	case <-jobs.done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat I/O worker did not exit after release")
	}
}

func TestRunManager_MarkSucceededErrorCancelsAttemptWithoutRetry(t *testing.T) {
	db := openJobDB(t)
	baseJobs := magi.NewDecisionJobRepository(db)
	jobs := &markSucceededErrorRepo{DecisionJobRepository: baseJobs, err: errors.New("storage unavailable")}
	orch := &successThenObserveCancellationOrchestrator{observed: make(chan struct{}), release: make(chan struct{})}
	reg := metrics.New()
	defer close(orch.release)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "success-error", MaxAttempts: 2, Metrics: reg,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-success-error"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	select {
	case <-orch.observed:
	case <-time.After(time.Second):
		t.Fatal("MarkSucceeded error did not cancel the completed attempt context")
	}
	deadline := time.Now().Add(time.Second)
	for rm.IsRunning("case-success-error") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rm.IsRunning("case-success-error") {
		t.Fatal("worker did not finish after MarkSucceeded error")
	}
	if calls := jobs.markFaileds.Load(); calls != 0 {
		t.Fatalf("MarkSucceeded error retried as failure %d times", calls)
	}
	if completed, failed := reg.RunsCompleted.Load(), reg.RunsFailed.Load(); completed != 0 || failed != 1 {
		t.Fatalf("MarkSucceeded error metrics completed=%d failed=%d, want 0/1", completed, failed)
	}
	job, err := baseJobs.GetByCase(context.Background(), "case-success-error")
	if err != nil || job.Status != entity.DecisionJobRunning {
		t.Fatalf("job after MarkSucceeded error = %+v err=%v", job, err)
	}
}

func TestRunManager_LeaseLossBeforeLateSuccessDoesNotMarkSucceeded(t *testing.T) {
	db := openJobDB(t)
	baseJobs := magi.NewDecisionJobRepository(db)
	jobs := &leaseLossBeforeSuccessRepo{DecisionJobRepository: baseJobs}
	lease := 30 * time.Millisecond
	orch := &lateSuccessAfterLeaseLossOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "lease-lost-before-success", LeaseDuration: lease, MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-lease-lost-before-success"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-orch.started
	select {
	case <-orch.cancelled:
	case <-time.After(5 * lease):
		t.Fatal("heartbeat lease loss did not cancel the attempt")
	}
	deadline := time.Now().Add(time.Second)
	for rm.IsRunning("case-lease-lost-before-success") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if rm.IsRunning("case-lease-lost-before-success") {
		t.Fatal("worker did not exit after late success")
	}
	if calls := jobs.markSucceededs.Load(); calls != 0 {
		t.Fatalf("MarkSucceeded calls = %d, want 0 after lease loss", calls)
	}
	job, err := baseJobs.GetByCase(context.Background(), "case-lease-lost-before-success")
	if err != nil || job.Status != entity.DecisionJobRunning {
		t.Fatalf("lease-lost job = %+v err=%v, want old owner not to succeed it", job, err)
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

func TestRunManager_PauseParksAndResumeWakesDurableJob(t *testing.T) {
	db := openJobDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &blockingUserOrchestrator{started: make(chan struct{}), release: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: &stubCaseRepo{case_: &entity.DecisionCase{ID: "case-pause"}},
		WorkerID: "worker-pause", MaxAttempts: 2, RetryBase: time.Millisecond,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-pause"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-orch.started

	if !rm.Pause("case-pause") {
		t.Fatal("pause should stop the running case")
	}
	job := waitJobStatus(t, jobs, "case-pause", entity.DecisionJobPaused)
	if job.Status != entity.DecisionJobPaused {
		t.Fatalf("job status = %s", job.Status)
	}
	close(orch.release)

	if !rm.Resume("case-pause") {
		t.Fatal("resume should re-queue the paused job")
	}
	job = waitJobStatus(t, jobs, "case-pause", entity.DecisionJobSucceeded)
	if job.Status != entity.DecisionJobSucceeded {
		t.Fatalf("resumed job status = %s", job.Status)
	}
}

func TestRunManager_ResumeIgnoresNonPausedJobs(t *testing.T) {
	db := openJobDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	rm := decision.NewRunManager(&durableRetryOrchestrator{}, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-resume", MaxAttempts: 2, RetryBase: time.Millisecond,
	})
	if _, err := jobs.Enqueue(context.Background(), "case-active", 2); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if rm.Resume("case-active") {
		t.Fatal("resume must not re-queue an active (non-paused) job")
	}
	job, err := jobs.GetByCase(context.Background(), "case-active")
	if err != nil || job.Status != entity.DecisionJobQueued {
		t.Fatalf("job should stay queued: %+v err=%v", job, err)
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
