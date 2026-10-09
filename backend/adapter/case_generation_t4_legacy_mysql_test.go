package magi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

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
