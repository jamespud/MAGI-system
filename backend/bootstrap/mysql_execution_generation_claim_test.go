package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type mysqlClaimOutcome struct {
	job    *entity.DecisionJob
	ok     bool
	err    error
	token  string
	worker string
}

func seedMySQLDecisionJob(t *testing.T, db *gorm.DB, caseID string, maxAttempts int) (port.Repository, port.DecisionJobRepository, *entity.DecisionJob) {
	t.Helper()
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	job, admitted, err := jobs.Admit(context.Background(), caseID, maxAttempts, 0)
	if err != nil || !admitted {
		t.Fatalf("admit: job=%+v admitted=%v err=%v", job, admitted, err)
	}
	return repo, jobs, job
}

func TestMySQLDecisionJobClaim_ConcurrentAllocatesOneGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-claim-concurrent", 3)
	start := make(chan struct{})
	out := make(chan mysqlClaimOutcome, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		worker := fmt.Sprintf("worker-%d", i)
		token := uuid.NewString()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			claimed, ok, err := jobs.Claim(context.Background(), job.ID, worker, token, time.Now().Add(time.Minute))
			out <- mysqlClaimOutcome{job: claimed, ok: ok, err: err, token: token, worker: worker}
		}()
	}
	close(start)
	wg.Wait()
	close(out)

	var winner mysqlClaimOutcome
	wins := 0
	for result := range out {
		if result.err != nil {
			t.Fatalf("concurrent claim error: %v", result.err)
		}
		if result.ok {
			wins++
			winner = result
		}
	}
	if wins != 1 {
		t.Fatalf("successful claims = %d, want 1", wins)
	}
	if winner.job == nil || winner.job.ExecutionGeneration != 1 || winner.job.ClaimToken != winner.token {
		t.Fatalf("winner = %+v token=%s", winner.job, winner.token)
	}

	caseAfter, err := repo.CaseRepo().Get(context.Background(), job.CaseID)
	if err != nil || caseAfter.ExecutionGeneration != 1 {
		t.Fatalf("case generation after concurrent claim = %+v err=%v", caseAfter, err)
	}
	stored, err := jobs.GetByCase(context.Background(), job.CaseID)
	if err != nil || stored.ExecutionGeneration != 1 || stored.Status != entity.DecisionJobRunning || stored.ClaimToken != winner.token {
		t.Fatalf("stored winner = %+v err=%v", stored, err)
	}

	recovered, err := jobs.GetByClaimToken(context.Background(), winner.token)
	if err != nil || recovered.ExecutionGeneration != 1 || recovered.ID != job.ID {
		t.Fatalf("recover by token = %+v err=%v", recovered, err)
	}
	repeated, ok, err := jobs.Claim(context.Background(), job.ID, winner.worker, winner.token, time.Now().Add(time.Minute))
	if err != nil || !ok || repeated.ExecutionGeneration != 1 {
		t.Fatalf("same-token claim = %+v ok=%v err=%v", repeated, ok, err)
	}
	caseAfter, _ = repo.CaseRepo().Get(context.Background(), job.CaseID)
	if caseAfter.ExecutionGeneration != 1 {
		t.Fatalf("same token allocated another generation: %d", caseAfter.ExecutionGeneration)
	}
	if other, ok, err := jobs.Claim(context.Background(), job.ID, "other-worker", uuid.NewString(), time.Now().Add(time.Minute)); err != nil || ok || other != nil {
		t.Fatalf("different token claimed running job: job=%+v ok=%v err=%v", other, ok, err)
	}

	_, otherJobs, otherJob := seedMySQLDecisionJob(t, db, "case-claim-token-conflict", 2)
	if claimed, ok, err := otherJobs.Claim(context.Background(), otherJob.ID, "worker-conflict", winner.token, time.Now().Add(time.Minute)); !errors.Is(err, port.ErrClaimTokenConflict) || ok || claimed != nil {
		t.Fatalf("cross-job token reuse = job=%+v ok=%v err=%v, want ErrClaimTokenConflict", claimed, ok, err)
	}
	otherCase, err := repo.CaseRepo().Get(context.Background(), otherJob.CaseID)
	if err != nil || otherCase.ExecutionGeneration != 0 {
		t.Fatalf("token conflict advanced other case = %+v err=%v", otherCase, err)
	}
}

func TestMySQLDecisionJobClaim_SupersededTokenRemainsIdempotent(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-claim-token-history", 4)
	ctx := context.Background()
	tokenA := uuid.NewString()
	first, ok, err := jobs.Claim(ctx, job.ID, "worker-a", tokenA, time.Now().Add(time.Minute))
	if err != nil || !ok || first.ExecutionGeneration != 1 {
		t.Fatalf("first claim = %+v ok=%v err=%v", first, ok, err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(ctx, job.ID, job.CaseID, "worker-a", first.ExecutionGeneration, "retry-a", &retryAt); err != nil {
		t.Fatalf("requeue gen1: %v", err)
	}

	tokenB := uuid.NewString()
	second, ok, err := jobs.Claim(ctx, job.ID, "worker-b", tokenB, time.Now().Add(time.Minute))
	if err != nil || !ok || second.ExecutionGeneration != 2 {
		t.Fatalf("second claim = %+v ok=%v err=%v", second, ok, err)
	}
	if err := jobs.MarkFailed(ctx, job.ID, job.CaseID, "worker-b", second.ExecutionGeneration, "retry-b", &retryAt); err != nil {
		t.Fatalf("requeue gen2: %v", err)
	}

	replayed, ok, err := jobs.Claim(ctx, job.ID, "worker-a", tokenA, time.Now().Add(time.Minute))
	if err != nil || !ok || replayed.ExecutionGeneration != first.ExecutionGeneration || replayed.ClaimToken != tokenA {
		t.Fatalf("superseded token replay = %+v ok=%v err=%v", replayed, ok, err)
	}
	caseAfter, err := repo.CaseRepo().Get(ctx, job.CaseID)
	if err != nil || caseAfter.ExecutionGeneration != 2 {
		t.Fatalf("superseded token allocated a new generation: case=%+v err=%v", caseAfter, err)
	}
	current, err := jobs.GetByCase(ctx, job.CaseID)
	if err != nil || current.Status != entity.DecisionJobQueued || current.ExecutionGeneration != 2 || current.ClaimToken != tokenB {
		t.Fatalf("superseded token changed current job: job=%+v err=%v", current, err)
	}
	var records int64
	if err := db.Model(&magi.DecisionJobClaimModel{}).Where("job_id = ?", job.ID).Count(&records).Error; err != nil {
		t.Fatalf("count claim records: %v", err)
	}
	if records != 2 {
		t.Fatalf("claim registry rows = %d, want 2", records)
	}
}

func TestMySQLDecisionJobClaim_JobWriteFailureRollsBackGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-claim-rollback", 2)
	if err := db.Exec(`CREATE TRIGGER t2_fail_claim BEFORE UPDATE ON decision_job
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected claim failure'`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	_, ok, err := jobs.Claim(context.Background(), job.ID, "worker-a", uuid.NewString(), time.Now().Add(time.Minute))
	if err == nil || ok {
		t.Fatalf("injected claim = ok=%v err=%v, want rollback error", ok, err)
	}
	caseAfter, getErr := repo.CaseRepo().Get(context.Background(), job.CaseID)
	if getErr != nil || caseAfter.ExecutionGeneration != 0 {
		t.Fatalf("case generation after rollback = %+v err=%v", caseAfter, getErr)
	}
	stored, getErr := jobs.GetByCase(context.Background(), job.CaseID)
	if getErr != nil || stored.Status != entity.DecisionJobQueued || stored.ExecutionGeneration != 0 || stored.ClaimToken != "" {
		t.Fatalf("job after rollback = %+v err=%v", stored, getErr)
	}
}

func TestMySQLDecisionJobClaim_RegistryWriteFailureRollsBackGenerationAndJob(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-claim-registry-rollback", 2)
	if err := db.Exec(`CREATE TRIGGER t2_fail_claim_registry BEFORE INSERT ON decision_job_claim
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'injected claim registry failure'`).Error; err != nil {
		t.Fatalf("create registry failure trigger: %v", err)
	}
	token := uuid.NewString()
	_, ok, err := jobs.Claim(context.Background(), job.ID, "worker-a", token, time.Now().Add(time.Minute))
	if err == nil || ok {
		t.Fatalf("injected registry claim = ok=%v err=%v, want rollback error", ok, err)
	}
	caseAfter, getErr := repo.CaseRepo().Get(context.Background(), job.CaseID)
	if getErr != nil || caseAfter.ExecutionGeneration != 0 {
		t.Fatalf("case generation after registry rollback = %+v err=%v", caseAfter, getErr)
	}
	stored, getErr := jobs.GetByCase(context.Background(), job.CaseID)
	if getErr != nil || stored.Status != entity.DecisionJobQueued || stored.ExecutionGeneration != 0 || stored.ClaimToken != "" {
		t.Fatalf("job after registry rollback = %+v err=%v", stored, getErr)
	}
	if recovered, getErr := jobs.GetByClaimToken(context.Background(), token); !errors.Is(getErr, gorm.ErrRecordNotFound) || recovered != nil {
		t.Fatalf("registry rollback left token mapping: job=%+v err=%v", recovered, getErr)
	}
}

type mysqlReplyLossJobRepo struct {
	port.DecisionJobRepository
	lost           atomic.Bool
	token          atomic.Value
	beforeRecovery func(context.Context, string) error
	recoveryOnce   sync.Once
	recoveryErr    error
}

var errInjectedClaimReplyLoss = errors.New("injected claim reply loss")

func (r *mysqlReplyLossJobRepo) Claim(ctx context.Context, jobID, workerID, claimToken string, leaseUntil time.Time) (*entity.DecisionJob, bool, error) {
	job, ok, err := r.DecisionJobRepository.Claim(ctx, jobID, workerID, claimToken, leaseUntil)
	if err == nil && ok && r.lost.CompareAndSwap(false, true) {
		r.token.Store(claimToken)
		return nil, false, errInjectedClaimReplyLoss
	}
	return job, ok, err
}

func (r *mysqlReplyLossJobRepo) GetByClaimToken(ctx context.Context, claimToken string) (*entity.DecisionJob, error) {
	if r.beforeRecovery != nil {
		r.recoveryOnce.Do(func() {
			r.recoveryErr = r.beforeRecovery(ctx, claimToken)
		})
		if r.recoveryErr != nil {
			return nil, r.recoveryErr
		}
	}
	return r.DecisionJobRepository.GetByClaimToken(ctx, claimToken)
}

type mysqlSuccessOrchestrator struct {
	calls atomic.Int32
}

func (o *mysqlSuccessOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	o.calls.Add(1)
	return &entity.Resolution{FinalDecision: entity.VoteDecisionApprove}, nil
}

func waitMySQLDecisionJobStatus(t *testing.T, jobs port.DecisionJobRepository, caseID string, want entity.DecisionJobStatus) *entity.DecisionJob {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := jobs.GetByCase(context.Background(), caseID)
		if err == nil && job.Status == want {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := jobs.GetByCase(context.Background(), caseID)
	t.Fatalf("job %s did not reach %s: %+v", caseID, want, job)
	return nil
}

func TestMySQLRunManager_ClaimReplyLossRecoversSameGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo := magi.NewRepository(db)
	caseID := "case-claim-reply-loss"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	baseJobs := magi.NewDecisionJobRepository(db)
	lossy := &mysqlReplyLossJobRepo{DecisionJobRepository: baseJobs}
	orch := &mysqlSuccessOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: lossy, CaseRepo: repo.CaseRepo(), WorkerID: "worker-reply-loss", LeaseDuration: time.Minute, MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitMySQLDecisionJobStatus(t, baseJobs, caseID, entity.DecisionJobSucceeded)
	if orch.calls.Load() != 1 {
		t.Fatalf("orchestrator calls = %d, want 1", orch.calls.Load())
	}
	if job.ExecutionGeneration != 1 || job.Attempt != 1 {
		t.Fatalf("reply-loss recovery allocated another claim: %+v", job)
	}
	token, _ := lossy.token.Load().(string)
	if token == "" || job.ClaimToken != token {
		t.Fatalf("recovered token = %q job=%+v", token, job)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.ExecutionGeneration != 1 {
		t.Fatalf("case after reply-loss recovery = %+v err=%v", caseAfter, err)
	}
}

func TestMySQLRunManager_ClaimReplyLossDoesNotResurrectCancelledOwner(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo := magi.NewRepository(db)
	caseID := "case-claim-reply-loss-cancel"
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	baseJobs := magi.NewDecisionJobRepository(db)
	lossy := &mysqlReplyLossJobRepo{DecisionJobRepository: baseJobs}
	lossy.beforeRecovery = func(ctx context.Context, _ string) error {
		job, err := baseJobs.GetByCase(ctx, caseID)
		if err != nil {
			return err
		}
		return baseJobs.Cancel(ctx, job.ID)
	}
	orch := &mysqlSuccessOrchestrator{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: lossy, CaseRepo: repo.CaseRepo(), WorkerID: "worker-reply-loss-cancel", LeaseDuration: time.Minute, MaxAttempts: 1,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: caseID, Status: entity.CaseStatusDraft}); err != nil {
		t.Fatalf("start: %v", err)
	}

	job := waitMySQLDecisionJobStatus(t, baseJobs, caseID, entity.DecisionJobCancelled)
	if !rm.WaitStopped(caseID, time.Second) {
		t.Fatal("worker must stop after recovered claim loses durable ownership")
	}
	if orch.calls.Load() != 0 {
		t.Fatalf("orchestrator calls = %d, want 0 after authority cancel wins", orch.calls.Load())
	}
	if job.ExecutionGeneration != 1 || job.Attempt != 1 {
		t.Fatalf("cancelled ambiguous claim = %+v, want generation 1 attempt 1", job)
	}
	token, _ := lossy.token.Load().(string)
	if token == "" {
		t.Fatal("reply-loss fixture did not retain the caller claim token")
	}
	recovered, err := baseJobs.GetByClaimToken(context.Background(), token)
	if err != nil || recovered.ExecutionGeneration != 1 {
		t.Fatalf("historical token recovery = %+v err=%v", recovered, err)
	}
	if recovered.Status != entity.DecisionJobRunning {
		t.Fatalf("claim registry snapshot status = %s, want historical RUNNING", recovered.Status)
	}
	caseAfter, err := repo.CaseRepo().Get(context.Background(), caseID)
	if err != nil || caseAfter.ExecutionGeneration != 1 {
		t.Fatalf("case after cancelled ambiguous claim = %+v err=%v", caseAfter, err)
	}
}

func TestMySQLDecisionJobOwnership_SameWorkerABAFencedByGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-same-worker-aba", 3)
	ctx := context.Background()
	worker := "worker-same"
	first, ok, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("first claim: job=%+v ok=%v err=%v", first, ok, err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(ctx, job.ID, job.CaseID, worker, first.ExecutionGeneration, "retry", &retryAt); err != nil {
		t.Fatalf("requeue gen1: %v", err)
	}
	second, ok, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || second.ExecutionGeneration != first.ExecutionGeneration+1 {
		t.Fatalf("second claim: job=%+v ok=%v err=%v", second, ok, err)
	}
	if err := jobs.MarkSucceeded(ctx, job.ID, "wrong-case", worker, second.ExecutionGeneration); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("wrong case identity error = %v, want ErrLeaseLost", err)
	}

	stale := first.ExecutionGeneration
	for name, mutate := range map[string]func() error{
		"heartbeat": func() error {
			return jobs.Heartbeat(ctx, job.ID, job.CaseID, worker, stale, time.Now().Add(time.Minute))
		},
		"succeeded": func() error { return jobs.MarkSucceeded(ctx, job.ID, job.CaseID, worker, stale) },
		"failed":    func() error { return jobs.MarkFailed(ctx, job.ID, job.CaseID, worker, stale, "late", nil) },
		"cancel":    func() error { return jobs.CancelOwned(ctx, job.ID, job.CaseID, worker, stale) },
		"pause":     func() error { return jobs.MarkPausedOwned(ctx, job.ID, job.CaseID, worker, stale) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("stale %s error = %v, want ErrLeaseLost", name, err)
			}
		})
	}
	event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseFailed, map[string]any{"reason": "stale"})
	committed, err := jobs.CommitFinalFailure(ctx, job.ID, worker, stale, job.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "late", &event)
	if err != nil || committed {
		t.Fatalf("stale final failure = committed=%v err=%v", committed, err)
	}
	stored, err := jobs.GetByCase(ctx, job.CaseID)
	if err != nil || stored.Status != entity.DecisionJobRunning || stored.ExecutionGeneration != second.ExecutionGeneration {
		t.Fatalf("gen2 after stale mutations = %+v err=%v", stored, err)
	}
	caseAfter, err := repo.CaseRepo().Get(ctx, job.CaseID)
	if err != nil || caseAfter.ExecutionGeneration != second.ExecutionGeneration {
		t.Fatalf("case generation after ABA = %+v err=%v", caseAfter, err)
	}
}

func TestMySQLDecisionJobOwnership_ExpiredLeaseCannotSettleOrRenew(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	_, jobs, job := seedMySQLDecisionJob(t, db, "case-expired-lease", 3)
	ctx := context.Background()
	worker := "worker-expired"
	first, ok, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(-time.Second))
	if err != nil || !ok {
		t.Fatalf("expired fixture claim: job=%+v ok=%v err=%v", first, ok, err)
	}
	gen := first.ExecutionGeneration
	for name, mutate := range map[string]func() error{
		"heartbeat": func() error {
			return jobs.Heartbeat(ctx, job.ID, job.CaseID, worker, gen, time.Now().Add(time.Minute))
		},
		"succeeded": func() error { return jobs.MarkSucceeded(ctx, job.ID, job.CaseID, worker, gen) },
		"failed":    func() error { return jobs.MarkFailed(ctx, job.ID, job.CaseID, worker, gen, "late", nil) },
		"cancel":    func() error { return jobs.CancelOwned(ctx, job.ID, job.CaseID, worker, gen) },
		"pause":     func() error { return jobs.MarkPausedOwned(ctx, job.ID, job.CaseID, worker, gen) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutate(); !errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("expired %s error = %v, want ErrLeaseLost", name, err)
			}
		})
	}
	event := entity.NewEvent(job.CaseID, "", nil, entity.EventCaseFailed, nil)
	committed, err := jobs.CommitFinalFailure(ctx, job.ID, worker, gen, job.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "late", &event)
	if err != nil || committed {
		t.Fatalf("expired final failure = committed=%v err=%v", committed, err)
	}
	if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
		t.Fatalf("requeue expired: %v", err)
	}
	second, ok, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || second.ExecutionGeneration != gen+1 {
		t.Fatalf("post-expiry claim = %+v ok=%v err=%v", second, ok, err)
	}
}

func TestMySQLDecisionJobOwnership_ExternalCancelAndPauseInvalidateOwner(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	ctx := context.Background()

	_, cancelJobs, cancelJob := seedMySQLDecisionJob(t, db, "case-external-cancel", 3)
	cancelled, ok, err := cancelJobs.Claim(ctx, cancelJob.ID, "worker-cancel", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("claim cancel fixture: %+v ok=%v err=%v", cancelled, ok, err)
	}
	if err := cancelJobs.Cancel(ctx, cancelJob.ID); err != nil {
		t.Fatalf("external cancel: %v", err)
	}
	if err := cancelJobs.MarkSucceeded(ctx, cancelJob.ID, cancelJob.CaseID, "worker-cancel", cancelled.ExecutionGeneration); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("late success after cancel = %v, want ErrLeaseLost", err)
	}
	if _, admitted, err := cancelJobs.Admit(ctx, cancelJob.CaseID, 3, 0); err != nil || !admitted {
		t.Fatalf("readmit cancelled job: admitted=%v err=%v", admitted, err)
	}
	cancelNext, ok, err := cancelJobs.Claim(ctx, cancelJob.ID, "worker-cancel", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || cancelNext.ExecutionGeneration != cancelled.ExecutionGeneration+1 {
		t.Fatalf("claim after cancel = %+v ok=%v err=%v", cancelNext, ok, err)
	}

	_, pauseJobs, pauseJob := seedMySQLDecisionJob(t, db, "case-external-pause", 3)
	paused, ok, err := pauseJobs.Claim(ctx, pauseJob.ID, "worker-pause", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("claim pause fixture: %+v ok=%v err=%v", paused, ok, err)
	}
	if err := pauseJobs.MarkPaused(ctx, pauseJob.ID); err != nil {
		t.Fatalf("external pause: %v", err)
	}
	if err := pauseJobs.Heartbeat(ctx, pauseJob.ID, pauseJob.CaseID, "worker-pause", paused.ExecutionGeneration, time.Now().Add(time.Minute)); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("late heartbeat after pause = %v, want ErrLeaseLost", err)
	}
	if err := pauseJobs.ResumeQueued(ctx, pauseJob.ID); err != nil {
		t.Fatalf("resume queued: %v", err)
	}
	pauseNext, ok, err := pauseJobs.Claim(ctx, pauseJob.ID, "worker-pause", uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !ok || pauseNext.ExecutionGeneration != paused.ExecutionGeneration+1 {
		t.Fatalf("claim after pause = %+v ok=%v err=%v", pauseNext, ok, err)
	}
}
