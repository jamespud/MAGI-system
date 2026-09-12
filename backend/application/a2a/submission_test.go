package a2aapp_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
	magi "github.com/jamespud/magi/backend/adapter"
	a2aapp "github.com/jamespud/magi/backend/application/a2a"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/application/redact"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openSubmissionDB(t *testing.T) *gorm.DB {
	t.Helper()
	// Use a file-backed SQLite database rather than ":memory:". An in-memory
	// database lives on a single pooled connection, so if that connection is
	// ever discarded (a query canceled mid-flight under -race/load can do this)
	// the schema vanishes and later queries fail with a spurious "no such
	// table". A file survives connection churn, matching production behavior.
	dsn := filepath.Join(t.TempDir(), "submission.db")
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	// Serialize access on one connection so concurrent readers/writers do not
	// trip SQLITE_BUSY.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatal(err)
	}
	return db
}

type blockingOrch struct{ started chan struct{} }

func newBlockingOrch() *blockingOrch { return &blockingOrch{started: make(chan struct{}, 1)} }

func (b *blockingOrch) Orchestrate(ctx context.Context, c *entity.DecisionCase) (*entity.Resolution, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// fakeJobRepo is a deterministic in-memory DecisionJobRepository for
// SubmissionService tests.
type fakeJobRepo struct {
	mu          sync.Mutex
	jobs        map[string]*entity.DecisionJob
	enqueues    int
	enqueueErr  error
	activeCount int
	runningAll  bool
	cases       map[string]*entity.DecisionCase
	events      map[string][]*entity.MagiEvent
	cursors     map[string]uint64
	commitErr   error
	// Optional cross-instance settlement barrier: the first Enqueue signals
	// enqueueStarted and then waits for enqueueRelease before persisting the
	// job, so a second replica can pass its GetByCase while no job exists yet.
	enqueueStarted chan struct{}
	enqueueRelease chan struct{}
}

func newFakeJobRepo() *fakeJobRepo {
	return &fakeJobRepo{jobs: make(map[string]*entity.DecisionJob), cases: make(map[string]*entity.DecisionCase), events: make(map[string][]*entity.MagiEvent), cursors: make(map[string]uint64)}
}

func (f *fakeJobRepo) Admit(ctx context.Context, caseID string, maxAttempts, perUserLimit int) (*entity.DecisionJob, bool, error) {
	if f.enqueueStarted != nil {
		select {
		case <-f.enqueueStarted:
		default:
			close(f.enqueueStarted)
		}
		<-f.enqueueRelease
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enqueues++
	if f.enqueueErr != nil {
		return nil, false, f.enqueueErr
	}
	if perUserLimit > 0 && f.activeCount >= perUserLimit {
		return nil, false, nil
	}
	if f.runningAll {
		job := &entity.DecisionJob{ID: "job-" + caseID, CaseID: caseID, Status: entity.DecisionJobRunning}
		f.jobs[caseID] = job
		f.cases[caseID] = &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}
		return job, true, nil
	}
	if existing, ok := f.jobs[caseID]; ok {
		if _, present := f.cases[caseID]; !present {
			f.cases[caseID] = &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}
		}
		return existing, true, nil
	}
	now := time.Now()
	job := &entity.DecisionJob{ID: "job-" + caseID, CaseID: caseID, Status: entity.DecisionJobQueued,
		MaxAttempts: maxAttempts, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
	f.jobs[caseID] = job
	f.cases[caseID] = &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}
	return job, true, nil
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
func (f *fakeJobRepo) CommitFinalFailure(ctx context.Context, jobID, workerID, caseID string, expected []entity.CaseStatus, lastError string, event *entity.MagiEvent) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.commitErr != nil {
		return false, f.commitErr
	}
	if event == nil {
		return false, errors.New("failure event is required")
	}
	for _, job := range f.jobs {
		if job.ID == jobID && job.Status == entity.DecisionJobRunning && job.WorkerID == workerID {
			case_, ok := f.cases[caseID]
			if !ok || !containsExpectedCaseStatus(case_.Status, expected) || isTerminalCaseStatus(case_.Status) {
				return false, nil
			}
			for _, existing := range f.events[caseID] {
				if existing.ID == event.ID {
					return false, errors.New("duplicate failure event")
				}
			}
			seq := f.cursors[caseID] + 1
			ev := *event
			ev.Seq = seq
			job.Status, job.WorkerID, job.LastError = entity.DecisionJobFailed, "", lastError
			case_.Status = entity.CaseStatusFailed
			f.cursors[caseID] = seq
			f.events[caseID] = append(f.events[caseID], &ev)
			event.Seq = seq
			return true, nil
		}
	}
	return false, nil
}

func containsExpectedCaseStatus(status entity.CaseStatus, expected []entity.CaseStatus) bool {
	for _, candidate := range expected {
		if candidate == status || (candidate == entity.CaseStatusDraft && status == "") {
			return true
		}
	}
	return false
}

func isTerminalCaseStatus(status entity.CaseStatus) bool {
	switch status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusFailed,
		entity.CaseStatusCancelled, entity.CaseStatusTimedOut, entity.CaseStatusInsufficientEv,
		entity.CaseStatusDeadlocked:
		return true
	default:
		return false
	}
}
func (f *fakeJobRepo) Cancel(ctx context.Context, jobID string) error { return nil }
func (f *fakeJobRepo) MarkPaused(ctx context.Context, jobID string) error {
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
	if f.runningAll {
		job, ok := f.jobs[caseID]
		if !ok {
			job = &entity.DecisionJob{ID: "job-" + caseID, CaseID: caseID, Status: entity.DecisionJobRunning}
			f.jobs[caseID] = job
			f.cases[caseID] = &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}
		}
		return job, nil
	}
	job, ok := f.jobs[caseID]
	if !ok {
		return nil, errors.New("record not found")
	}
	return job, nil
}
func (f *fakeJobRepo) CountActiveByUser(ctx context.Context, userID int64) (int, error) {
	return f.activeCount, nil
}

func (f *fakeJobRepo) enqueueCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.enqueues
}

var _ port.DecisionJobRepository = (*fakeJobRepo)(nil)

type fakeBudgetChecker struct{ exceeded bool }

func (f fakeBudgetChecker) CheckBudget(ctx context.Context, userID int64) (*decision.BudgetExceededInfo, error) {
	if f.exceeded {
		return &decision.BudgetExceededInfo{CostExceeded: true}, nil
	}
	return nil, nil
}

func newSubmissionSvc(t *testing.T, db *gorm.DB, jobs *fakeJobRepo, orch *blockingOrch, budget decision.BudgetChecker, maxConcurrent int) (*a2aapp.SubmissionService, a2aapp.SubmissionRepository) {
	t.Helper()
	repo := magi.NewA2ASubmissionRepository(db)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, MaxConcurrentRunsPerUser: maxConcurrent, BudgetChecker: budget,
	})
	// Join the fire-and-forget worker before the harness temp dir is cleaned.
	t.Cleanup(rm.Shutdown)
	parser := a2aapp.NewInputParser(65536, 16)
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	svc := a2aapp.NewSubmissionService(parser, repo, rm, proj, 3)
	return svc, repo
}

type countingSubmissionRepo struct {
	a2aapp.SubmissionRepository
	claimStartCount     int
	settleStartedCount  int
	settleRejectedCount int
}

type countingDecisionJobRepo struct {
	port.DecisionJobRepository
	enqueueCount int
}

func (r *countingDecisionJobRepo) Admit(ctx context.Context, caseID string, maxAttempts, perUserLimit int) (*entity.DecisionJob, bool, error) {
	r.enqueueCount++
	return r.DecisionJobRepository.Admit(ctx, caseID, maxAttempts, perUserLimit)
}

func (r *countingSubmissionRepo) ClaimStart(ctx context.Context, id, token string, leaseUntil time.Time) (*a2aapp.PreparedSubmission, bool, error) {
	r.claimStartCount++
	return r.SubmissionRepository.ClaimStart(ctx, id, token, leaseUntil)
}

func (r *countingSubmissionRepo) SettleStarted(ctx context.Context, id, token string) error {
	r.settleStartedCount++
	return r.SubmissionRepository.SettleStarted(ctx, id, token)
}

func (r *countingSubmissionRepo) SettleRejected(ctx context.Context, id, token, code string) error {
	r.settleRejectedCount++
	return r.SubmissionRepository.SettleRejected(ctx, id, token, code)
}

func submissionReq(messageID string) *a2a.SendMessageRequest {
	return &a2a.SendMessageRequest{Message: &a2a.Message{
		ID: messageID, Role: a2a.MessageRoleUser,
		Parts: a2a.ContentParts{a2a.NewTextPart("Should MAGI expose A2A?")},
	}}
}

func TestSubmissionService_SubmitStartsNewTask(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	svc, repo := newSubmissionSvc(t, db, jobs, orch, fakeBudgetChecker{}, 0)

	task, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.ID == "" {
		t.Fatalf("task = %+v", task)
	}
	sub, err := repo.GetByTask(context.Background(), 7, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding state = %s, want STARTED", sub.State)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want 1", jobs.enqueueCount())
	}
	<-orch.started
	// let the worker observe cancellation so the goroutine exits cleanly
}

func TestSubmissionService_SubmitReplaysEqualHash(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	svc, _ := newSubmissionSvc(t, db, jobs, orch, fakeBudgetChecker{}, 0)

	first, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	<-orch.started
	second, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("replay task id = %s, want %s", second.ID, first.ID)
	}
	var count int64
	if err := db.Model(&magi.A2ASubmissionModel{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("submission count = %d, want 1", count)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want 1 (replay must not enqueue a second job)", jobs.enqueueCount())
	}
}

func TestSubmissionService_SubmitAlreadyCompletedCaseStarts(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{}, 0)

	parsed, err := (a2aapp.NewInputParser(65536, 16)).Parse(submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	// Seed a PREPARED binding whose case is already RESOLVED (crash/restart path).
	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: parsed.RequestHash, TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-1").Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Submit(context.Background(), 7, submissionReq("msg-1")); err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding state = %s, want STARTED for already-completed case", sub.State)
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("completed case enqueued a job: %d", jobs.enqueueCount())
	}
}

func TestSubmissionService_SubmitBudgetRejectionRejects(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{exceeded: true}, 0)

	task, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionRejected || sub.ErrorCode != "budget_exceeded" {
		t.Fatalf("binding = %+v, want REJECTED/budget_exceeded", sub)
	}
	if jobs.enqueueCount() != 0 {
		t.Fatalf("rejected case was enqueued: %d", jobs.enqueueCount())
	}
}

func TestSubmissionService_SubmitRateLimitRejects(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	jobs.activeCount = 1
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{}, 1)

	task, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionRejected || sub.ErrorCode != "rate_limited" {
		t.Fatalf("binding = %+v, want REJECTED/rate_limited", sub)
	}
}

func TestSubmissionService_SubmitTransientStartFailureLeavesClaim(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	jobs.enqueueErr = errors.New("database unavailable")
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{}, 0)

	task, err := svc.Submit(context.Background(), 7, submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, string(task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarting {
		t.Fatalf("binding state = %s, want STARTING lease for transient failure", sub.State)
	}
}

func TestSubmissionService_RecoverStartsPreparedBindings(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{}, 0)

	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding state = %s, want STARTED after recovery", sub.State)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want 1", jobs.enqueueCount())
	}
}

func TestSubmissionService_RecoverLeavesClaimForRecovery(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	jobs.enqueueErr = errors.New("database unavailable")
	svc, repo := newSubmissionSvc(t, db, jobs, newBlockingOrch(), fakeBudgetChecker{}, 0)

	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := repo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := svc.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	sub, err := repo.GetByTask(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarting {
		t.Fatalf("binding state = %s, want STARTING lease when start keeps failing", sub.State)
	}
}

func TestSubmissionService_RecoverSettlesExistingTerminalJobsOnce(t *testing.T) {
	for _, tt := range []struct {
		name      string
		jobStatus entity.DecisionJobStatus
		wantState a2a.TaskState
		wantPause bool
	}{
		{name: "failed", jobStatus: entity.DecisionJobFailed, wantState: a2a.TaskStateFailed},
		{name: "cancelled", jobStatus: entity.DecisionJobCancelled, wantState: a2a.TaskStateCanceled},
		{name: "paused", jobStatus: entity.DecisionJobPaused, wantState: a2a.TaskStateWorking, wantPause: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := openSubmissionDB(t)
			ctx := context.Background()
			caseID := "terminal-recovery-" + tt.name
			baseRepo := magi.NewA2ASubmissionRepository(db)
			if _, _, err := baseRepo.Prepare(ctx, a2aapp.PrepareCommand{
				SubmissionID: "sub-" + caseID, MessageID: "msg-" + caseID, RequestHash: "hash-" + caseID,
				TaskID: caseID, ContextID: "conv-" + caseID, InputMessageID: "input-" + caseID,
				CaseMessageID: "case-msg-" + caseID, UserID: 7, Question: "q", MaxDebateRounds: 3,
			}); err != nil {
				t.Fatal(err)
			}
			if _, claimed, err := baseRepo.ClaimStart(ctx, "sub-"+caseID, "expired-token", time.Now().Add(-time.Minute)); err != nil || !claimed {
				t.Fatalf("expired claim = claimed %v err %v", claimed, err)
			}
			now := time.Now()
			if err := db.Create(&magi.DecisionJobModel{
				ID: "job-" + caseID, CaseID: caseID, Status: string(tt.jobStatus),
				AvailableAt: now, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				t.Fatal(err)
			}
			jobs := &countingDecisionJobRepo{DecisionJobRepository: magi.NewDecisionJobRepository(db)}
			rm := decision.NewRunManager(newBlockingOrch(), decision.RunManagerDeps{JobRepo: jobs})
			t.Cleanup(rm.Shutdown)
			countedRepo := &countingSubmissionRepo{SubmissionRepository: baseRepo}
			proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
			svc := a2aapp.NewSubmissionService(a2aapp.NewInputParser(65536, 16), countedRepo, rm, proj, 3)

			if err := svc.Recover(ctx); err != nil {
				t.Fatalf("first recovery: %v", err)
			}
			sub, err := baseRepo.GetByTask(ctx, 7, caseID)
			if err != nil {
				t.Fatal(err)
			}
			if sub.State != a2aapp.SubmissionStarted {
				t.Fatalf("job %s left binding in %s, want STARTED", tt.jobStatus, sub.State)
			}
			if jobs.enqueueCount != 0 {
				t.Fatalf("existing %s job was enqueued again: %d", tt.jobStatus, jobs.enqueueCount)
			}
			record, err := baseRepo.GetTaskRecord(ctx, 7, caseID)
			if err != nil {
				t.Fatal(err)
			}
			task := proj.Project(record, 1, false)
			if task.Status.State != tt.wantState {
				t.Fatalf("projected state = %s, want %s", task.Status.State, tt.wantState)
			}
			magiMetadata, ok := task.Metadata["magi"].(map[string]any)
			if !ok {
				t.Fatalf("task metadata missing magi object: %#v", task.Metadata)
			}
			if paused, _ := magiMetadata["paused"].(bool); paused != tt.wantPause {
				t.Fatalf("projected paused = %v, want %v", paused, tt.wantPause)
			}

			var jobsBefore int64
			if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", caseID).Count(&jobsBefore).Error; err != nil {
				t.Fatal(err)
			}
			claimsBefore, settlesBefore, enqueuesBefore := countedRepo.claimStartCount, countedRepo.settleStartedCount, jobs.enqueueCount
			if err := svc.Recover(ctx); err != nil {
				t.Fatalf("second recovery: %v", err)
			}
			var jobsAfter int64
			if err := db.Model(&magi.DecisionJobModel{}).Where("case_id = ?", caseID).Count(&jobsAfter).Error; err != nil {
				t.Fatal(err)
			}
			if countedRepo.claimStartCount != claimsBefore || countedRepo.settleStartedCount != settlesBefore {
				t.Fatalf("second recovery changed claim/settle counts: claims %d -> %d, settles %d -> %d", claimsBefore, countedRepo.claimStartCount, settlesBefore, countedRepo.settleStartedCount)
			}
			if jobs.enqueueCount != enqueuesBefore {
				t.Fatalf("second recovery changed enqueue count: %d -> %d", enqueuesBefore, jobs.enqueueCount)
			}
			if jobsAfter != jobsBefore {
				t.Fatalf("second recovery changed job count: %d -> %d", jobsBefore, jobsAfter)
			}
			if countedRepo.settleRejectedCount != 0 {
				t.Fatalf("terminal job was rejected %d times", countedRepo.settleRejectedCount)
			}
		})
	}
}

// TestSubmissionService_ConcurrentRecoverCannotRejectStartedBinding guards the
// cross-instance settlement invariant: when two replicas recover the same
// PREPARED binding and only one can acquire the run slot, the loser's
// rate-limit rejection must never overwrite the winner's STARTED settlement.
func TestSubmissionService_ConcurrentRecoverCannotRejectStartedBinding(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	baseRepo := magi.NewA2ASubmissionRepository(db)
	orch := newBlockingOrch()
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	parser := a2aapp.NewInputParser(65536, 16)

	rmA := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, MaxConcurrentRunsPerUser: 1,
	})
	rmB := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, MaxConcurrentRunsPerUser: 1,
	})
	t.Cleanup(rmA.Shutdown)
	t.Cleanup(rmB.Shutdown)
	svcA := a2aapp.NewSubmissionService(parser, baseRepo, rmA, proj, 3)
	svcB := a2aapp.NewSubmissionService(parser, baseRepo, rmB, proj, 3)

	parsed, err := parser.Parse(submissionReq("msg-1"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: parsed.RequestHash, TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}
	if _, _, err := baseRepo.Prepare(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, svc := range []*a2aapp.SubmissionService{svcA, svcB} {
		wg.Add(1)
		go func(svc *a2aapp.SubmissionService) {
			defer wg.Done()
			errs <- svc.Recover(context.Background())
		}(svc)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	sub, err := baseRepo.GetByTask(context.Background(), 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding = %+v, want STARTED (a started task must never become REJECTED)", sub)
	}
	job, err := jobs.GetByCase(context.Background(), "case-1")
	if err != nil || job.Status != entity.DecisionJobRunning {
		t.Fatalf("job = %+v err=%v, want running", job, err)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want exactly 1", jobs.enqueueCount())
	}
}

// TestSubmissionService_ClaimFencingLosesWrongToken guards the conditional
// settlement: only the claim owner's token may finalize a STARTING binding, so
// a losing replica can never overwrite the winner's settlement.
func TestSubmissionService_ClaimFencingLosesWrongToken(t *testing.T) {
	db := openSubmissionDB(t)
	repo := magi.NewA2ASubmissionRepository(db)
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := repo.ClaimStart(ctx, "sub-1", "winner-token", time.Now().Add(time.Minute)); err != nil || !claimed {
		t.Fatalf("claim = claimed %v err %v", claimed, err)
	}
	// A losing replica's token must not settle the winner's claim.
	if err := repo.SettleRejected(ctx, "sub-1", "loser-token", "rate_limited"); !errors.Is(err, a2aapp.ErrClaimLost) {
		t.Fatalf("loser settle error = %v, want ErrClaimLost", err)
	}
	if err := repo.SettleStarted(ctx, "sub-1", "winner-token"); err != nil {
		t.Fatalf("winner settle: %v", err)
	}
	sub, err := repo.GetByTask(ctx, 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding = %+v, want STARTED", sub)
	}
}

// TestSubmissionService_RunRecoveryReclaimsExpiredClaim guards the continuous
// recovery worker: an expired STARTING lease is claimable again and settles to
// STARTED without manual intervention.
func TestSubmissionService_RunRecoveryReclaimsExpiredClaim(t *testing.T) {
	db := openSubmissionDB(t)
	jobs := newFakeJobRepo()
	orch := newBlockingOrch()
	repo := magi.NewA2ASubmissionRepository(db)
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{JobRepo: jobs})
	t.Cleanup(rm.Shutdown)
	parser := a2aapp.NewInputParser(65536, 16)
	proj := a2aapp.NewTaskProjector(redact.New("sk-secret"))
	svc := a2aapp.NewSubmissionService(parser, repo, rm, proj, 3,
		a2aapp.WithRecoveryInterval(10*time.Millisecond), a2aapp.WithStartTimeout(time.Second))
	ctx := context.Background()
	if _, _, err := repo.Prepare(ctx, a2aapp.PrepareCommand{
		SubmissionID: "sub-1", MessageID: "msg-1", RequestHash: "hash", TaskID: "case-1",
		ContextID: "conv-1", InputMessageID: "input-1", CaseMessageID: "case-msg-1",
		UserID: 7, Question: "q", MaxDebateRounds: 3,
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed claimant whose lease already expired.
	if _, claimed, err := repo.ClaimStart(ctx, "sub-1", "stale-token", time.Now().Add(-time.Minute)); err != nil || !claimed {
		t.Fatalf("stale claim = claimed %v err %v", claimed, err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.RunRecovery(runCtx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitFor(t, 2*time.Second, func() bool {
		sub, err := repo.GetByTask(ctx, 7, "case-1")
		return err == nil && sub.State == a2aapp.SubmissionStarted
	})
	sub, err := repo.GetByTask(ctx, 7, "case-1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.State != a2aapp.SubmissionStarted {
		t.Fatalf("binding = %+v, want STARTED after recovery reclaimed the lease", sub)
	}
	if jobs.enqueueCount() != 1 {
		t.Fatalf("enqueues = %d, want 1", jobs.enqueueCount())
	}
}
