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
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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

func TestRunManager_GenerationRetryDoesNotUseCaseWideCleanup(t *testing.T) {
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
	if len(cleaner.calls) != 0 {
		t.Fatalf("generation-aware retry must not invoke case-wide cleaner: %v", cleaner.calls)
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

// TestRunManager_GenerationRetryIgnoresLegacyCleanupFailure proves T3
// correctness no longer depends on case-wide cleanup. The cleaner is unsafe for
// positive generations and is reserved for generation-0 compatibility until T5.
func TestRunManager_GenerationRetryIgnoresLegacyCleanupFailure(t *testing.T) {
	db := openJobDB(t)
	repo := magi.NewRepository(db)
	if err := repo.CaseRepo().Create(context.Background(), &entity.DecisionCase{ID: "case-bad-clean"}); err != nil {
		t.Fatalf("create case: %v", err)
	}
	jobs := magi.NewDecisionJobRepository(db)
	orch := &durableRetryOrchestrator{}
	cleaner := &failingCleaner{}
	rm := decision.NewRunManager(orch, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: repo.(port.OwnedCaseCommitter), WorkerID: "worker-bad-clean",
		MaxAttempts: 3, RetryBase: 10 * time.Millisecond, Cleaner: cleaner,
	})
	if err := rm.Start(context.Background(), &entity.DecisionCase{ID: "case-bad-clean"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	job := waitJobStatus(t, jobs, "case-bad-clean", entity.DecisionJobSucceeded)
	if job.Attempt != 2 || job.ExecutionGeneration != 2 {
		t.Fatalf("retry result = %+v, want attempt=2 generation=2", job)
	}
	if got := atomic.LoadInt32(&orch.calls); got != 2 {
		t.Fatalf("orchestrator calls = %d, want 2", got)
	}
	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	if len(cleaner.calls) != 0 {
		t.Fatalf("generation-aware retry invoked legacy cleaner: %v", cleaner.calls)
	}
}
