package decision

import (
	"context"
	"errors"
	"fmt"
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

type runHandle struct {
	cancel    context.CancelFunc
	done      chan struct{}
}

type RunManagerDeps struct {
	JobRepo                  port.DecisionJobRepository
	CaseRepo                 port.CaseRepository
	WorkerID                 string
	LeaseDuration            time.Duration
	MaxAttempts              int
	RetryBase                time.Duration
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
	return &RunManager{
		orch: orch, jobRepo: d.JobRepo, caseRepo: d.CaseRepo,
		workerID: d.WorkerID, lease: d.LeaseDuration, maxAttempts: d.MaxAttempts,
		retryBase: d.RetryBase, metrics: d.Metrics, maxConcurrentPerUser: d.MaxConcurrentRunsPerUser, cleaner: d.Cleaner,
		budgetChecker: d.BudgetChecker,
		liveEvents:    d.LiveEvents,
		userRuns:      make(map[int64]int), runs: make(map[string]*runHandle),
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
		if claimed.Attempt > 1 && c.Status != entity.CaseStatusResolved {
			if !m.resetCaseForRetry(ctx, c) {
				m.releaseRejectedRetryClaim(claimed)
				return
			}
			if m.cleaner != nil {
				_ = m.cleaner.CleanupCaseArtifacts(context.Background(), c.ID)
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
			if err := m.jobRepo.MarkSucceeded(context.Background(), claimed.ID, m.workerID); err != nil {
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
			if err := m.jobRepo.MarkFailed(context.Background(), claimed.ID, m.workerID, runErr.Error(), &retryAt); err != nil {
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
		committed, err := m.jobRepo.CommitFinalFailure(context.Background(), claimed.ID, m.workerID, c.ID, statuses, runErr.Error(), &event)
		if err != nil {
			return
		}
		if !committed {
			attemptCancel(port.ErrLeaseLost)
			return
		}
		c.Status = entity.CaseStatusFailed
		if m.liveEvents != nil {
			_ = m.liveEvents.PublishLive(context.Background(), event)
		}
		return
	}
}

// settleTerminalCaseJob closes a still-running owner claim when the Case has
// already reached an authoritative terminal state. This can happen when a
// remote replica wins the Case transition while this worker is executing.
func (m *RunManager) settleTerminalCaseJob(job *entity.DecisionJob, c *entity.DecisionCase, lastError string, onLeaseLost func(error)) {
	var err error
	switch c.Status {
	case entity.CaseStatusFailed, entity.CaseStatusCancelled, entity.CaseStatusTimedOut:
		err = m.jobRepo.MarkFailed(context.Background(), job.ID, m.workerID, lastError, nil)
	case entity.CaseStatusResolved, entity.CaseStatusMemoryIndexed, entity.CaseStatusInsufficientEv, entity.CaseStatusDeadlocked:
		err = m.jobRepo.MarkSucceeded(context.Background(), job.ID, m.workerID)
	default:
		onLeaseLost(port.ErrLeaseLost)
		return
	}
	if errors.Is(err, port.ErrLeaseLost) {
		onLeaseLost(port.ErrLeaseLost)
	}
}

func (m *RunManager) releaseRejectedRetryClaim(job *entity.DecisionJob) {
	if job == nil {
		return
	}
	err := m.jobRepo.MarkFailed(context.Background(), job.ID, m.workerID, "retry reset fenced", nil)
	if err != nil && !errors.Is(err, port.ErrLeaseLost) {
		_ = m.jobRepo.Cancel(context.Background(), job.ID)
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

// Recover requeues expired leases and resumes queued work after process start.
func (m *RunManager) Recover(ctx context.Context) error {
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
		if err != nil || c == nil {
			continue
		}
		m.launch(c, job)
	}
	return nil
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
		job, err := m.jobRepo.GetByCase(context.Background(), caseID)
		if err == nil && job != nil &&
			(job.Status == entity.DecisionJobQueued || job.Status == entity.DecisionJobRunning) {
			_ = m.jobRepo.Cancel(context.Background(), job.ID)
			return true
		}
	}
	return local
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
		job, err := m.jobRepo.GetByCase(context.Background(), caseID)
		if err == nil && job != nil &&
			(job.Status == entity.DecisionJobQueued || job.Status == entity.DecisionJobRunning) {
			if m.jobRepo.MarkPaused(context.Background(), job.ID) == nil {
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
	job, err := m.jobRepo.GetByCase(context.Background(), caseID)
	if err != nil || job == nil || job.Status != entity.DecisionJobPaused {
		return false
	}
	if err := m.jobRepo.ResumeQueued(context.Background(), job.ID); err != nil {
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
	c, err := m.caseRepo.Get(context.Background(), caseID)
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
		_ = m.jobRepo.MarkPaused(context.Background(), jobID)
		return
	}
	_ = m.jobRepo.Cancel(context.Background(), jobID)
}

func (m *RunManager) IsRunning(caseID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[caseID]
	return ok
}
