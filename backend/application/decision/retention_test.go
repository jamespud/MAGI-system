package decision_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jamespud/magi/backend/internal/testwriter"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/application/decision"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type retainingRetryOrchestrator struct{ repo port.Repository }

func (o *retainingRetryOrchestrator) Orchestrate(context.Context, *entity.DecisionCase) (*entity.Resolution, error) {
	return nil, port.ErrLeaseLost
}

func (o *retainingRetryOrchestrator) OrchestrateForExecution(ctx context.Context, c *entity.DecisionCase, owner *entity.ExecutionContext) (*entity.Resolution, error) {
	artifacts := o.repo.(port.OwnedArtifactRepository)
	evidence := &entity.EvidenceRecord{ID: fmt.Sprintf("evidence-g%d", owner.ExecutionGeneration), CaseID: c.ID, CreatedAt: time.Now()}
	if err := artifacts.CreateEvidenceOwned(ctx, owner, evidence); err != nil {
		return nil, err
	}
	if owner.ExecutionGeneration == 1 {
		return nil, errors.New("retry with previous evidence retained")
	}
	current, err := artifacts.ListEvidenceByGeneration(ctx, c.ID, owner.ExecutionGeneration)
	if err != nil || len(current) != 1 || current[0].ID != evidence.ID {
		return nil, fmt.Errorf("retry read previous generation: %+v, %v", current, err)
	}
	res := &entity.Resolution{ID: "retention-resolution", CaseID: c.ID, ExecutionGeneration: owner.ExecutionGeneration, KeyEvidenceIDs: []string{evidence.ID}, CreatedAt: time.Now()}
	event := entity.NewEvent(c.ID, "", nil, entity.EventCaseCompleted, nil)
	event.ExecutionGeneration = owner.ExecutionGeneration
	ok, err := o.repo.(port.OwnedCaseCommitter).CommitTerminalOwned(ctx, owner, c.Status, entity.CaseStatusResolved, res, &event)
	if err != nil || !ok {
		return nil, fmt.Errorf("retry terminal commit: %v, %w", ok, err)
	}
	c.Status = entity.CaseStatusResolved
	return res, nil
}

func TestRunManager_RetryRetainsPreviousGenerationEvidence(t *testing.T) {
	ctx := context.Background()
	db := openJobDB(t)
	if err := db.AutoMigrate(&magi.EvidenceModel{}, &magi.VoteModel{}, &magi.ClaimModel{}, &magi.ResolutionModel{}); err != nil {
		t.Fatal(err)
	}
	repo := magi.NewRepository(db)
	c := &entity.DecisionCase{ID: "case-retention", Status: entity.CaseStatusDraft}
	if err := repo.CaseRepo().Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	testwriter.Enable(t, db)
	jobs := magi.NewDecisionJobRepository(db)
	manager := decision.NewRunManager(&retainingRetryOrchestrator{repo: repo}, decision.RunManagerDeps{
		JobRepo: jobs, CaseRepo: repo.CaseRepo(), OwnedCases: repo.(port.OwnedCaseCommitter),
		WorkerID: "retention-worker", MaxAttempts: 2, RetryBase: time.Millisecond,
	})
	t.Cleanup(manager.Shutdown)
	if err := manager.Start(ctx, c); err != nil {
		t.Fatal(err)
	}
	if !manager.WaitStopped(c.ID, 5*time.Second) {
		t.Fatal("retry did not stop")
	}
	job, err := jobs.GetByCase(ctx, c.ID)
	if err != nil || job.Status != entity.DecisionJobSucceeded || job.Attempt != 2 || job.ExecutionGeneration != 2 {
		t.Fatalf("retry Job=%+v err=%v", job, err)
	}
	history, err := repo.EvidenceRepo().ListByCase(ctx, c.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	for _, evidence := range history {
		if evidence.ID != fmt.Sprintf("evidence-g%d", evidence.ExecutionGeneration) || evidence.ExecutionGeneration < 1 || evidence.ExecutionGeneration > 2 {
			t.Fatalf("history lost provenance: %+v", evidence)
		}
	}
	resolution, err := repo.ResolutionRepo().Get(ctx, c.ID)
	if err != nil || resolution.ExecutionGeneration != 2 || len(resolution.KeyEvidenceIDs) != 1 || resolution.KeyEvidenceIDs[0] != "evidence-g2" {
		t.Fatalf("resolution=%+v err=%v", resolution, err)
	}
}
