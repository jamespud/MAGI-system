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

type runHandle struct {
	cancel    context.CancelFunc
	done      chan struct{}
	slotTaken bool
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
	RunCounter               port.RunCounter
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
	runCounter           port.RunCounter
	userRuns             map[int64]int
	mu                   sync.Mutex
	runs                 map[string]*runHandle
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
		runCounter: d.RunCounter,
		userRuns:   make(map[int64]int), runs: make(map[string]*runHandle),
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
	slotHeld := false
	m.mu.Lock()
	_, local := m.runs[c.ID]
	m.mu.Unlock()
	if local {
		return ErrAlreadyRunning
	}
	if m.maxConcurrentPerUser > 0 && c.UserID != 0 {
		if m.runCounter != nil {
			// Atomic cross-instance slot: the counter increments and checks the
			// limit in one statement, closing the check-then-act race between
			// replicas. The slot is released when the run finishes.
			ok, err := m.runCounter.Acquire(ctx, c.UserID, m.maxConcurrentPerUser)
			if err != nil {
				return fmt.Errorf("run manager: acquire run slot: %w", err)
			}
			if !ok {
				return ErrRateLimited
			}
			slotHeld = true
		} else if m.jobRepo != nil {
			active, err := m.jobRepo.CountActiveByUser(ctx, c.UserID)
			if err != nil {
				return fmt.Errorf("run manager: count active runs: %w", err)
			}
			if active >= m.maxConcurrentPerUser {
				return ErrRateLimited
			}
		} else {
			m.mu.Lock()
			active := m.userRuns[c.UserID]
			m.mu.Unlock()
			if active >= m.maxConcurrentPerUser {
				return ErrRateLimited
			}
		}
	}

	var job *entity.DecisionJob
	if m.jobRepo != nil {
		if err := m.jobRepo.RequeueExpired(ctx, time.Now()); err != nil {
			return fmt.Errorf("run manager: recover expired job: %w", err)
		}
		var err error
		job, err = m.jobRepo.Enqueue(ctx, c.ID, m.maxAttempts)
		if err != nil {
			if slotHeld {
				_ = m.runCounter.Release(context.Background(), c.UserID)
			}
			return fmt.Errorf("run manager: enqueue: %w", err)
		}
		switch job.Status {
		case entity.DecisionJobSucceeded:
			if slotHeld {
				_ = m.runCounter.Release(context.Background(), c.UserID)
			}
			return ErrAlreadyCompleted
		case entity.DecisionJobRunning:
			if slotHeld {
				_ = m.runCounter.Release(context.Background(), c.UserID)
			}
			return ErrAlreadyRunning
		}
	}
	if !m.launch(c, job, slotHeld) {
		if slotHeld {
			_ = m.runCounter.Release(context.Background(), c.UserID)
		}
		return ErrAlreadyRunning
	}
	return nil
}

func (m *RunManager) launch(c *entity.DecisionCase, job *entity.DecisionJob, slotHeld bool) bool {
	runCtx, cancel := context.WithCancel(context.Background())
	h := &runHandle{cancel: cancel, done: make(chan struct{}), slotTaken: slotHeld}
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
			if h.slotTaken && m.runCounter != nil && c.UserID != 0 {
				_ = m.runCounter.Release(context.Background(), c.UserID)
			}
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
	for {
		if ctx.Err() != nil {
			if job != nil {
				_ = m.jobRepo.Cancel(context.Background(), job.ID)
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
			if m.cleaner != nil {
				_ = m.cleaner.CleanupCaseArtifacts(context.Background(), c.ID)
			}
			c.Status = entity.CaseStatusDraft
			if m.caseRepo != nil {
				_ = m.caseRepo.UpdateStatus(context.Background(), c.ID, entity.CaseStatusDraft)
			}
		}
		stopHeartbeat := m.startHeartbeat(ctx, claimed.ID)
		runStart := time.Now()
		m.metrics.RunStart()
		m.metrics.RunStartForUser(userIDString(c.UserID))
		_, runErr := m.orch.Orchestrate(ctx, c)
		m.metrics.RunFinish(runErr == nil)
		m.metrics.RunFinishForUser(userIDString(c.UserID))
		m.metrics.RecordRunDuration(time.Since(runStart).Milliseconds())
		stopHeartbeat()

		if runErr == nil {
			_ = m.jobRepo.MarkSucceeded(context.Background(), claimed.ID, m.workerID)
			return
		}
		if ctx.Err() != nil {
			_ = m.jobRepo.Cancel(context.Background(), claimed.ID)
			return
		}
		if claimed.Attempt < claimed.MaxAttempts {
			retryAt := time.Now().Add(m.retryDelay(claimed.Attempt))
			_ = m.jobRepo.MarkFailed(context.Background(), claimed.ID, m.workerID, runErr.Error(), &retryAt)
			timer := time.NewTimer(time.Until(retryAt))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				_ = m.jobRepo.Cancel(context.Background(), claimed.ID)
				return
			case <-timer.C:
			}
			continue
		}
		_ = m.jobRepo.MarkFailed(context.Background(), claimed.ID, m.workerID, runErr.Error(), nil)
		return
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

func (m *RunManager) startHeartbeat(ctx context.Context, jobID string) func() {
	interval := m.lease / 3
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		defer close(done)
		for {
			select {
			case <-ticker.C:
				_ = m.jobRepo.Heartbeat(context.Background(), jobID, m.workerID, time.Now().Add(m.lease))
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
		m.launch(c, job, false)
	}
	return nil
}

// Cancel cancels a local worker or a durable queued/running job.
func (m *RunManager) Cancel(caseID string) bool {
	m.mu.Lock()
	h, local := m.runs[caseID]
	m.mu.Unlock()
	if local {
		h.cancel()
	}
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

func (m *RunManager) IsRunning(caseID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.runs[caseID]
	return ok
}
