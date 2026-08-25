package decision_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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

// fakeJobRepo is a deterministic in-memory DecisionJobRepository for
// EnsureStarted tests. Enqueue returns the existing queued/running/succeeded
// job or creates a queued one; other statuses are surfaced verbatim.
type fakeJobRepo struct {
	mu          sync.Mutex
	jobs        map[string]*entity.DecisionJob
	enqueues    int
	enqueueErr  error
	activeCount int
}

func newFakeJobRepo() *fakeJobRepo {
	return &fakeJobRepo{jobs: make(map[string]*entity.DecisionJob)}
}

func (f *fakeJobRepo) Enqueue(ctx context.Context, caseID string, maxAttempts int) (*entity.DecisionJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueues++
	if f.enqueueErr != nil {
		return nil, f.enqueueErr
	}
	if existing, ok := f.jobs[caseID]; ok {
		return existing, nil
	}
	now := time.Now()
	job := &entity.DecisionJob{ID: "job-" + caseID, CaseID: caseID, Status: entity.DecisionJobQueued,
		MaxAttempts: maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	f.jobs[caseID] = job
	return job, nil
}

func (f *fakeJobRepo) Claim(ctx context.Context, jobID, workerID string, leaseUntil time.Time) (*entity.DecisionJob, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.jobs {
		if j.ID == jobID && j.Status == entity.DecisionJobQueued {
			j.Status = entity.DecisionJobRunning
			j.WorkerID = workerID
			j.LeaseUntil = &leaseUntil
			j.Attempt++
			return j, true, nil
		}
	}
	return nil, false, nil
}
func (f *fakeJobRepo) Heartbeat(ctx context.Context, jobID, workerID string, leaseUntil time.Time) error {
	return nil
}
func (f *fakeJobRepo) MarkSucceeded(ctx context.Context, jobID, workerID string) error { return nil }
func (f *fakeJobRepo) MarkFailed(ctx context.Context, jobID, workerID, lastError string, retryAt *time.Time) error {
	return nil
}
func (f *fakeJobRepo) Cancel(ctx context.Context, jobID string) error { return nil }
func (f *fakeJobRepo) MarkPaused(ctx context.Context, jobID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.jobs {
		if j.ID == jobID {
			j.Status = entity.DecisionJobPaused
		}
	}
	return nil
}
func (f *fakeJobRepo) ResumeQueued(ctx context.Context, jobID string) error { return nil }
func (f *fakeJobRepo) RequeueExpired(ctx context.Context, now time.Time) error {
	return nil
}
func (f *fakeJobRepo) ListRunnable(ctx context.Context, now time.Time) ([]*entity.DecisionJob, error) {
	return nil, nil
}
func (f *fakeJobRepo) GetByCase(ctx context.Context, caseID string) (*entity.DecisionJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	job, ok := f.jobs[caseID]
	if !ok {
		return nil, gormErrRecordNotFound()
	}
	return job, nil
}
func (f *fakeJobRepo) CountActiveByUser(ctx context.Context, userID int64) (int, error) {
	return f.activeCount, nil
}

func gormErrRecordNotFound() error {
	return errors.New("record not found")
}

func (f *fakeJobRepo) enqueueCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enqueues
}

func TestRunManager_EnsureStarted_FreshStartEnqueuesOnce(t *testing.T) {
	jobs := newFakeJobRepo()
	orch := newBlockingOrchestrator()
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs})
	c := &entity.DecisionCase{ID: "c1", UserID: 7, Status: entity.CaseStatusDraft}
	if err := rm.EnsureStarted(context.Background(), c); err != nil {
		t.Fatalf("ensure started: %v", err)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want 1", jobs.enqueueCount())
	}
	<-orch.started
	rm.Cancel("c1")
}

func TestRunManager_EnsureStarted_IdempotentForRunningJob(t *testing.T) {
	jobs := newFakeJobRepo()
	jobs.jobs["c1"] = &entity.DecisionJob{ID: "job-c1", CaseID: "c1", Status: entity.DecisionJobRunning}
	rm := decision.NewRunManager(newBlockingOrchestrator(), decision.RunManagerDeps{JobRepo: jobs})
	c := &entity.DecisionCase{ID: "c1", Status: entity.CaseStatusDraft}
	if err := rm.EnsureStarted(context.Background(), c); err != nil {
		t.Fatalf("ensure started: %v", err)
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("second job enqueued: %d", jobs.enqueueCount())
	}
}

func TestRunManager_EnsureStarted_CompletedCaseIsNoOp(t *testing.T) {
	jobs := newFakeJobRepo()
	rm := decision.NewRunManager(newBlockingOrchestrator(), decision.RunManagerDeps{JobRepo: jobs})
	for _, status := range []entity.CaseStatus{
		entity.CaseStatusResolved, entity.CaseStatusFailed, entity.CaseStatusCancelled,
		entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked,
	} {
		if err := rm.EnsureStarted(context.Background(), &entity.DecisionCase{ID: "c1", Status: status}); err != nil {
			t.Fatalf("ensure started %s: %v", status, err)
		}
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("terminal case enqueued a job: %d", jobs.enqueueCount())
	}
}

func TestRunManager_EnsureStarted_RejectsTerminatedJob(t *testing.T) {
	jobs := newFakeJobRepo()
	jobs.jobs["c1"] = &entity.DecisionJob{ID: "job-c1", CaseID: "c1", Status: entity.DecisionJobFailed}
	rm := decision.NewRunManager(newBlockingOrchestrator(), decision.RunManagerDeps{JobRepo: jobs})
	err := rm.EnsureStarted(context.Background(), &entity.DecisionCase{ID: "c1", Status: entity.CaseStatusDraft})
	if !errors.Is(err, decision.ErrJobTerminated) {
		t.Fatalf("error = %v, want ErrJobTerminated", err)
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("terminated job was reset: %d", jobs.enqueueCount())
	}
}

func TestRunManager_EnsureStarted_PreservesBudgetChecks(t *testing.T) {
	jobs := newFakeJobRepo()
	rm := decision.NewRunManager(newBlockingOrchestrator(), decision.RunManagerDeps{
		JobRepo: jobs, BudgetChecker: &exceededBudgetChecker{},
	})
	err := rm.EnsureStarted(context.Background(), &entity.DecisionCase{ID: "c1", UserID: 7, Status: entity.CaseStatusDraft})
	if !errors.Is(err, decision.ErrBudgetExceeded) {
		t.Fatalf("error = %v, want ErrBudgetExceeded", err)
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("budget-exceeded case was enqueued: %d", jobs.enqueueCount())
	}
}

type exceededBudgetChecker struct{}

func (exceededBudgetChecker) CheckBudget(ctx context.Context, userID int64) (*decision.BudgetExceededInfo, error) {
	return &decision.BudgetExceededInfo{TokensExceeded: true}, nil
}

var _ port.DecisionJobRepository = (*fakeJobRepo)(nil)

// stubResRepo is a minimal ResolutionRepository for Service tests.
type stubResRepo struct{ res *entity.Resolution }

func (s *stubResRepo) Create(ctx context.Context, r *entity.Resolution) error { return nil }
func (s *stubResRepo) Get(ctx context.Context, caseID string) (*entity.Resolution, error) {
	if s.res != nil && s.res.CaseID == caseID {
		return s.res, nil
	}
	return nil, fmt.Errorf("record not found")
}

func TestService_ReportLoadsResolution(t *testing.T) {
	res := &entity.Resolution{CaseID: "c1", FinalReport: "the final report text"}
	svc := decision.NewService(nil, decision.ServiceConfig{}, decision.WithResolutionRepo(&stubResRepo{res: res}))

	got := svc.Report(context.Background(), "c1")
	if got != "the final report text" {
		t.Fatalf("Report should load resolution.FinalReport, got %q", got)
	}

	// Unknown case -> empty string, no panic.
	if svc.Report(context.Background(), "unknown") != "" {
		t.Fatal("Report for unknown case should be empty string")
	}
}
