package magi_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
)

func t4Completion(owner *entity.ExecutionContext, target entity.CaseStatus) entity.MagiEvent {
	event := entity.NewEvent(owner.CaseID, "same-run", nil, entity.EventCaseCompleted, map[string]any{"status": string(target)})
	if target == entity.CaseStatusFailed {
		event.Type = entity.EventCaseFailed
	}
	event.ExecutionGeneration = owner.ExecutionGeneration
	return event
}
func t4Resolution(owner *entity.ExecutionContext) *entity.Resolution {
	return &entity.Resolution{ID: "res-t4-" + uuid.NewString(), CaseID: owner.CaseID, ExecutionGeneration: owner.ExecutionGeneration, CreatedAt: time.Now()}
}

// Every failure checks the whole write set, including cursor and released
// ownership. Counts alone cannot distinguish a different generation's result.
func assertT4State(t *testing.T, db *gorm.DB, owner *entity.ExecutionContext, caseStatus entity.CaseStatus, jobStatus entity.DecisionJobStatus, resolutionIDs, eventIDs []string) {
	t.Helper()
	var c magi.CaseModel
	if err := db.Where("id = ?", owner.CaseID).First(&c).Error; err != nil {
		t.Fatal(err)
	}
	if c.Status != string(caseStatus) || c.ExecutionGeneration != owner.ExecutionGeneration {
		t.Fatalf("Case=%+v expected status=%s generation=%d", c, caseStatus, owner.ExecutionGeneration)
	}
	var job magi.DecisionJobModel
	if err := db.Where("id = ?", owner.JobID).First(&job).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != string(jobStatus) || job.CaseID != owner.CaseID || job.ExecutionGeneration != owner.ExecutionGeneration {
		t.Fatalf("Job=%+v", job)
	}
	if jobStatus == entity.DecisionJobRunning {
		if job.WorkerID != owner.WorkerID || job.LeaseUntil == nil {
			t.Fatalf("lost active owner: %+v", job)
		}
	} else if job.WorkerID != "" || job.LeaseUntil != nil {
		t.Fatalf("settlement kept owner: %+v", job)
	}
	var resolutions []magi.ResolutionModel
	if err := db.Where("case_id = ?", owner.CaseID).Find(&resolutions).Error; err != nil {
		t.Fatal(err)
	}
	if len(resolutions) != len(resolutionIDs) {
		t.Fatalf("resolutions=%+v want=%v", resolutions, resolutionIDs)
	}
	for i, res := range resolutions {
		if res.ID != resolutionIDs[i] || res.ExecutionGeneration != owner.ExecutionGeneration {
			t.Fatalf("resolution=%+v want=%v", res, resolutionIDs)
		}
	}
	var events []magi.EventModel
	if err := db.Where("case_id = ?", owner.CaseID).Order("seq ASC").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != len(eventIDs) {
		t.Fatalf("events=%+v want=%v", events, eventIDs)
	}
	for i, event := range events {
		if event.ID != eventIDs[i] || event.ExecutionGeneration != owner.ExecutionGeneration || event.Seq != uint64(i+1) {
			t.Fatalf("event=%+v want=%v", event, eventIDs)
		}
	}
	var cursors []magi.EventCursorModel
	if err := db.Where("case_id = ?", owner.CaseID).Find(&cursors).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		if len(cursors) != 0 {
			t.Fatalf("rejected transaction created cursor: %+v", cursors)
		}
	} else if len(cursors) != 1 || cursors[0].NextSeq != uint64(len(events)+1) {
		t.Fatalf("cursor=%+v events=%+v", cursors, events)
	}
}

func TestCaseGeneration_TerminalABAOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	first, old := claimArtifactOwner(t, jobs, job, "same-worker")
	requeueArtifactOwner(t, jobs, first, "same-worker")
	_, current := claimArtifactOwner(t, jobs, job, "same-worker")
	baseline := transitionT4Case(t, repo, current, entity.CaseStatusDraft, entity.CaseStatusEvaluating)
	if ok, err := t4Committer(repo).ResetCaseForRetryOwned(ctx, current, []entity.CaseStatus{entity.CaseStatusEvaluating}); err != nil || !ok {
		t.Fatalf("current retry reset=%v err=%v", ok, err)
	}
	event := t4Completion(old, entity.CaseStatusResolved)
	seq := event.Seq
	committed, err := t4Committer(repo).CommitTerminalOwned(ctx, old, entity.CaseStatusDraft, entity.CaseStatusResolved, t4Resolution(old), &event)
	if committed || (err != nil && !errors.Is(err, port.ErrLeaseLost)) {
		t.Fatalf("stale terminal=%v err=%v", committed, err)
	}
	if event.Seq != seq {
		t.Fatalf("rejected seq=%d want=%d", event.Seq, seq)
	}
	assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, []string{baseline.ID})
	res := t4Resolution(current)
	event = t4Completion(current, entity.CaseStatusResolved)
	committed, err = t4Committer(repo).CommitTerminalOwned(ctx, current, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || !committed {
		t.Fatalf("current terminal=%v err=%v", committed, err)
	}
	assertT4State(t, db, current, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{baseline.ID, event.ID})
}

func TestCaseGeneration_SameWorkerFinalFailureABAOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	_, jobs, job := seedArtifactGenerationJob(t, db)
	first, old := claimArtifactOwner(t, jobs, job, "same-worker")
	requeueArtifactOwner(t, jobs, first, "same-worker")
	_, current := claimArtifactOwner(t, jobs, job, "same-worker")
	event := t4Completion(old, entity.CaseStatusFailed)
	originalSeq := event.Seq
	committed, err := jobs.CommitFinalFailure(ctx, old.JobID, old.WorkerID, old.ExecutionGeneration, old.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "late failure", &event)
	if err != nil || committed || event.Seq != originalSeq {
		t.Fatalf("stale failure=%v err=%v event=%+v", committed, err, event)
	}
	assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
	event = t4Completion(current, entity.CaseStatusFailed)
	committed, err = jobs.CommitFinalFailure(ctx, current.JobID, current.WorkerID, current.ExecutionGeneration, current.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "final failure", &event)
	if err != nil || !committed {
		t.Fatalf("current failure=%v err=%v", committed, err)
	}
	assertT4State(t, db, current, entity.CaseStatusFailed, entity.DecisionJobFailed, nil, []string{event.ID})
}

func TestCaseGeneration_TerminalReferencesRollbackOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	artifacts := repo.(port.OwnedArtifactRepository)
	first, old := claimArtifactOwner(t, jobs, job, "same-worker")
	oldVote, oldEvidence, oldClaim := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := artifacts.CreateVoteOwned(ctx, old, &entity.Vote{ID: oldVote, CaseID: old.CaseID}); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.CreateEvidenceOwned(ctx, old, &entity.EvidenceRecord{ID: oldEvidence, CaseID: old.CaseID}); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.CreateClaimOwned(ctx, old, &entity.Claim{ID: oldClaim, CaseID: old.CaseID}); err != nil {
		t.Fatal(err)
	}
	requeueArtifactOwner(t, jobs, first, "same-worker")
	_, current := claimArtifactOwner(t, jobs, job, "same-worker")
	for _, kind := range []string{"vote", "evidence", "claim"} {
		t.Run(kind, func(t *testing.T) {
			res := t4Resolution(current)
			switch kind {
			case "vote":
				res.VoteIDs = []string{oldVote}
			case "evidence":
				res.KeyEvidenceIDs = []string{oldEvidence}
			case "claim":
				res.KeyClaimIDs = []string{oldClaim}
			}
			event := t4Completion(current, entity.CaseStatusResolved)
			seq := event.Seq
			committed, err := t4Committer(repo).CommitTerminalOwned(ctx, current, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
			if err == nil || committed || event.Seq != seq {
				t.Fatalf("cross-generation reference=%v err=%v event=%+v", committed, err, event)
			}
			assertT4State(t, db, current, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
		})
	}
	vote, evidence, claim := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := artifacts.CreateVoteOwned(ctx, current, &entity.Vote{ID: vote, CaseID: current.CaseID}); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.CreateEvidenceOwned(ctx, current, &entity.EvidenceRecord{ID: evidence, CaseID: current.CaseID}); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.CreateClaimOwned(ctx, current, &entity.Claim{ID: claim, CaseID: current.CaseID}); err != nil {
		t.Fatal(err)
	}
	res := t4Resolution(current)
	res.VoteIDs, res.KeyEvidenceIDs, res.KeyClaimIDs = []string{vote}, []string{evidence}, []string{claim}
	event := t4Completion(current, entity.CaseStatusResolved)
	committed, err := t4Committer(repo).CommitTerminalOwned(ctx, current, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || !committed {
		t.Fatalf("valid references=%v err=%v", committed, err)
	}
	assertT4State(t, db, current, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{event.ID})
}

type t4ReplyLossPool struct {
	gorm.ConnPool
	sqlDB             *sql.DB
	lost              atomic.Bool
	blockConfirmation bool
	afterCommit       func()
}

func (p *t4ReplyLossPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	if p.blockConfirmation && p.lost.Load() {
		return nil, errors.New("confirmation unavailable")
	}
	tx, err := p.sqlDB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &t4ReplyLossTx{Tx: tx, pool: p}, nil
}

type t4ReplyLossTx struct {
	*sql.Tx
	pool *t4ReplyLossPool
}

func (tx *t4ReplyLossTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.lost.CompareAndSwap(false, true) {
		if tx.pool.afterCommit != nil {
			tx.pool.afterCommit()
		}
		return errors.New("COMMIT succeeded; reply lost")
	}
	return nil
}
func t4FaultDB(t *testing.T, db *gorm.DB, unavailable bool, afterCommit func()) (*gorm.DB, *t4ReplyLossPool) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool := &t4ReplyLossPool{ConnPool: sqlDB, sqlDB: sqlDB, blockConfirmation: unavailable, afterCommit: afterCommit}
	fault := db.Session(&gorm.Session{NewDB: true, Initialized: true})
	fault.Config.ConnPool, fault.Statement.ConnPool = pool, pool
	return fault, pool
}

func TestCaseGeneration_PostCommitReplyLossOnMySQL(t *testing.T) {
	for _, target := range []entity.CaseStatus{entity.CaseStatusResolved, entity.CaseStatusDeadlocked, entity.CaseStatusFailed} {
		t.Run(string(target), func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "reply-loss")
			fault, pool := t4FaultDB(t, db, false, nil)
			event := t4Completion(owner, target)
			var res *entity.Resolution
			var committed bool
			var err error
			if target == entity.CaseStatusFailed {
				committed, err = magi.NewDecisionJobRepository(fault).CommitFinalFailure(ctx, owner.JobID, owner.WorkerID, owner.ExecutionGeneration, owner.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "failed", &event)
			} else {
				if target == entity.CaseStatusResolved {
					res = t4Resolution(owner)
				}
				committed, err = t4Committer(magi.NewRepository(fault)).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, target, res, &event)
			}
			if err != nil || !committed || !pool.lost.Load() || event.Seq != 1 {
				t.Fatalf("reply loss commit=%v err=%v injected=%v event=%+v", committed, err, pool.lost.Load(), event)
			}
			resolutions := []string(nil)
			if res != nil {
				resolutions = []string{res.ID}
			}
			jobStatus := entity.DecisionJobSucceeded
			if target == entity.CaseStatusFailed {
				jobStatus = entity.DecisionJobFailed
			}
			assertT4State(t, db, owner, target, jobStatus, resolutions, []string{event.ID})
			// Reusing the exact submission cannot create a second effect.
			if target == entity.CaseStatusFailed {
				committed, err = jobs.CommitFinalFailure(ctx, owner.JobID, owner.WorkerID, owner.ExecutionGeneration, owner.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, "failed", &event)
			} else {
				committed, err = t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, target, res, &event)
			}
			if err != nil || committed {
				t.Fatalf("duplicate terminal=%v err=%v", committed, err)
			}
			if err := jobs.RequeueExpired(ctx, time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if claimed, ok, err := jobs.Claim(ctx, job.ID, "next-worker", uuid.NewString(), time.Now().Add(time.Minute)); err != nil || ok || claimed != nil {
				t.Fatalf("terminal re-claim=%+v ok=%v err=%v", claimed, ok, err)
			}
			assertT4State(t, db, owner, target, jobStatus, resolutions, []string{event.ID})
		})
	}
}

func TestCaseGeneration_UnknownOutcomeIsNotStatusOnlySuccessOnMySQL(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprintf("unavailable_%v", unavailable), func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			_, owner := claimArtifactOwner(t, jobs, job, "unknown")
			event := t4Completion(owner, entity.CaseStatusResolved)
			res := t4Resolution(owner)
			actualID, seq := event.ID, event.Seq
			var hook func()
			if !unavailable {
				hook = func() { event.ID = "different-submission" }
			}
			fault, pool := t4FaultDB(t, db, unavailable, hook)
			committed, err := t4Committer(magi.NewRepository(fault)).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
			if committed || !errors.Is(err, port.ErrCommitOutcomeUnknown) || !pool.lost.Load() || event.Seq != seq {
				t.Fatalf("unknown outcome=%v err=%v event=%+v", committed, err, event)
			}
			assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{actualID})
			stored, err := repo.EventRepo().ListByCase(ctx, owner.CaseID)
			if err != nil || len(stored) != 1 || stored[0].ExecutionGeneration != owner.ExecutionGeneration || stored[0].Seq != 1 {
				t.Fatalf("authoritative events=%+v err=%v", stored, err)
			}
		})
	}
}

func TestCaseGeneration_EventInsertFailureRollsBackTerminalOnMySQL(t *testing.T) {
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "rollback")
	event := t4Completion(owner, entity.CaseStatusResolved)
	// Existing event creates cursor=2. The duplicate fails after Case and
	// Resolution writes and cursor allocation, all of which must roll back.
	if err := db.Create(&magi.EventCursorModel{CaseID: owner.CaseID, NextSeq: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&magi.EventModel{ID: event.ID, CaseID: owner.CaseID, ExecutionGeneration: owner.ExecutionGeneration, Seq: 1, Type: string(event.Type), PayloadJSON: string(event.Payload), Timestamp: event.Timestamp}).Error; err != nil {
		t.Fatal(err)
	}
	event.Seq = 999
	committed, err := t4Committer(repo).CommitTerminalOwned(context.Background(), owner, entity.CaseStatusDraft, entity.CaseStatusResolved, t4Resolution(owner), &event)
	if committed || err == nil || errors.Is(err, port.ErrCommitOutcomeUnknown) || event.Seq != 999 {
		t.Fatalf("rollback=%v err=%v event=%+v", committed, err, event)
	}
	assertT4State(t, db, owner, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, []string{event.ID})
}
