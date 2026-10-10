package magi_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestCaseGeneration_ReviewLegacyWritesCannotBypassGenerationOnMySQL(t *testing.T) {
	for _, operation := range []string{"status", "status_cas", "pause", "completion_event", "status_event", "failure_event"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			first, _ := claimArtifactOwner(t, jobs, job, "same-worker")
			requeueArtifactOwner(t, jobs, first, "same-worker")
			_, owner := claimArtifactOwner(t, jobs, job, "same-worker")
			if owner.ExecutionGeneration != 2 {
				t.Fatalf("generation=%d want=2", owner.ExecutionGeneration)
			}
			var beforeCase magi.CaseModel
			var beforeJob magi.DecisionJobModel
			if err := db.First(&beforeCase, "id = ?", owner.CaseID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&beforeJob, "id = ?", owner.JobID).Error; err != nil {
				t.Fatal(err)
			}
			switch operation {
			case "status":
				if err := repo.CaseRepo().UpdateStatus(ctx, owner.CaseID, entity.CaseStatusResolved); !errors.Is(err, port.ErrLeaseLost) {
					t.Errorf("legacy status err=%v want ErrLeaseLost", err)
				}
			case "status_cas":
				ok, err := repo.CaseRepo().(port.ConditionalCaseStatusWriter).UpdateStatusIfCurrent(ctx, owner.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusResolved)
				if err != nil || ok {
					t.Errorf("legacy status CAS=%v err=%v", ok, err)
				}
			case "pause":
				if err := repo.CaseRepo().(port.PauseStatusWriter).UpdatePaused(ctx, owner.CaseID, entity.CaseStatusPaused, entity.CaseStatusDraft); !errors.Is(err, port.ErrLeaseLost) {
					t.Errorf("legacy pause err=%v want ErrLeaseLost", err)
				}
			default:
				kind := entity.EventCaseCompleted
				if operation == "status_event" {
					kind = entity.EventCaseStatusChanged
				} else if operation == "failure_event" {
					kind = entity.EventCaseFailed
				}
				event := entity.NewEvent(owner.CaseID, "same-run", nil, kind, nil)
				seq := event.Seq
				if err := repo.EventRepo().Create(ctx, &event); !errors.Is(err, port.ErrLeaseLost) {
					t.Errorf("legacy event err=%v want ErrLeaseLost", err)
				}
				if event.Seq != seq {
					t.Errorf("rejected event Seq=%d want=%d", event.Seq, seq)
				}
			}
			var afterCase magi.CaseModel
			var afterJob magi.DecisionJobModel
			if err := db.First(&afterCase, "id = ?", owner.CaseID).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&afterJob, "id = ?", owner.JobID).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeCase, afterCase) || !reflect.DeepEqual(beforeJob, afterJob) {
				t.Errorf("legacy write changed Case/Job: Case before=%+v after=%+v Job before=%+v after=%+v", beforeCase, afterCase, beforeJob, afterJob)
			}
			assertT4State(t, db, owner, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
			res, event := t4Resolution(owner), t4Completion(owner, entity.CaseStatusResolved)
			ok, err := t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
			if err != nil || !ok {
				t.Fatalf("current owner terminal=%v err=%v", ok, err)
			}
			assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{event.ID})
		})
	}
}

func TestCaseGeneration_ReviewLegacyGenerationZeroCompatibilityOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, _, job := seedArtifactGenerationJob(t, db)
	cases := repo.CaseRepo()
	if err := cases.UpdateStatus(ctx, job.CaseID, entity.CaseStatusInvestigating); err != nil {
		t.Fatal(err)
	}
	if ok, err := cases.(port.ConditionalCaseStatusWriter).UpdateStatusIfCurrent(ctx, job.CaseID, []entity.CaseStatus{entity.CaseStatusInvestigating}, entity.CaseStatusDraft); err != nil || !ok {
		t.Fatalf("generation-0 CAS=%v err=%v", ok, err)
	}
	if err := cases.(port.PauseStatusWriter).UpdatePaused(ctx, job.CaseID, entity.CaseStatusPaused, entity.CaseStatusDraft); err != nil {
		t.Fatal(err)
	}
	if err := cases.(port.PauseStatusWriter).UpdatePaused(ctx, job.CaseID, entity.CaseStatusDraft, ""); err != nil {
		t.Fatal(err)
	}
	var eventIDs []string
	for i, kind := range []entity.EventType{entity.EventCaseStatusChanged, entity.EventCaseCompleted, entity.EventCaseFailed} {
		event := entity.NewEvent(job.CaseID, "legacy-run", nil, kind, nil)
		if err := repo.EventRepo().Create(ctx, &event); err != nil || event.Seq != uint64(i+1) {
			t.Fatalf("generation-0 event=%+v err=%v", event, err)
		}
		eventIDs = append(eventIDs, event.ID)
	}
	if err := cases.UpdateStatus(ctx, "missing-case", entity.CaseStatusResolved); !errors.Is(err, port.ErrLeaseLost) {
		t.Errorf("missing Case status err=%v", err)
	}
	if err := cases.(port.PauseStatusWriter).UpdatePaused(ctx, "missing-case", entity.CaseStatusPaused, entity.CaseStatusDraft); !errors.Is(err, port.ErrLeaseLost) {
		t.Errorf("missing Case pause err=%v", err)
	}
	assertT4State(t, db, &entity.ExecutionContext{CaseID: job.CaseID, JobID: job.ID}, entity.CaseStatusDraft, entity.DecisionJobQueued, nil, eventIDs)
}

func TestCaseGeneration_LegacyCommitsCannotWritePositiveCaseOnMySQL(t *testing.T) {
	ctx := context.Background()
	db := openArtifactGenerationMySQL(t)
	repo, jobs, job := seedArtifactGenerationJob(t, db)
	_, owner := claimArtifactOwner(t, jobs, job, "current-owner")
	// Generation-0 input must not bypass the persisted positive-generation
	// Case fence. None of these legacy calls carries active ownership.
	event := entity.NewEvent(owner.CaseID, "", nil, entity.EventCaseStatusChanged, nil)
	seq := event.Seq
	committed, err := repo.(port.StatusTransitionCommitter).CommitStatusTransition(ctx, owner.CaseID, []entity.CaseStatus{entity.CaseStatusDraft}, entity.CaseStatusInvestigating, &event)
	if err != nil || committed || event.Seq != seq {
		t.Fatalf("legacy status=%v err=%v event=%+v", committed, err, event)
	}
	event = entity.NewEvent(owner.CaseID, "", nil, entity.EventCaseCompleted, nil)
	res := t4Resolution(owner)
	res.ExecutionGeneration = 0
	committed, err = repo.(port.TerminalCommitter).CommitTerminal(ctx, owner.CaseID, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || committed {
		t.Fatalf("legacy terminal=%v err=%v", committed, err)
	}
	if err := repo.ResolutionRepo().Create(ctx, t4Resolution(owner)); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("unowned Resolution err=%v", err)
	}
	for _, kind := range []entity.EventType{entity.EventCaseStatusChanged, entity.EventCaseCompleted, entity.EventCaseFailed} {
		event.Type, event.ExecutionGeneration = kind, owner.ExecutionGeneration
		if err := repo.EventRepo().Create(ctx, &event); !errors.Is(err, port.ErrLeaseLost) {
			t.Fatalf("unowned authoritative Event type=%s err=%v", kind, err)
		}
	}
	assertT4State(t, db, owner, entity.CaseStatusDraft, entity.DecisionJobRunning, nil, nil)
	res, event = t4Resolution(owner), t4Completion(owner, entity.CaseStatusResolved)
	committed, err = t4Committer(repo).CommitTerminalOwned(ctx, owner, entity.CaseStatusDraft, entity.CaseStatusResolved, res, &event)
	if err != nil || !committed {
		t.Fatalf("owned terminal=%v err=%v", committed, err)
	}
	assertT4State(t, db, owner, entity.CaseStatusResolved, entity.DecisionJobSucceeded, []string{res.ID}, []string{event.ID})
	wrongWorker := *owner
	wrongWorker.WorkerID = "different-worker"
	settled, err := repo.(port.ExecutionSettlementReader).ExecutionSettled(ctx, &wrongWorker)
	if err != nil || settled {
		t.Fatalf("settlement replay accepted unrelated worker: %v err=%v", settled, err)
	}
}
