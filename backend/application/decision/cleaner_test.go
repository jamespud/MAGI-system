package decision_test

import (
	"context"
	"sync"
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
