package magi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func t4Committer(repo port.Repository) port.OwnedCaseCommitter {
	return repo.(port.OwnedCaseCommitter)
}

func transitionT4Case(t *testing.T, repo port.Repository, owner *entity.ExecutionContext, expected, target entity.CaseStatus) entity.MagiEvent {
	t.Helper()
	event := entity.NewEvent(owner.CaseID, "same-run", nil, entity.EventCaseStatusChanged, nil)
	event.ExecutionGeneration = owner.ExecutionGeneration
	if ok, err := t4Committer(repo).CommitStatusTransitionOwned(context.Background(), owner, []entity.CaseStatus{expected}, target, &event); err != nil || !ok {
		t.Fatalf("owned setup transition=%v err=%v", ok, err)
	}
	return event
}

func TestCaseGeneration_StatusAndRetryResetABAOnMySQL(t *testing.T) {
	for _, reset := range []bool{false, true} {
		name := "status"
		if reset {
			name = "retry_reset"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db := openArtifactGenerationMySQL(t)
			repo, jobs, job := seedArtifactGenerationJob(t, db)
			if err := repo.CaseRepo().UpdateStatus(ctx, job.CaseID, entity.CaseStatusInvestigating); err != nil {
				t.Fatal(err)
			}
			first, owner1 := claimArtifactOwner(t, jobs, job, "same-worker")
			requeueArtifactOwner(t, jobs, first, "same-worker")
			second, owner2 := claimArtifactOwner(t, jobs, job, "same-worker")
			// Real ABA: move away from S and return to S under the next claim.
			owned := t4Committer(repo)
			if ok, err := owned.ResetCaseForRetryOwned(ctx, owner2, []entity.CaseStatus{entity.CaseStatusInvestigating}); err != nil || !ok {
				t.Fatalf("current retry reset=%v err=%v", ok, err)
			}
			baseline := transitionT4Case(t, repo, owner2, entity.CaseStatusDraft, entity.CaseStatusInvestigating)
			event := entity.NewEvent(job.CaseID, "same-run", nil, entity.EventCaseStatusChanged, nil)
			originalSeq := event.Seq
			commit := func(owner *entity.ExecutionContext) (bool, error) {
				event.ExecutionGeneration = owner.ExecutionGeneration
				if reset {
					return owned.ResetCaseForRetryOwned(ctx, owner, []entity.CaseStatus{entity.CaseStatusInvestigating})
				}
				return owned.CommitStatusTransitionOwned(ctx, owner, []entity.CaseStatus{entity.CaseStatusInvestigating}, entity.CaseStatusDraft, &event)
			}
			committed, err := commit(owner1)
			if committed || (err != nil && !errors.Is(err, port.ErrLeaseLost)) {
				t.Errorf("stale generation commit=%v err=%v", committed, err)
			}
			after, err := repo.CaseRepo().Get(ctx, job.CaseID)
			if err != nil || after.Status != entity.CaseStatusInvestigating || after.ExecutionGeneration != second.ExecutionGeneration {
				t.Fatalf("stale writer changed Case: %+v err=%v", after, err)
			}
			events, err := repo.EventRepo().ListByCase(ctx, job.CaseID)
			if err != nil || len(events) != 1 || events[0].ID != baseline.ID || event.Seq != originalSeq {
				t.Fatalf("rejected event=%+v events=%v err=%v", event, events, err)
			}
			assertT4State(t, db, owner2, entity.CaseStatusInvestigating, entity.DecisionJobRunning, nil, []string{baseline.ID})
			current, err := jobs.GetByCase(ctx, job.CaseID)
			if err != nil || current.Status != entity.DecisionJobRunning || current.WorkerID != owner2.WorkerID || current.ExecutionGeneration != owner2.ExecutionGeneration {
				t.Fatalf("Job=%+v err=%v", current, err)
			}
			committed, err = commit(owner2)
			if err != nil || !committed {
				t.Fatalf("current owner commit=%v err=%v", committed, err)
			}
			after, err = repo.CaseRepo().Get(ctx, job.CaseID)
			if err != nil || after.Status != entity.CaseStatusDraft || after.ExecutionGeneration != second.ExecutionGeneration {
				t.Fatalf("current writer Case=%+v err=%v", after, err)
			}
			events, err = repo.EventRepo().ListByCase(ctx, job.CaseID)
			want := 2
			if reset {
				want = 1
			}
			if err != nil || len(events) != want {
				t.Fatalf("events=%v err=%v want=%d", events, err, want)
			}
		})
	}
}
