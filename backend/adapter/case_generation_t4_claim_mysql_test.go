package magi_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/gorm"
)

func TestCaseGeneration_ConcurrentRetryClaimWinsOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	if err := repo.CaseRepo().UpdateStatus(context.Background(), job.CaseID, entity.CaseStatusInvestigating); err != nil {
		t.Fatal(err)
	}
	first, old := claimArtifactOwner(t, jobs, job, "same-worker")
	requeueArtifactOwner(t, jobs, first, "same-worker")
	locked, release, lateStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once, releaseOnce, lateOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	lockName, startName := "t4:claim_holds_locks", "t4:retry_reset_started"
	if err := db.Callback().Create().After("gorm:create").Register(lockName, func(tx *gorm.DB) {
		if tx.Statement.Table == "decision_job_claim" && tx.Statement.Context.Value(t4ContextKey{}) == "claim" && tx.Error == nil {
			once.Do(func() { close(locked); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove(lockName)
	if err := db.Callback().Query().Before("gorm:query").Register(startName, func(tx *gorm.DB) {
		if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "reset" {
			lateOnce.Do(func() { close(lateStarted) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(startName)
	claimCtx, cancelClaim := context.WithTimeout(context.WithValue(context.Background(), t4ContextKey{}, "claim"), 5*time.Second)
	defer cancelClaim()
	type claimResult struct {
		job *entity.DecisionJob
		ok  bool
		err error
	}
	claimedDone := make(chan claimResult, 1)
	go func() {
		j, ok, err := jobs.Claim(claimCtx, job.ID, "same-worker", uuid.NewString(), time.Now().Add(time.Minute))
		claimedDone <- claimResult{j, ok, err}
	}()
	select {
	case <-locked:
	case <-claimCtx.Done():
		t.Fatal("new Claim never acquired ownership locks")
	}
	resetCtx, cancelReset := context.WithTimeout(context.WithValue(context.Background(), t4ContextKey{}, "reset"), 5*time.Second)
	defer cancelReset()
	resetDone := make(chan t4Outcome, 1)
	go func() {
		ok, err := t4Committer(repo).ResetCaseForRetryOwned(resetCtx, old, []entity.CaseStatus{entity.CaseStatusInvestigating})
		resetDone <- t4Outcome{ok, err}
	}()
	select {
	case <-lateStarted:
	case <-resetCtx.Done():
		t.Fatal("old reset never reached ownership lock")
	}
	releaseOnce.Do(func() { close(release) })
	claimed, reset := <-claimedDone, <-resetDone
	if !claimed.ok || claimed.err != nil || reset.committed || reset.err != nil {
		t.Fatalf("claim=%+v reset=%+v", claimed, reset)
	}
	current := &entity.ExecutionContext{CaseID: claimed.job.CaseID, JobID: claimed.job.ID, WorkerID: claimed.job.WorkerID, ExecutionGeneration: claimed.job.ExecutionGeneration}
	assertT4State(t, db, current, entity.CaseStatusInvestigating, entity.DecisionJobRunning, nil, nil)
	if ok, err := t4Committer(repo).ResetCaseForRetryOwned(context.Background(), current, []entity.CaseStatus{entity.CaseStatusInvestigating}); err != nil || !ok {
		t.Fatalf("current reset=%v err=%v", ok, err)
	}
	assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
	if ok, err := t4Committer(repo).ResetCaseForRetryOwned(context.Background(), current, []entity.CaseStatus{entity.CaseStatusDraft}); err != nil || !ok {
		t.Fatalf("current DRAFT no-op=%v err=%v", ok, err)
	}
	assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
}

func TestCaseGeneration_HistoricalTerminalReconciliationOnMySQL(t *testing.T) {
	for _, target := range []entity.CaseStatus{entity.CaseStatusResolved, entity.CaseStatusDeadlocked} {
		t.Run(string(target), func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "crashed-worker")
			event := t4Completion(owner, target)
			var res *entity.Resolution
			var resolutionIDs []string
			if target == entity.CaseStatusResolved {
				res = t4Resolution(owner)
				resolutionIDs = []string{res.ID}
			}
			if ok, err := t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, target, res, &event); err != nil || !ok {
				t.Fatalf("terminal=%v err=%v", ok, err)
			}
			// Reconstruct the historical split-commit crash window without
			// changing its terminal Case/Resolution/Event generation.
			if err := db.Model(&magi.DecisionJobModel{}).Where("id = ?", owner.JobID).Updates(map[string]any{"status": string(entity.DecisionJobRunning), "worker_id": owner.WorkerID, "lease_until": time.Now().Add(-time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := jobs.Claim(ctx, job.ID, "replacement-worker", uuid.NewString(), time.Now().Add(time.Minute))
			if err != nil || ok || claimed != nil {
				t.Fatalf("terminal replacement=%+v ok=%v err=%v", claimed, ok, err)
			}
			assertT4State(t, db, owner, target, entity.DecisionJobSucceeded, resolutionIDs, []string{event.ID})
			current, err := jobs.GetByCase(ctx, job.CaseID)
			if err != nil || current.Attempt != 1 {
				t.Fatalf("reconciliation allocated attempt: %+v err=%v", current, err)
			}
			var count int64
			if err := db.Model(&magi.DecisionJobClaimModel{}).Where("case_id = ?", owner.CaseID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("reconciliation created Claim count=%d err=%v", count, err)
			}
		})
	}
}

func TestCaseGeneration_ReviewHistoricalTerminalClassificationOnMySQL(t *testing.T) {
	for _, tc := range []struct {
		status entity.CaseStatus
		job    entity.DecisionJobStatus
	}{
		{entity.CaseStatusMemoryIndexed, entity.DecisionJobSucceeded},
		{entity.CaseStatusInsufficientEv, entity.DecisionJobSucceeded},
		{entity.CaseStatusFailed, entity.DecisionJobFailed},
		{entity.CaseStatusTimedOut, entity.DecisionJobFailed},
		{entity.CaseStatusCancelled, entity.DecisionJobCancelled},
		{entity.CaseStatusPaused, entity.DecisionJobPaused},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			_, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "crashed-worker")
			// Explicitly reconstruct historical state; public legacy APIs must
			// not be able to create this window for a positive generation.
			if err := db.Model(&magi.CaseModel{}).Where("id = ?", owner.CaseID).Update("status", string(tc.status)).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&magi.DecisionJobModel{}).Where("id = ?", owner.JobID).Update("lease_until", time.Now().Add(-time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			if err := jobs.RequeueExpired(ctx, time.Now()); err != nil {
				t.Fatal(err)
			}
			claimed, ok, err := jobs.Claim(ctx, job.ID, "replacement-worker", uuid.NewString(), time.Now().Add(time.Minute))
			if err != nil || ok || claimed != nil {
				t.Fatalf("historical replacement=%+v ok=%v err=%v", claimed, ok, err)
			}
			assertT4State(t, db, owner, tc.status, tc.job, nil, nil)
			current, err := jobs.GetByCase(ctx, job.CaseID)
			if err != nil || current.Attempt != 1 {
				t.Fatalf("reconciliation allocated attempt: %+v err=%v", current, err)
			}
			var count int64
			if err := db.Model(&magi.DecisionJobClaimModel{}).Where("case_id = ?", owner.CaseID).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("reconciliation created Claim count=%d err=%v", count, err)
			}
		})
	}
}

func TestCaseGeneration_TerminalWithoutReceiptCannotClaimOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	first, owner := claimArtifactOwner(t, jobs, job, "crashed-worker")
	requeueArtifactOwner(t, jobs, first, "crashed-worker")
	// Deliberately seed an incomplete historical terminal row through SQL;
	// generation-0 APIs must reject creating it after ownership was claimed.
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", owner.CaseID).Update("status", string(entity.CaseStatusResolved)).Error; err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := jobs.Claim(ctx, job.ID, "replacement-worker", uuid.NewString(), time.Now().Add(time.Minute))
	if err == nil || ok || claimed != nil {
		t.Fatalf("incomplete terminal replacement=%+v ok=%v err=%v", claimed, ok, err)
	}
	assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobQueued, nil, nil)
}
