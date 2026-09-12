package decision_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
)

type recordingCleaner struct {
	mu    sync.Mutex
	calls []string
}

func (c *recordingCleaner) CleanupCaseArtifacts(_ context.Context, caseID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, caseID)
	return nil
}

func TestRunManager_RetryCleansPreviousAttemptArtifacts(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "case-clean"}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	orch := &durableRetryOrchestrator{}
	cleaner := &recordingCleaner{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-clean", MaxAttempts: 2, RetryBase: 10 * time.Millisecond,
		Cleaner: cleaner,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-clean"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, "case-clean", entity.DecisionJobSucceeded)
	if job.Attempt != 2 {
		t.Fatalf("expected 2 attempts, got %d", job.Attempt)
	}
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	if len(cleaner.calls) != 1 || cleaner.calls[0] != "case-clean" {
		t.Fatalf("cleaner calls: %v", cleaner.calls)
	}
}

type failingCleaner struct {
	mu    sync.Mutex
	calls []string
}

func (c *failingCleaner) CleanupCaseArtifacts(_ context.Context, caseID string) error {
	c.mu.Lock()
	c.calls = append(c.calls, caseID)
	c.mu.Unlock()
	return errors.New("cleanup unavailable")
}

// TestRunManager_RetryAbortsWhenCleanupFails proves the retry cleanup is a
// barrier: if attempt N-1 artifacts cannot be removed, attempt N must not start.
func TestRunManager_RetryAbortsWhenCleanupFails(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "case-bad-clean"}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	orch := &durableRetryOrchestrator{}
	cleaner := &failingCleaner{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, WorkerID: "worker-bad-clean", MaxAttempts: 3, RetryBase: 10 * time.Millisecond,
		Cleaner: cleaner,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-bad-clean"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The job must reach a terminal FAILED state via the fenced settlement, not
	// sit in RUNNING until its lease expires.
	job := waitJobStatus(t, jobs, "case-bad-clean", entity.DecisionJobFailed)
	if job.Status != entity.DecisionJobFailed {
		t.Fatalf("job status = %s, want %s", job.Status, entity.DecisionJobFailed)
	}
	if !strings.Contains(job.LastError, "retry cleanup failed") {
		t.Fatalf("last error = %q, want it to record the cleanup failure", job.LastError)
	}
	if job.WorkerID != "" {
		t.Fatalf("worker id = %q, want it cleared on settlement", job.WorkerID)
	}
	if got := atomic.LoadInt32(&orch.calls); got != 1 {
		t.Fatalf("orchestrator calls = %d, want 1 (retry must not run after a failed cleanup)", got)
	}
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	if len(cleaner.calls) != 1 {
		t.Fatalf("cleaner calls = %v, want exactly one", cleaner.calls)
	}
}
