package dataset_test

import (
	"context"
	"testing"
	"time"

	"github.com/jamespud/magi/backend/application/dataset"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestService_ResumeSkipsCompletedItems(t *testing.T) {
	repo := newStubDatasetRepo()
	orch := &stubOrch{}
	svc := dataset.NewService(repo, &stubCaseRepo{}, orch, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "resume", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "q1", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "q2", ExpectedDecision: entity.VoteDecisionApprove},
	})
	items, _ := repo.ListItems(ctx, d.ID)
	run := &entity.BenchmarkRun{ID: "bench-resume", DatasetID: d.ID, Status: entity.BenchmarkRunRunning, Total: 2, RunsPerItem: 1, StartedAt: time.Now(), CreatedAt: time.Now()}
	_ = repo.CreateRun(ctx, run)
	// Item 1 already completed before the "restart".
	_ = repo.CreateItemResult(ctx, &entity.BenchmarkItemResult{
		RunID: run.ID, DatasetItemID: items[0].ID, CaseID: "case-done",
		ExpectedDecision: entity.VoteDecisionApprove, ActualDecision: entity.VoteDecisionApprove,
		Matched: true, Runs: 1, Consistency: 1, Decisions: []entity.VoteDecision{entity.VoteDecisionApprove},
	})
	if err := svc.RecoverOrphanRuns(ctx); err != nil {
		t.Fatalf("recover: %v", err)
	}
	run = waitRun(t, repo, run.ID, entity.BenchmarkRunSucceeded)
	if run.Accuracy != 1 || run.Stability != 1 || run.Matched != 2 {
		t.Fatalf("resumed run: %+v", run)
	}
	orch.mu.Lock()
	calls := orch.call
	orch.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected only the missing item to run, orchestrator calls=%d", calls)
	}
}

func TestService_ItemCRUDAndExport(t *testing.T) {
	repo := newStubDatasetRepo()
	svc := dataset.NewService(repo, &stubCaseRepo{}, &stubOrch{}, 1)
	ctx := context.Background()
	d, _ := svc.Create(ctx, 0, "crud", "")
	_, _ = svc.AddItems(ctx, 0, d.ID, []dataset.NewItem{
		{Question: "a?", ExpectedDecision: entity.VoteDecisionApprove},
		{Question: "b?", ExpectedDecision: entity.VoteDecisionReject, Weight: 2},
	})
	items, _ := svc.ListItems(ctx, 0, d.ID)
	if len(items) != 2 {
		t.Fatalf("items: %d", len(items))
	}
	if err := svc.UpdateItem(ctx, 0, d.ID, items[0].ID, dataset.NewItem{
		Question: "a-updated?", ExpectedDecision: entity.VoteDecisionReject,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, _ := repo.GetItem(ctx, items[0].ID)
	if updated.Question != "a-updated?" || updated.ExpectedDecision != entity.VoteDecisionReject {
		t.Fatalf("updated: %+v", updated)
	}
	if err := svc.DeleteItem(ctx, 0, d.ID, items[1].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := repo.GetDataset(ctx, d.ID)
	if got.ItemCount != 1 {
		t.Fatalf("item count after delete: %d", got.ItemCount)
	}
	exported, _ := svc.ExportItems(ctx, 0, d.ID)
	if len(exported) != 1 || exported[0].Question != "a-updated?" {
		t.Fatalf("export: %+v", exported)
	}
	if err := svc.DeleteItem(ctx, 0, d.ID, "missing"); err == nil {
		t.Fatal("deleting unknown item should fail")
	}
}
