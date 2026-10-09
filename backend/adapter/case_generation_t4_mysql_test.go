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
			first, owner1 := claimArtifactOwner(t, jobs, job, "same-worker")
			if err := repo.CaseRepo().UpdateStatus(ctx, job.CaseID, entity.CaseStatusInvestigating); err != nil {
				t.Fatal(err)
			}
			requeueArtifactOwner(t, jobs, first, "same-worker")
			second, owner2 := claimArtifactOwner(t, jobs, job, "same-worker")
			// Real ABA: move away from S and return to S under the next claim.
			if err := repo.CaseRepo().UpdateStatus(ctx, job.CaseID, entity.CaseStatusDraft); err != nil {
				t.Fatal(err)
			}
			if err := repo.CaseRepo().UpdateStatus(ctx, job.CaseID, entity.CaseStatusInvestigating); err != nil {
				t.Fatal(err)
			}
			owned := t4Committer(repo)
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
			if err != nil || len(events) != 0 || event.Seq != originalSeq {
				t.Fatalf("rejected event=%+v events=%v err=%v", event, events, err)
			}
			var cursorCount int64
			if err := db.Table("magi_event_cursor").Where("case_id = ?", job.CaseID).Count(&cursorCount).Error; err != nil || cursorCount != 0 {
				t.Fatalf("rejected cursor count=%d err=%v", cursorCount, err)
			}
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
			want := 1
			if reset {
				want = 0
			}
			if err != nil || len(events) != want {
				t.Fatalf("events=%v err=%v want=%d", events, err, want)
			}
		})
	}
}
