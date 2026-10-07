package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestMySQLArtifactGeneration_StaleWriterFencedAndReadsIsolated(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-t3-artifact-fence", 3)
	owned, ok := repo.(port.OwnedArtifactRepository)
	if !ok {
		t.Fatal("repository missing OwnedArtifactRepository")
	}
	ctx := context.Background()
	worker := "worker-t3"
	first, claimed, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim gen1: job=%+v claimed=%v err=%v", first, claimed, err)
	}
	owner1 := &entity.ExecutionContext{
		CaseID: job.CaseID, JobID: job.ID, WorkerID: worker,
		JobAttempt: first.Attempt, ExecutionGeneration: first.ExecutionGeneration,
	}
	ev1 := &entity.EvidenceRecord{ID: "ev-g1", CaseID: job.CaseID, ExecutionGeneration: first.ExecutionGeneration}
	if err := owned.CreateEvidenceOwned(ctx, owner1, ev1); err != nil {
		t.Fatalf("write gen1 evidence: %v", err)
	}

	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(ctx, job.ID, job.CaseID, worker, first.ExecutionGeneration, "retry", &retryAt); err != nil {
		t.Fatalf("requeue gen1: %v", err)
	}
	second, claimed, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !claimed || second.ExecutionGeneration != first.ExecutionGeneration+1 {
		t.Fatalf("claim gen2: job=%+v claimed=%v err=%v", second, claimed, err)
	}
	owner2 := &entity.ExecutionContext{
		CaseID: job.CaseID, JobID: job.ID, WorkerID: worker,
		JobAttempt: second.Attempt, ExecutionGeneration: second.ExecutionGeneration,
	}

	stale := &entity.EvidenceRecord{ID: "ev-late-g1", CaseID: job.CaseID, ExecutionGeneration: first.ExecutionGeneration}
	if err := owned.CreateEvidenceOwned(ctx, owner1, stale); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("late gen1 write error = %v, want ErrLeaseLost", err)
	}
	var staleRows int64
	if err := db.Model(&magi.EvidenceModel{}).Where("id = ?", stale.ID).Count(&staleRows).Error; err != nil {
		t.Fatalf("count stale evidence: %v", err)
	}
	if staleRows != 0 {
		t.Fatalf("late gen1 evidence rows = %d, want 0", staleRows)
	}

	ev2 := &entity.EvidenceRecord{ID: "ev-g2", CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration}
	if err := owned.CreateEvidenceOwned(ctx, owner2, ev2); err != nil {
		t.Fatalf("write gen2 evidence: %v", err)
	}
	gen1, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, first.ExecutionGeneration)
	if err != nil || len(gen1) != 1 || gen1[0].ID != ev1.ID {
		t.Fatalf("gen1 read = %+v err=%v", gen1, err)
	}
	gen2, err := owned.ListEvidenceByGeneration(ctx, job.CaseID, second.ExecutionGeneration)
	if err != nil || len(gen2) != 1 || gen2[0].ID != ev2.ID {
		t.Fatalf("gen2 read = %+v err=%v", gen2, err)
	}
	history, err := repo.EvidenceRepo().ListByCase(ctx, job.CaseID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history read = %+v err=%v", history, err)
	}
}

func TestMySQLArtifactGeneration_CheckpointDoesNotCrossGeneration(t *testing.T) {
	db := provideDBForTest(t, newMySQLSchema(t))
	repo, jobs, job := seedMySQLDecisionJob(t, db, "case-t3-checkpoint-fence", 3)
	checkpoints, ok := repo.CheckpointRepo().(port.GenerationCheckpointRepository)
	if !ok {
		t.Fatal("checkpoint repository missing GenerationCheckpointRepository")
	}
	ctx := context.Background()
	worker := "worker-t3-checkpoint"
	first, claimed, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim gen1: job=%+v claimed=%v err=%v", first, claimed, err)
	}
	owner1 := &entity.ExecutionContext{
		CaseID: job.CaseID, JobID: job.ID, WorkerID: worker,
		JobAttempt: first.Attempt, ExecutionGeneration: first.ExecutionGeneration,
	}
	const runID = "logical-agent-run"
	state1 := &entity.AgentState{
		RunID: runID, CaseID: job.CaseID, ExecutionGeneration: first.ExecutionGeneration,
		StepCount: 3, Phase: "gather",
	}
	if err := checkpoints.SaveForExecution(ctx, owner1, state1); err != nil {
		t.Fatalf("save gen1 checkpoint: %v", err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := jobs.MarkFailed(ctx, job.ID, job.CaseID, worker, first.ExecutionGeneration, "retry", &retryAt); err != nil {
		t.Fatalf("requeue gen1: %v", err)
	}
	second, claimed, err := jobs.Claim(ctx, job.ID, worker, uuid.NewString(), time.Now().Add(time.Minute))
	if err != nil || !claimed {
		t.Fatalf("claim gen2: job=%+v claimed=%v err=%v", second, claimed, err)
	}
	owner2 := &entity.ExecutionContext{
		CaseID: job.CaseID, JobID: job.ID, WorkerID: worker,
		JobAttempt: second.Attempt, ExecutionGeneration: second.ExecutionGeneration,
	}
	if got, err := checkpoints.LoadForExecution(ctx, owner2, runID); err != nil || got != nil {
		t.Fatalf("gen2 implicitly loaded gen1 checkpoint: got=%+v err=%v", got, err)
	}
	state1.StepCount = 4
	if err := checkpoints.SaveForExecution(ctx, owner1, state1); !errors.Is(err, port.ErrLeaseLost) {
		t.Fatalf("late gen1 checkpoint save = %v, want ErrLeaseLost", err)
	}
	state2 := &entity.AgentState{
		RunID: runID, CaseID: job.CaseID, ExecutionGeneration: second.ExecutionGeneration,
		StepCount: 1, Phase: "gather",
	}
	if err := checkpoints.SaveForExecution(ctx, owner2, state2); err != nil {
		t.Fatalf("save gen2 checkpoint: %v", err)
	}
	got2, err := checkpoints.LoadForExecution(ctx, owner2, runID)
	if err != nil || got2 == nil || got2.StepCount != 1 || got2.ExecutionGeneration != second.ExecutionGeneration {
		t.Fatalf("load gen2 checkpoint = %+v err=%v", got2, err)
	}
	var rows int64
	if err := db.Model(&magi.CheckpointModel{}).Where("run_id = ?", runID).Count(&rows).Error; err != nil {
		t.Fatalf("count checkpoint generations: %v", err)
	}
	if rows != 2 {
		t.Fatalf("checkpoint generations = %d, want 2", rows)
	}
}
