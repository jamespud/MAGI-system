package decision

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jamespud/magi/backend/application/metrics"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

// ErrAlreadyRunning is returned when Start is called for a case that already
// has an active (non-completed) run.
var ErrAlreadyRunning = errors.New("case already running")

// ErrAlreadyCompleted prevents silently executing the same durable case twice.
var ErrAlreadyCompleted = errors.New("case already completed")

// ErrRateLimited is returned when the user exceeds the configured concurrent run limit.
var ErrRateLimited = errors.New("rate limit exceeded")

// ErrBudgetExceeded is returned when the user has exhausted a configured
// per-user token or cost budget.
var ErrBudgetExceeded = errors.New("budget exceeded")

// ErrJobTerminated means the durable job already reached an authority-terminal
// state (failed/cancelled/paused) that a fresh start must not override.
var ErrJobTerminated = errors.New("case job is terminated")

// BudgetExceededInfo describes which budget dimension blocked the run.
type BudgetExceededInfo struct {
	TokensExceeded bool
	CostExceeded   bool
}

// BudgetChecker reports whether a user may start a new run given their
// cumulative token/cost usage. Implementations are supplied by the container
// (admin usage + config); a nil checker means budgets are not configured.
type BudgetChecker interface {
	CheckBudget(ctx context.Context, userID int64) (*BudgetExceededInfo, error)
}

// settlementTimeout bounds every detached durable write. Settlements must not
// be aborted by the originating request/attempt cancellation, but neither may
// they block a worker forever if the store stalls.
const settlementTimeout = 10 * time.Second

// detachedContext returns a context that ignores parent cancellation (so a
// request or attempt cancel cannot abort a durable settlement) but still has a
// deadline (so a stuck DB call cannot block the worker indefinitely).
func detachedContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), settlementTimeout)
}

type runHandle struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type RunManagerDeps struct {
	JobRepo                  port.DecisionJobRepository
	CaseRepo                 port.CaseRepository
	WorkerID                 string
	LeaseDuration            time.Duration
	MaxAttempts              int
	RetryBase                time.Duration
	RecoveryInterval         time.Duration
	Metrics                  *metrics.Registry
	Cleaner                  port.ArtifactCleaner
	MaxConcurrentRunsPerUser int
	BudgetChecker            BudgetChecker
	LiveEvents               port.LiveEventPublisher
}

// RunManager owns async case execution. With a JobRepo it uses a durable
// envelope and leases; without one it keeps the lightweight in-memory behavior
// used by unit tests and local callers.
type RunManager struct {
	orch                 Orchestrator
	jobRepo              port.DecisionJobRepository
	caseRepo             port.CaseRepository
	workerID             string
	lease                time.Duration
	maxAttempts          int
	retryBase            time.Duration
	metrics              *metrics.Registry
	cleaner              port.ArtifactCleaner
	maxConcurrentPerUser int
	recoveryInterval     time.Duration
	budgetChecker        BudgetChecker
	liveEvents           port.LiveEventPublisher
	userRuns             map[int64]int
	mu                   sync.Mutex
	runs                 map[string]*runHandle
	paused               map[string]bool
}

func NewRunManager(orch Orchestrator, deps ...RunManagerDeps) *RunManager {
	var d RunManagerDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	if d.WorkerID == "" {
		d.WorkerID = "worker-" + uuid.NewString()
	}
	if d.LeaseDuration <= 0 {
		d.LeaseDuration = 5 * time.Minute
	}
	if d.MaxAttempts <= 0 {
		d.MaxAttempts = 3
	}
	if d.RetryBase <= 0 {
		d.RetryBase = time.Second
	}
	if d.RecoveryInterval <= 0 {
		d.RecoveryInterval = 2 * time.Second
	}
	return &RunManager{
		orch: orch, jobRepo: d.JobRepo, caseRepo: d.CaseRepo,
		workerID: d.WorkerID, lease: d.LeaseDuration, maxAttempts: d.MaxAttempts,
		retryBase: d.RetryBase, metrics: d.Metrics, maxConcurrentPerUser: d.MaxConcurrentRunsPerUser, cleaner: d.Cleaner,
		recoveryInterval: d.RecoveryInterval,
		budgetChecker:    d.BudgetChecker,
		liveEvents:       d.LiveEvents,
		userRuns:         make(map[int64]int), runs: make(map[string]*runHandle),
		paused: make(map[string]bool),
	}
}

func (d RunManagerDeps) OrchOrNil(orch Orchestrator) Orchestrator {
	return orch
}

func userIDString(userID int64) string {
	if userID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", userID)
}

// Start persists and launches a case job. The originating HTTP context is
// deliberately not used for execution, so request cancellation cannot kill a
// submitted decision.
func (m *RunManager) Start(ctx context.Context, c *entity.DecisionCase) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("run manager: case is required")
	}
	m.mu.Lock()
	_, local := m.runs[c.ID]
	m.mu.Unlock()
	if local {
		return ErrAlreadyRunning
	}
	if m.budgetChecker != nil && c.UserID != 0 {
		info, err := m.budgetChecker.CheckBudget(ctx, c.UserID)
		if err != nil {
			return fmt.Errorf("run manager: budget check: %w", err)
		}
		if info != nil && (info.TokensExceeded || info.CostExceeded) {
			return fmt.Errorf("%w: %s", ErrBudgetExceeded, budgetDetail(info))
		}
	}
	var job *entity.DecisionJob
	if m.jobRepo != nil {
		var admitted bool
		var err error
		job, admitted, err = m.jobRepo.Admit(ctx, c.ID, m.maxAttempts, m.maxConcurrentPerUser)
		if err != nil {
			return fmt.Errorf("run manager: admit: %w", err)
		}
		if !admitted {
			return ErrRateLimited
		}
		switch job.Status {
		case entity.DecisionJobSucceeded:
			return ErrAlreadyCompleted
		case entity.DecisionJobRunning:
			return ErrAlreadyRunning
		}
	} else if m.maxConcurrentPerUser > 0 && c.UserID != 0 {
		// No durable job repo: preserve the in-memory per-user limit used by
		// unit tests and lightweight local callers.
		m.mu.Lock()
		active := m.userRuns[c.UserID]
		m.mu.Unlock()
		if active >= m.maxConcurrentPerUser {
			return ErrRateLimited
		}
	}
	if !m.launch(c, job) {
		return ErrAlreadyRunning
	}
	return nil
}

// EnsureStarted starts c idempotently. It returns nil when the case already has
// a queued/running/succeeded durable job or the case is already a valid
// terminal result; it never resets a completed case or enqueues a second job.
// A fresh start still runs the authority budget/concurrency checks.
func (m *RunManager) EnsureStarted(ctx context.Context, c *entity.DecisionCase) error {
	if c == nil || c.ID == "" {
		return fmt.Errorf("run manager: case is required")
	}
	if isTerminalCaseStatus(c.Status) {
		return nil
	}
	if m.jobRepo != nil {
		if err := m.jobRepo.RequeueExpired(ctx, time.Now()); err != nil {
			return fmt.Errorf("run manager: recover expired job: %w", err)
		}
		if existing, err := m.jobRepo.GetByCase(ctx, c.ID); err == nil && existing != nil {
			switch existing.Status {
			case entity.DecisionJobQueued, entity.DecisionJobRunning, entity.DecisionJobSucceeded:
				return nil
			case entity.DecisionJobFailed, entity.DecisionJobCancelled, entity.DecisionJobPaused:
				return fmt.Errorf("%w: %s", ErrJobTerminated, existing.Status)
			}
		}
	}
	return m.Start(ctx, c)
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

func budgetDetail(info *BudgetExceededInfo) string {
	switch {
	case info == nil:
		return "unknown"
	case info.TokensExceeded && info.CostExceeded:
		return "token and cost limits"
	case info.TokensExceeded:
		return "token limit"
	default:
		return "cost limit"
	}
}

func (m *RunManager) launch(c *entity.DecisionCase, job *entity.DecisionJob) bool {
	runCtx, cancel := context.WithCancel(context.Background())
	h := &runHandle{cancel: cancel, done: make(chan struct{})}
	m.mu.Lock()
	if _, exists := m.runs[c.ID]; exists {
		m.mu.Unlock()
		cancel()
		return false
	}
	m.runs[c.ID] = h
	if m.jobRepo == nil && c.UserID != 0 {
		m.userRuns[c.UserID]++
	}
	m.mu.Unlock()

	go func() {
		defer close(h.done)
		defer func() {
			m.mu.Lock()
			delete(m.runs, c.ID)
			m.mu.Unlock()
		}()
		m.execute(runCtx, c, job)
	}()
	return true
}

func (m *RunManager) execute(ctx context.Context, c *entity.DecisionCase, job *entity.DecisionJob) {
	defer func() {
		m.mu.Lock()
		if m.jobRepo == nil && c.UserID != 0 && m.userRuns[c.UserID] > 0 {
			m.userRuns[c.UserID]--
		}
		m.mu.Unlock()
	}()
	if m.jobRepo == nil {
		runStart := time.Now()
		m.metrics.RunStart()
		m.metrics.RunStartForUser(userIDString(c.UserID))
		_, err := m.orch.Orchestrate(ctx, c)
		m.metrics.RunFinish(err == nil)
		m.metrics.RunFinishForUser(userIDString(c.UserID))
		m.metrics.RecordRunDuration(time.Since(runStart).Milliseconds())
		return
	}
	if job == nil {
		return
	}
	for {
		if ctx.Err() != nil {
			if job != nil {
				m.cancelWorkerJob(c.ID, job.ID)
			}
			return
		}
		leaseUntil := time.Now().Add(m.lease)
		claimed, ok, err := m.jobRepo.Claim(ctx, job.ID, m.workerID, leaseUntil)
		if err != nil || !ok {
			return
		}
		c.ExecutionAttempt = claimed.Attempt
		// A remote replica may have committed an authoritative terminal
		// transition between the case load that launched this worker and this
		// claim. Re-read the case so the terminal settlement below never acts
		// on a stale status; when the case can no longer be read, release the
		// claim and stop instead of running a retry against an unknown state.
		if m.caseRepo != nil {
			fresh, err := m.caseRepo.Get(ctx, c.ID)
			if err != nil {
				m.releaseRejectedRetryClaim(claimed)
				return
			}
			c = fresh
			// ExecutionAttempt is runtime-only and not part of CaseModel; the
			// authoritative re-read above must not lose the claim's attempt.
			c.ExecutionAttempt = claimed.Attempt
		}
		// A Case that already reached an authoritative terminal state (for
		// example a replica committed DEADLOCKED while this worker was down)
		// must settle the durable Job without retry-reset or orchestration.
		if m.settleClaimedCaseIfTerminal(c, claimed) {
			return
		}
		if claimed.Attempt > 1 && c.Status != entity.CaseStatusResolved {
			if !m.resetCaseForRetry(ctx, c) {
				m.releaseRejectedRetryClaim(claimed)
				return
			}
			if m.cleaner != nil {
				// Cleanup is a correctness barrier for the retry: a partial
				// cleanup would leave attempt N-1 artifacts (votes, evidence,
				// claims) mixed with attempt N, so a failure aborts the retry
				// rather than being silently ignored.
				cleanupCtx, cancelCleanup := detachedContext(ctx)
				cleanupErr := m.cleaner.CleanupCaseArtifacts(cleanupCtx, c.ID)
				cancelCleanup()
				if cleanupErr != nil {
					log.Printf("run manager: retry cleanup for case %s failed, aborting retry: %v", c.ID, cleanupErr)
					_ = m.markJobFailed(claimed.ID, "retry cleanup failed", nil)
					return
				}
			}
		}
		attemptCtx, attemptCancel := context.WithCancelCause(ctx)
		stopHeartbeat := m.startHeartbeat(attemptCtx, attemptCancel, claimed.ID, leaseUntil)
		runStart := time.Now()
		m.metrics.RunStart()
		m.metrics.RunStartForUser(userIDString(c.UserID))
		_, runErr := m.orch.Orchestrate(attemptCtx, c)
		stopHeartbeat()
		finishMetrics := func(ok bool) {
			m.metrics.RunFinish(ok)
			m.metrics.RunFinishForUser(userIDString(c.UserID))
			m.metrics.RecordRunDuration(time.Since(runStart).Milliseconds())
		}

		if runErr == nil {
			if errors.Is(context.Cause(attemptCtx), port.ErrLeaseLost) {
				finishMetrics(false)
				return
			}
			if err := m.markJobSucceeded(claimed.ID); err != nil {
				attemptCancel(port.ErrLeaseLost)
				finishMetrics(false)
			} else {
				finishMetrics(true)
			}
			return
		}
		finishMetrics(false)
		if errors.Is(runErr, port.ErrLeaseLost) || errors.Is(context.Cause(attemptCtx), port.ErrLeaseLost) {
			return
		}
		if ctx.Err() != nil {
			m.cancelWorkerJob(c.ID, claimed.ID)
			return
		}
		if claimed.Attempt < claimed.MaxAttempts {
			retryAt := time.Now().Add(m.retryDelay(claimed.Attempt))
			if err := m.markJobFailed(claimed.ID, runErr.Error(), &retryAt); err != nil {
				if errors.Is(err, port.ErrLeaseLost) {
					attemptCancel(port.ErrLeaseLost)
				}
				return
			}
			timer := time.NewTimer(time.Until(retryAt))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				m.cancelWorkerJob(c.ID, claimed.ID)
				return
			case <-timer.C:
			}
			continue
		}
		event := entity.NewEvent(c.ID, "", nil, entity.EventCaseFailed, map[string]any{
			"status": string(entity.CaseStatusFailed),
		})
		statuses := []entity.CaseStatus{c.Status}
		if c.Status == entity.CaseStatusDraft {
			statuses = append(statuses, "")
		}
		if isTerminalCaseStatus(c.Status) {
			m.settleTerminalCaseJob(claimed, c, runErr.Error(), attemptCancel)
			return
		}
		committed, err := m.commitFinalFailure(claimed.ID, c.ID, statuses, runErr.Error(), &event)
		if err != nil {
			return
		}
		if !committed {
			attemptCancel(port.ErrLeaseLost)
			return
		}
		c.Status = entity.CaseStatusFailed
		m.publishLive(event)
		return
	}
}

// The helpers below wrap every durable settlement write in a detached, bounded
// context. They replace context.Background() call sites so a queued settlement
// cannot hang a worker while still surviving caller cancellation.
func (m *RunManager) markJobSucceeded(jobID string) error {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.MarkSucceeded(ctx, jobID, m.workerID)
}

func (m *RunManager) markJobFailed(jobID, reason string, retryAt *time.Time) error {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.MarkFailed(ctx, jobID, m.workerID, reason, retryAt)
}

func (m *RunManager) markJobPaused(jobID string) error {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.MarkPaused(ctx, jobID)
}

func (m *RunManager) cancelJob(jobID string) error {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.Cancel(ctx, jobID)
}

func (m *RunManager) getJobByCase(caseID string) (*entity.DecisionJob, error) {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.GetByCase(ctx, caseID)
}

func (m *RunManager) resumeJob(jobID string) error {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.ResumeQueued(ctx, jobID)
}

func (m *RunManager) commitFinalFailure(jobID, caseID string, statuses []entity.CaseStatus, reason string, event *entity.MagiEvent) (bool, error) {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.jobRepo.CommitFinalFailure(ctx, jobID, m.workerID, caseID, statuses, reason, event)
}

func (m *RunManager) getCaseByID(caseID string) (*entity.DecisionCase, error) {
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	return m.caseRepo.Get(ctx, caseID)
}

func (m *RunManager) publishLive(event entity.MagiEvent) {
	if m.liveEvents == nil {
		return
	}
	ctx, cancel := detachedContext(context.Background())
	defer cancel()
	_ = m.liveEvents.PublishLive(ctx, event)
}

// settleTerminalCaseJob closes a still-running owner claim when the Case has
// already reached an authoritative terminal state. This can happen when a
// remote replica wins the Case transition while this worker is executing.
func (m *RunManager) settleTerminalCaseJob(job *entity.DecisionJob, c *entity.DecisionCase, lastError string, onLeaseLost func(error)) {
	var err error
	switch c.Status {
	case entity.CaseStatusFailed, entity.CaseStatusCancelled, entity.CaseStatusTimedOut:
		err = m.markJobFailed(job.ID, lastError, nil)
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		err = m.markJobSucceeded(job.ID)
	default:
		onLeaseLost(port.ErrLeaseLost)
		return
	}
	if errors.Is(err, port.ErrLeaseLost) {
		onLeaseLost(port.ErrLeaseLost)
	}
}

// settleClaimedCaseIfTerminal settles a Job that a worker just claimed when the
// Case has already reached an authoritative terminal state. It returns true so
// the caller stops without retry-reset or orchestration. CANCELLED uses the
// job cancellation path; every other terminal uses the shared settlement.
func (m *RunManager) settleClaimedCaseIfTerminal(c *entity.DecisionCase, claimed *entity.DecisionJob) bool {
	switch c.Status {
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked,
		entity.CaseStatusFailed, entity.CaseStatusTimedOut:
		// The job is currently running under this worker fence, so the shared
		// settlement can close it.
		m.settleTerminalCaseJob(claimed, c, "terminal case settlement", func(error) {})
		return true
	case entity.CaseStatusCancelled:
		if err := m.cancelJob(claimed.ID); err != nil && !errors.Is(err, port.ErrLeaseLost) {
			log.Printf("run manager: settle cancelled job %s: %v", claimed.ID, err)
		}
		return true
	default:
		return false
	}
}

func (m *RunManager) releaseRejectedRetryClaim(job *entity.DecisionJob) {
	if job == nil {
		return
	}
	err := m.markJobFailed(job.ID, "retry reset fenced", nil)
	if err != nil && !errors.Is(err, port.ErrLeaseLost) {
		_ = m.cancelJob(job.ID)
	}
}

func (m *RunManager) resetCaseForRetry(ctx context.Context, c *entity.DecisionCase) bool {
	if m.caseRepo == nil {
		c.Status = entity.CaseStatusDraft
		return true
	}
	if writer, ok := m.caseRepo.(port.ConditionalCaseStatusWriter); ok {
		updated, err := writer.UpdateStatusIfCurrent(ctx, c.ID, retryableCaseStatuses(), entity.CaseStatusDraft)
		if err != nil || !updated {
			return false
		}
	} else if err := m.caseRepo.UpdateStatus(ctx, c.ID, entity.CaseStatusDraft); err != nil {
		return false
	}
	c.Status = entity.CaseStatusDraft
	return true
}

func retryableCaseStatuses() []entity.CaseStatus {
	return []entity.CaseStatus{
		"",
		entity.CaseStatusDraft,
		entity.CaseStatusNormalizing,
		entity.CaseStatusContextBuilding,
		entity.CaseStatusRetrievingMemory,
		entity.CaseStatusInvestigating,
		entity.CaseStatusEvidenceGating,
		entity.CaseStatusCollectingVotes,
		entity.CaseStatusConsensusCheck,
		entity.CaseStatusResolving,
		entity.CaseStatusGeneratingReport,
		entity.CaseStatusSavingMemory,
		entity.CaseStatusEvaluating,
		entity.CaseStatusDebating,
		entity.CaseStatusReflecting,
		entity.CaseStatusRevoting,
		entity.CaseStatusMemoryIndexed,
		entity.CaseStatusInsufficientEv,
	}
}

func (m *RunManager) retryDelay(attempt int) time.Duration {
	delay := m.retryBase
	for i := 1; i < attempt; i++ {
		if delay >= time.Minute/2 {
			return time.Minute
		}
		delay *= 2
	}
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

type heartbeatRequest struct {
	leaseUntil     time.Time
	nextLeaseUntil time.Time
}

type heartbeatResult struct {
	nextLeaseUntil time.Time
	err            error
}

func (m *RunManager) startHeartbeat(ctx context.Context, attemptCancel context.CancelCauseFunc, jobID string, leaseUntil time.Time) func() {
	interval := m.lease / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	workerCtx, workerCancel := context.WithCancel(ctx)
	requests := make(chan heartbeatRequest)
	results := make(chan heartbeatResult, 1)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		for {
			select {
			case request := <-requests:
				callCtx, callCancel := context.WithDeadline(workerCtx, request.leaseUntil)
				err := m.jobRepo.Heartbeat(callCtx, jobID, m.workerID, request.nextLeaseUntil)
				callCancel()
				select {
				case results <- heartbeatResult{nextLeaseUntil: request.nextLeaseUntil, err: err}:
				case <-workerCtx.Done():
					return
				}
			case <-workerCtx.Done():
				return
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(done)
		defer workerCancel()
		// Heartbeat implementations must honor ctx. Go cannot forcibly kill a
		// blocked call, but this worker bounds an uncooperative repository to one
		// in-flight call per attempt rather than one goroutine per ticker tick.
		leaseTimer := time.NewTimer(time.Until(leaseUntil))
		defer leaseTimer.Stop()
		resetLeaseTimer := func(next time.Time) {
			if !leaseTimer.Stop() {
				select {
				case <-leaseTimer.C:
				default:
				}
			}
			leaseTimer.Reset(time.Until(next))
		}
		inFlight := false
		for {
			select {
			case <-leaseTimer.C:
				attemptCancel(port.ErrLeaseLost)
				return
			case <-ticker.C:
				if inFlight {
					continue
				}
				request := heartbeatRequest{leaseUntil: leaseUntil, nextLeaseUntil: time.Now().Add(m.lease)}
				select {
				case requests <- request:
					inFlight = true
				case <-stop:
					return
				case <-ctx.Done():
					return
				default:
					// The single I/O worker is still starting; a later ticker will
					// submit the heartbeat while the lease timer remains authoritative.
				}
			case result := <-results:
				inFlight = false
				if result.err == nil && time.Now().Before(leaseUntil) {
					leaseUntil = result.nextLeaseUntil
					resetLeaseTimer(leaseUntil)
					continue
				}
				if errors.Is(result.err, port.ErrLeaseLost) || !time.Now().Before(leaseUntil) {
					attemptCancel(port.ErrLeaseLost)
					return
				}
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

// Recover is the startup compatibility entry: it performs one sweep and stops.
func (m *RunManager) Recover(ctx context.Context) error {
	return m.RecoverOnce(ctx)
}

// RecoverOnce performs a single recovery sweep: requeue expired leases and
// relaunch runnable work. Per-job Case load failures (e.g. a deleted Case) are
// logged and skipped so one bad row cannot stall the rest of the sweep.
func (m *RunManager) RecoverOnce(ctx context.Context) error {
	if m.jobRepo == nil {
		return nil
	}
	if err := m.jobRepo.RequeueExpired(ctx, time.Now()); err != nil {
		return fmt.Errorf("run manager: requeue expired: %w", err)
	}
	jobs, err := m.jobRepo.ListRunnable(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("run manager: list runnable jobs: %w", err)
	}
	if m.caseRepo == nil {
		return fmt.Errorf("run manager: case repository is required for recovery")
	}
	for _, job := range jobs {
		if job == nil {
			continue
		}
		c, err := m.caseRepo.Get(ctx, job.CaseID)
		if err != nil {
			log.Printf("run manager: recovery: load case %s: %v", job.CaseID, err)
			continue
		}
		if c == nil {
			log.Printf("run manager: recovery: case %s is missing; skipping job %s", job.CaseID, job.ID)
			continue
		}
		m.launch(c, job)
	}
	return nil
}

// RunRecovery continuously replays recovery sweeps until ctx is canceled. It
// runs an immediate sweep, then every RecoveryInterval. A repository-wide
// failure is logged and retried on the next tick rather than exiting forever.
func (m *RunManager) RunRecovery(ctx context.Context) error {
	if m.jobRepo == nil {
		return nil
	}
	for {
		if err := m.RecoverOnce(ctx); err != nil {
			log.Printf("run manager: recovery sweep: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.recoveryInterval):
		}
	}
}

// CancelLocal only signals an in-process worker. Distributed callers that
// already committed their own transaction use this to avoid a second status
// mutation; the legacy Cancel method retains the API-v1 durable behavior.
func (m *RunManager) CancelLocal(caseID string) bool {
	m.mu.Lock()
	h, local := m.runs[caseID]
	m.mu.Unlock()
	if local {
		h.cancel()
	}
	return local
}

// Cancel cancels a local worker or a durable queued/running job.
func (m *RunManager) Cancel(caseID string) bool {
	local := m.CancelLocal(caseID)
	if m.jobRepo != nil {
		job, err := m.getJobByCase(caseID)
		if err == nil && job != nil &&
			(job.Status == entity.DecisionJobQueued || job.Status == entity.DecisionJobRunning) {
			_ = m.cancelJob(job.ID)
			return true
		}
	}
	return local
}

// WaitStopped blocks until the in-process worker for caseID has fully exited,
// or the timeout elapses. It returns true when no worker is running. Delete
// uses this as a fence: cleanup must not interleave with a final artifact
// write from a worker that was just cancelled.
func (m *RunManager) WaitStopped(caseID string, timeout time.Duration) bool {
	m.mu.Lock()
	h, active := m.runs[caseID]
	m.mu.Unlock()
	if !active {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-h.done:
		return true
	case <-timer.C:
		return false
	}
}

// Pause stops a running case but keeps its durable job parked instead of
// cancelled, so a later Resume can wake it from its checkpoint. Returns true
// when a local worker or durable job was stopped.
func (m *RunManager) Pause(caseID string) bool {
	m.mu.Lock()
	h, local := m.runs[caseID]
	m.paused[caseID] = true
	m.mu.Unlock()
	parked := false
	if m.jobRepo != nil {
		job, err := m.getJobByCase(caseID)
		if err == nil && job != nil &&
			(job.Status == entity.DecisionJobQueued || job.Status == entity.DecisionJobRunning) {
			if m.markJobPaused(job.ID) == nil {
				parked = true
			}
		}
	}
	if local {
		h.cancel()
	}
	return parked || local
}

// Resume wakes a paused durable job back into the runnable set and relaunches
// the worker when the previous one has fully exited. Local-only runs (no
// durable job) cannot be resumed and return false.
func (m *RunManager) Resume(caseID string) bool {
	if m.jobRepo == nil {
		return false
	}
	job, err := m.getJobByCase(caseID)
	if err != nil || job == nil || job.Status != entity.DecisionJobPaused {
		return false
	}
	if err := m.resumeJob(job.ID); err != nil {
		return false
	}
	m.mu.Lock()
	m.paused[caseID] = false
	h, active := m.runs[caseID]
	m.mu.Unlock()
	if active {
		select {
		case <-h.done:
		case <-time.After(time.Second):
		}
		m.mu.Lock()
		_, stillActive := m.runs[caseID]
		m.mu.Unlock()
		if stillActive {
			return true
		}
	}
	if m.caseRepo == nil {
		return true
	}
	c, err := m.getCaseByID(caseID)
	if err != nil || c == nil {
		return true
	}
	m.launch(c, job)
	return true
}

// cancelWorkerJob records the durable job state when a worker stops because
// its context was cancelled: a deliberate pause parks the job, any other
// cancellation (API cancel, shutdown) marks it cancelled.
func (m *RunManager) cancelWorkerJob(caseID, jobID string) {
	m.mu.Lock()
	wasPaused := m.paused[caseID]
	m.mu.Unlock()
	if wasPaused {
		_ = m.markJobPaused(jobID)
		return
	}
	_ = m.cancelJob(jobID)
}

func (m *RunManager) IsRunning(caseID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[caseID]
	return ok
}

// Shutdown cancels every in-process worker and waits a bounded time for them
// to exit. Workers are derived from context.Background() so the container
// lifecycle owns their cancellation: a failed startup after Recover or a
// graceful stop must not leak goroutines.
func (m *RunManager) Shutdown() {
	m.mu.Lock()
	handles := make([]*runHandle, 0, len(m.runs))
	for _, h := range m.runs {
		handles = append(handles, h)
	}
	m.mu.Unlock()
	for _, h := range handles {
		h.cancel()
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, h := range handles {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		select {
		case <-h.done:
		case <-time.After(remaining):
			return
		}
	}
}
