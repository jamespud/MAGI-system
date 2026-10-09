package magi_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

type t4ContextKey struct{}
type t4Outcome struct {
	committed bool
	err       error
}

func TestCaseGeneration_ControlWinsOnMySQL(t *testing.T) {
	for _, target := range []entity.CaseStatus{entity.CaseStatusPaused, entity.CaseStatusCancelled} {
		t.Run(string(target), func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "control")
			locked, release, lateStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			var lockedOnce, lateOnce sync.Once
			afterName, beforeName := "t4:control_locked", "t4:late_started"
			if err := db.Callback().Query().After("gorm:query").Register(afterName, func(tx *gorm.DB) {
				if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "control" && tx.Error == nil {
					lockedOnce.Do(func() { close(locked); <-release })
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(afterName)
			if err := db.Callback().Query().Before("gorm:query").Register(beforeName, func(tx *gorm.DB) {
				if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "late" {
					lateOnce.Do(func() { close(lateStarted) })
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(beforeName)
			controlDone, lateDone := make(chan t4Outcome, 1), make(chan t4Outcome, 1)
			controlCtx, cancelControl := context.WithTimeout(context.WithValue(ctx, t4ContextKey{}, "control"), 5*time.Second)
			defer cancelControl()
			go func() {
				ok, err := repo.(port.CaseControlCommitter).CommitCaseControl(controlCtx, owner.CaseID, target)
				controlDone <- t4Outcome{ok, err}
			}()
			select {
			case <-locked:
			case <-controlCtx.Done():
				t.Fatal("control never acquired Job lock")
			}
			event := entity.NewEvent(owner.CaseID, "same-run", nil, entity.EventCaseStatusChanged, nil)
			event.ExecutionGeneration = owner.ExecutionGeneration
			seq := event.Seq
			lateCtx, cancelLate := context.WithTimeout(context.WithValue(ctx, t4ContextKey{}, "late"), 5*time.Second)
			defer cancelLate()
			go func() {
				ok, err := t4Committer(repo).CommitStatusTransitionOwned(lateCtx, owner, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusInvestigating, &event)
				lateDone <- t4Outcome{ok, err}
			}()
			select {
			case <-lateStarted:
			case <-lateCtx.Done():
				t.Fatal("late write never reached Job lock")
			}
			releaseOnce.Do(func() { close(release) })
			control, late := <-controlDone, <-lateDone
			if !control.committed || control.err != nil || late.committed || late.err != nil || event.Seq != seq {
				t.Fatalf("control=%+v late=%+v seq=%d want=%d", control, late, event.Seq, seq)
			}
			jobStatus := entity.DecisionJobPaused
			if target == entity.CaseStatusCancelled {
				jobStatus = entity.DecisionJobCancelled
			}
			assertT4State(t, db, owner, target, jobStatus, nil, nil)
			terminalEvent := t4Completion(owner, entity.CaseStatusResolved)
			ok, err := t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, t4Resolution(owner), &terminalEvent)
			if ok || err != nil {
				t.Fatalf("late terminal=%v err=%v", ok, err)
			}
			ok, err = t4Committer(repo).ResetCaseForRetryOwned(ctx, owner, []entity.CaseStatus{target, entity.CaseStatusDraft})
			if ok || err != nil {
				t.Fatalf("late reset=%v err=%v", ok, err)
			}
			failure := t4Completion(owner, entity.CaseStatusFailed)
			ok, err = jobs.CommitFinalFailure(ctx, owner.JobID, owner.WorkerID, owner.ExecutionGeneration, owner.CaseID, []entity.CaseStatus{target}, "late failure", &failure)
			if ok || err != nil {
				t.Fatalf("late failure=%v err=%v", ok, err)
			}
			err = repo.(port.OwnedArtifactRepository).CreateEvidenceOwned(ctx, owner, &entity.EvidenceRecord{ID: uuid.NewString(), CaseID: owner.CaseID})
			if !errors.Is(err, port.ErrLeaseLost) {
				t.Fatalf("late artifact err=%v", err)
			}
			var count int64
			if err := db.Model(&magi.EvidenceModel{}).Where("case_id = ?", owner.CaseID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("late artifact count=%d err=%v", count, err)
			}
			assertT4State(t, db, owner, target, jobStatus, nil, nil)
			if target == entity.CaseStatusPaused {
				// Returning to the pre-pause state with the same worker is
				// another ABA; only the newly claimed generation may continue.
				if err := repo.CaseRepo().UpdateStatus(ctx, owner.CaseID, entity.CaseStatusDraft); err != nil {
					t.Fatal(err)
				}
				if err := jobs.ResumeQueued(ctx, owner.JobID); err != nil {
					t.Fatal(err)
				}
				_, next := claimArtifactOwner(t, jobs, job, owner.WorkerID)
				ok, err = t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, t4Resolution(owner), &terminalEvent)
				if ok || err != nil {
					t.Fatalf("resumed stale terminal=%v err=%v", ok, err)
				}
				nextEvent, res := t4Completion(next, entity.CaseStatusResolved), t4Resolution(next)
				ok, err = t4Committer(repo).CommitTerminalOwned(ctx, next, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &nextEvent)
				if err != nil || !ok {
					t.Fatalf("resumed current terminal=%v err=%v", ok, err)
				}
				assertT4State(t, db, next, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{nextEvent.ID})
			}
		})
	}
}

func TestCaseGeneration_TerminalWinsExternalControlOnMySQL(t *testing.T) {
	for _, target := range []entity.CaseStatus{entity.CaseStatusPaused, entity.CaseStatusCancelled} {
		t.Run(string(target), func(t *testing.T) {
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "terminal-first")
			event, res := t4Completion(owner, entity.CaseStatusResolved), t4Resolution(owner)
			ok, err := t4Committer(repo).CommitTerminalOwned(context.Background(), owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
			if err != nil || !ok {
				t.Fatalf("terminal=%v err=%v", ok, err)
			}
			ok, err = repo.(port.CaseControlCommitter).CommitCaseControl(context.Background(), owner.CaseID, target)
			if err != nil || ok {
				t.Fatalf("control after terminal=%v err=%v", ok, err)
			}
			assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{event.ID})
		})
	}
}

func TestCaseGeneration_PauseReadsLockedCaseOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "pause-read")
	blocker := openA2AMySQL(t).Begin()
	defer blocker.Rollback()
	var c magi.CaseModel
	if err := blocker.Raw("SELECT * FROM decision_case WHERE id = ? FOR UPDATE", owner.CaseID).Scan(&c).Error; err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	var once sync.Once
	name := "t4:pause_job_locked"
	if err := db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "pause" && tx.Error == nil {
			once.Do(func() { close(locked) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	pauseCtx, cancel := context.WithTimeout(context.WithValue(ctx, t4ContextKey{}, "pause"), 5*time.Second)
	defer cancel()
	done := make(chan t4Outcome, 1)
	go func() {
		ok, err := repo.(port.CaseControlCommitter).CommitCaseControl(pauseCtx, owner.CaseID, entity.CaseStatusPaused)
		done <- t4Outcome{ok, err}
	}()
	select {
	case <-locked:
	case <-pauseCtx.Done():
		t.Fatal("pause never locked Job")
	}
	if err := blocker.Model(&magi.CaseModel{}).Where("id = ?", owner.CaseID).Update("status", string(entity.CaseStatusInvestigating)).Error; err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	result := <-done
	if !result.committed || result.err != nil {
		t.Fatalf("pause=%+v", result)
	}
	if err := db.Where("id = ?", owner.CaseID).First(&c).Error; err != nil {
		t.Fatal(err)
	}
	if c.PausedFromStatus != string(entity.CaseStatusInvestigating) {
		t.Fatalf("paused_from_status=%q", c.PausedFromStatus)
	}
	assertT4State(t, db, owner, entity.CaseStatusPaused, entity.DecisionJobPaused, nil, nil)
}

func TestCaseGeneration_ControlRollbackAndReplyLossOnMySQL(t *testing.T) {
	for _, loss := range []bool{false, true} {
		name := "rollback"
		if loss {
			name = "reply_loss"
		}
		t.Run(name, func(t *testing.T) {
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "control-fault")
			if loss {
				fault, pool := t4FaultDB(t, db, false, nil)
				ok, err := magi.NewRepository(fault).(port.CaseControlCommitter).CommitCaseControl(context.Background(), owner.CaseID, entity.CaseStatusPaused)
				if !ok || err != nil || !pool.lost.Load() {
					t.Fatalf("control reply loss=%v err=%v", ok, err)
				}
				assertT4State(t, db, owner, entity.CaseStatusPaused, entity.DecisionJobPaused, nil, nil)
			} else {
				injected := errors.New("Job control update failed")
				callback := "t4:control_failure"
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "decision_job" {
						tx.AddError(injected)
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Update().Remove(callback)
				ok, err := repo.(port.CaseControlCommitter).CommitCaseControl(context.Background(), owner.CaseID, entity.CaseStatusCancelled)
				if ok || !errors.Is(err, injected) {
					t.Fatalf("control rollback=%v err=%v", ok, err)
				}
				assertT4State(t, db, owner, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
			}
		})
	}
}

func TestCaseGeneration_LeaseCheckedAfterLockAcquisitionOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "lock-expiry")
	blocker := openA2AMySQL(t).Begin()
	defer blocker.Rollback()
	var id string
	if err := blocker.Raw("SELECT id FROM decision_job WHERE id = ? FOR UPDATE", owner.JobID).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	name := "t4:lease_gate"
	if err := db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "decision_job" && tx.Statement.Context.Value(t4ContextKey{}) == "expiry" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Query().Remove(name)
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), t4ContextKey{}, "expiry"), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- repo.(port.OwnedArtifactRepository).CreateEvidenceOwned(ctx, owner, &entity.EvidenceRecord{ID: uuid.NewString(), CaseID: owner.CaseID})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("owner never reached lock")
	}
	// The old implementation sampled application time before this barrier.
	// Expiry is now after that sample but before the owner can acquire locks.
	if err := blocker.Exec("UPDATE decision_job SET lease_until = CURRENT_TIMESTAMP(6) WHERE id = ?", owner.JobID).Error; err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit().Error; err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-done; !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("expired-after-wait err=%v", err)
	}
	assertT4State(t, db, owner, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
	var count int64
	if err := db.Model(&magi.EvidenceModel{}).Where("case_id = ?", owner.CaseID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("late evidence count=%d err=%v", count, err)
	}
}
