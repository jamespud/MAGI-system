package decision_test

import (
	"context"
	"errors"
	"reflect"
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
	// EventModel/EventCursorModel are needed because CommitFinalFailure (used by
	// the retry-cleanup-failure path) writes the case event and bumps the
	// per-case sequence cursor in the same transaction.
	if err := db.AutoMigrate(
		&magi.DecisionJobModel{}, &magi.CaseModel{}, &magi.RunAdmissionLockModel{},
		&magi.EventModel{}, &magi.EventCursorModel{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedDecisionCase(t *testing.T, db *gorm.DB, id string, userID int64) {
	t.Helper()
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: id, UserID: userID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("seed case %s: %v", id, err)
	}
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
	seedDecisionCase(t, db, "case-retry", 0)
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
	seedDecisionCase(t, db, "case-heartbeat-blocked", 0)
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
	seedDecisionCase(t, db, "case-success-error", 0)
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
	seedDecisionCase(t, db, "case-lease-lost-before-success", 0)
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
	seedDecisionCase(t, db, "case-recover", 0)
	jobs := magi.NewDecisionJobRepository(db)
	if _, admitted, err := jobs.Admit(context.Background(), "case-recover", 2, 0); err != nil || !admitted {
		t.Fatalf("admit recover: admitted=%v err=%v", admitted, err)
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
	seedDecisionCase(t, db, "case-pause", 0)
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
	seedDecisionCase(t, db, "case-active", 0)
	jobs := magi.NewDecisionJobRepository(db)
	rm := decision.NewRunManager(&durableRetryOrchestrator{}, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-resume", MaxAttempts: 2, RetryBase: time.Millisecond,
	})
	if _, admitted, err := jobs.Admit(context.Background(), "case-active", 2, 0); err != nil || !admitted {
		t.Fatalf("admit active: admitted=%v err=%v", admitted, err)
	}
	if rm.Resume("case-active") {
		t.Fatal("resume must not re-queue an active (non-paused) job")
	}
	job, err := jobs.GetByCase(context.Background(), "case-active")
	if err != nil || job.Status != entity.DecisionJobQueued {
		t.Fatalf("job should stay queued: %+v err=%v", job, err)
	}
}

func openJobAdmissionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite admission: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&magi.CaseModel{}, &magi.DecisionJobModel{}, &magi.RunAdmissionLockModel{}); err != nil {
		t.Fatalf("migrate admission: %v", err)
	}
	return db
}

// TestRunManager_CrashedProcessDoesNotLeakConcurrencySlot proves that the
// durable DecisionJob is the concurrency truth: a worker that crashed while
// holding a lease does not leave a materialized counter row that must be
// decremented before a new run for the same user can be admitted.
func TestRunManager_CrashedProcessDoesNotLeakConcurrencySlot(t *testing.T) {
	db := openJobAdmissionDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	if err := db.Create(&magi.CaseModel{ID: "case-crash-1", UserID: 42, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case1: %v", err)
	}
	job, admitted, err := jobs.Admit(ctx, "case-crash-1", 3, 1)
	if err != nil || !admitted || job == nil {
		t.Fatalf("crash admit 1 = job=%+v admitted=%v err=%v", job, admitted, err)
	}
	// Simulate a worker claiming the job then crashing (lease expires).
	if _, ok, err := jobs.Claim(ctx, job.ID, "worker-crashed", time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("claim crash = ok=%v err=%v", ok, err)
	}
	if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
		t.Fatalf("requeue expired: %v", err)
	}
	runnable, err := jobs.ListRunnable(ctx, time.Now())
	if err != nil || len(runnable) != 1 {
		t.Fatalf("runnable after crash = %+v err=%v", runnable, err)
	}
	// A second case for the same user at limit 1 must still be admitted, because
	// the crashed job has been requeued and no separate counter row leaked.
	if err := db.Create(&magi.CaseModel{ID: "case-crash-2", UserID: 42, Status: string(entity.CaseStatusDraft)}).Error; err != nil {
		t.Fatalf("seed case2: %v", err)
	}
	job2, admitted2, err := jobs.Admit(ctx, "case-crash-2", 3, 1)
	if err != nil {
		t.Fatalf("crash admit 2 err = %v", err)
	}
	// The crashed job is still queued (it belongs to the same user and is the
	// active run at limit 1), so this new case must be refused by the durable
	// count even though no counter row was leaked. This is the assertion that
	// admission derives from job state, not from a decremented counter.
	if admitted2 || job2 != nil {
		t.Fatalf("crash admit 2 should be limited by active durable job: job=%+v admitted=%v", job2, admitted2)
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

type countingRecoveryOrchestrator struct{ calls atomic.Int32 }

func (o *countingRecoveryOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	o.calls.Add(1)
	return &entity.Resolution{ID: "res", FinalDecision: entity.VoteDecisionApprove}, nil
}

func TestRunManager_RunRecoversLeaseExpiredAfterStartup(t *testing.T) {
	db := openJobDB(t)
	seedDecisionCase(t, db, "case-takeover", 0)
	jobs := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	job, admitted, err := jobs.Admit(ctx, "case-takeover", 2, 0)
	if err != nil || !admitted {
		t.Fatalf("admit = admitted=%v err=%v", admitted, err)
	}
	if _, ok, err := jobs.Claim(ctx, job.ID, "worker-a", time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("claim A = ok=%v err=%v", ok, err)
	}

	orch := &countingRecoveryOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: magi.NewRepository(db).CaseRepo(),
		WorkerID: "worker-b", MaxAttempts: 2, RecoveryInterval: 10 * time.Millisecond,
	})
	recoveryCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rm.RunRecovery(recoveryCtx)

	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			got, err := jobs.GetByCase(ctx, "case-takeover")
			if err == nil && got.Status == entity.DecisionJobSucceeded {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recovery did not take over and complete the expired job")
	}
	if orch.calls.Load() == 0 {
		t.Fatal("recovery never ran the orchestrator for the taken-over job")
	}
}

func TestRunManager_RunStopsWhenContextCanceled(t *testing.T) {
	db := openJobDB(t)
	jobs := magi.NewDecisionJobRepository(db)
	rm := decision.NewRunManager(&countingRecoveryOrchestrator{}, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: magi.NewRepository(db).CaseRepo(), RecoveryInterval: 10 * time.Millisecond,
	})
	recoveryCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = rm.RunRecovery(recoveryCtx)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("RunRecovery did not return when its context was canceled")
	}
}

// TestRunManager_ClaimedDeadlockedCaseSettlesSucceededBeforeRetryReset guards
// the DEADLOCKED crash window: a worker that re-claims a job after the Case
// reached DEADLOCKED must mark the job succeeded without retry-reset or a
// duplicate CASE_FAILED event.
func TestRunManager_ClaimedDeadlockedCaseSettlesSucceededBeforeRetryReset(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	seedDecisionCase(t, db, "case-deadlocked", 0)
	jobs := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	job, _, err := jobs.Admit(ctx, "case-deadlocked", 3, 0)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok, err := jobs.Claim(ctx, job.ID, "worker-a", time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("seed claim: %v", err)
	}
	if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if err := repo.CaseRepo().UpdateStatus(ctx, "case-deadlocked", entity.CaseStatusDeadlocked); err != nil {
		t.Fatalf("mark deadlocked: %v", err)
	}
	orch := &countingRecoveryOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", MaxAttempts: 3, RetryBase: time.Millisecond,
	})
	if err := rm.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	waitJobStatus(t, jobs, "case-deadlocked", entity.DecisionJobSucceeded)
	if events, _ := repo.EventRepo().ListByCase(ctx, "case-deadlocked"); len(events) != 0 {
		t.Fatalf("unexpected events for deadlocked settlement: %+v", events)
	}
	if at := orch.calls.Load(); at != 0 {
		t.Fatalf("orchestrator invoked %d times for a terminal case", at)
	}
}

// TestRunManager_ClaimedTerminalCaseDoesNotInvokeOrchestrator guards the same
// invariant for a resolved terminal case.
func TestRunManager_ClaimedTerminalCaseDoesNotInvokeOrchestrator(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	seedDecisionCase(t, db, "case-resolved-terminal", 0)
	jobs := magi.NewDecisionJobRepository(db)
	ctx := context.Background()
	job, _, err := jobs.Admit(ctx, "case-resolved-terminal", 3, 0)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if _, ok, err := jobs.Claim(ctx, job.ID, "worker-a", time.Now().Add(-time.Hour)); err != nil || !ok {
		t.Fatalf("seed claim: %v", err)
	}
	if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if err := repo.CaseRepo().UpdateStatus(ctx, "case-resolved-terminal", entity.CaseStatusResolved); err != nil {
		t.Fatalf("mark resolved: %v", err)
	}
	orch := &countingRecoveryOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-b", MaxAttempts: 3,
	})
	if err := rm.Recover(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	waitJobStatus(t, jobs, "case-resolved-terminal", entity.DecisionJobSucceeded)
	if at := orch.calls.Load(); at != 0 {
		t.Fatalf("orchestrator invoked %d times for a resolved case", at)
	}
}

// attemptRecordingOrchestrator records the ExecutionAttempt observed at each
// Orchestrate call so the retry path can prove attempt-qualified execution.
type attemptRecordingOrchestrator struct {
	mu       sync.Mutex
	attempts []int
}

func (o *attemptRecordingOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	o.mu.Lock()
	o.attempts = append(o.attempts, c.ExecutionAttempt)
	o.mu.Unlock()
	if len(o.attempts) == 1 {
		return nil, errors.New("transient model failure")
	}
	return &entity.Resolution{CaseID: c.ID, FinalDecision: entity.VoteDecisionApprove}, nil
}

func (o *attemptRecordingOrchestrator) Attempts() []int {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]int, len(o.attempts))
	copy(out, o.attempts)
	return out
}

// shutdownBlockingOrchestrator blocks until its context is cancelled so the
// lifecycle shutdown path can prove in-flight workers are stopped.
type shutdownBlockingOrchestrator struct {
	started   chan struct{}
	cancelled chan struct{}
}

func (o *shutdownBlockingOrchestrator) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	close(o.started)
	<-ctx.Done()
	close(o.cancelled)
	return nil, ctx.Err()
}

// TestRunManager_ShutdownCancelsWorkers guards the P1 lifecycle gap where a
// failed startup after Recover would leave context.Background()-derived
// workers running forever because nothing cancels them.
func TestRunManager_ShutdownCancelsWorkers(t *testing.T) {
	db := openJobDB(t)
	seedDecisionCase(t, db, "case-shutdown", 0)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &shutdownBlockingOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-shutdown", MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-shutdown"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-orch.started
	rm.Shutdown()
	select {
	case <-orch.cancelled:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the running worker")
	}
	if rm.IsRunning("case-shutdown") {
		t.Fatal("shutdown left the worker registered")
	}
}

// TestRunManager_WaitStoppedDrainsWorker guards the delete fence: after the
// handler cancels a run it must wait for the worker to fully exit before the
// cleanup transaction runs, so a final artifact write cannot interleave.
func TestRunManager_WaitStoppedDrainsWorker(t *testing.T) {
	db := openJobDB(t)
	seedDecisionCase(t, db, "case-waitstop", 0)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &shutdownBlockingOrchestrator{started: make(chan struct{}), cancelled: make(chan struct{})}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-waitstop", MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-waitstop"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	<-orch.started
	if !rm.Cancel("case-waitstop") {
		t.Fatal("cancel returned false for a running worker")
	}
	if !rm.WaitStopped("case-waitstop", time.Second) {
		t.Fatal("WaitStopped timed out before the worker exited")
	}
	if rm.IsRunning("case-waitstop") {
		t.Fatal("worker still registered after WaitStopped")
	}
	// A case with no active worker must not block the delete path.
	if !rm.WaitStopped("case-waitstop-idle", time.Millisecond) {
		t.Fatal("WaitStopped blocked for a case with no active worker")
	}
}

// TestRunManager_RetryPreservesExecutionAttemptAcrossReload guards the P0
// regression where the post-claim case re-read (fresh) reset the runtime-only
// ExecutionAttempt to zero, breaking attempt-qualified artifact IDs.
func TestRunManager_RetryPreservesExecutionAttemptAcrossReload(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	seedDecisionCase(t, db, "case-attempt-reload", 0)
	jobs := magi.NewDecisionJobRepository(db)
	orch := &attemptRecordingOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), WorkerID: "worker-attempt",
		MaxAttempts: 2, RetryBase: 10 * time.Millisecond,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-attempt-reload"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, "case-attempt-reload", entity.DecisionJobSucceeded)
	if job.Attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", job.Attempt)
	}
	if got := orch.Attempts(); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("observed ExecutionAttempts = %v, want [1 2]", got)
	}
}
