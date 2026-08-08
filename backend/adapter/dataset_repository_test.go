package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openDatasetDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestDatasetRepository_RoundTrip(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewDatasetRepository(db)
	ctx := context.Background()

	ds := &entity.BenchmarkDataset{Name: "launch-eval", Description: "launch decisions"}
	if err := repo.CreateDataset(ctx, ds); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if ds.ID == "" {
		t.Fatal("dataset id not assigned")
	}
	got, err := repo.GetDataset(ctx, ds.ID)
	if err != nil || got == nil || got.Name != "launch-eval" {
		t.Fatalf("get dataset: %v %+v", err, got)
	}

	items := []*entity.BenchmarkItem{
		{DatasetID: ds.ID, Question: "ship A?", ExpectedDecision: entity.VoteDecisionApprove, Weight: 2},
		{DatasetID: ds.ID, Question: "ship B?", ExpectedDecision: entity.VoteDecisionReject},
	}
	if err := repo.CreateItems(ctx, items); err != nil {
		t.Fatalf("create items: %v", err)
	}
	listed, err := repo.ListItems(ctx, ds.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list items: %v len=%d", err, len(listed))
	}

	run := &entity.BenchmarkRun{DatasetID: ds.ID, Status: entity.BenchmarkRunRunning, Total: 2}
	if err := repo.CreateRun(ctx, run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	run.Matched = 1
	run.Accuracy = 0.5
	run.Status = entity.BenchmarkRunSucceeded
	if err := repo.UpdateRun(ctx, run); err != nil {
		t.Fatalf("update run: %v", err)
	}
	runGot, err := repo.GetRun(ctx, run.ID)
	if err != nil || runGot.Status != entity.BenchmarkRunSucceeded || runGot.Accuracy != 0.5 {
		t.Fatalf("get run: %v %+v", err, runGot)
	}

	if err := repo.CreateItemResult(ctx, &entity.BenchmarkItemResult{RunID: run.ID, DatasetItemID: listed[0].ID, ExpectedDecision: entity.VoteDecisionApprove, ActualDecision: entity.VoteDecisionApprove, Matched: true, Score: 2}); err != nil {
		t.Fatalf("create result: %v", err)
	}
	results, err := repo.ListItemResults(ctx, run.ID)
	if err != nil || len(results) != 1 || !results[0].Matched {
		t.Fatalf("list results: %v %+v", err, results)
	}

	runs, err := repo.ListRuns(ctx, ds.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs: %v %d", err, len(runs))
	}
}

func TestDatasetRepository_FeedbackAndResolutionEvaluation(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	res := &entity.Resolution{
		ID: "res-1", CaseID: "c1", FinalDecision: entity.VoteDecisionApprove,
		Evaluation: &entity.Evaluation{ConsensusRound: 1, TotalTokens: 42, FirstRoundConsensus: true},
	}
	if err := repo.ResolutionRepo().Create(ctx, res); err != nil {
		t.Fatalf("create resolution: %v", err)
	}
	got, err := repo.ResolutionRepo().Get(ctx, "c1")
	if err != nil || got == nil || got.Evaluation == nil || got.Evaluation.TotalTokens != 42 || !got.Evaluation.FirstRoundConsensus {
		t.Fatalf("resolution evaluation: %v %+v", err, got)
	}

	dr := magi.NewDatasetRepository(db)
	ds := &entity.BenchmarkDataset{Name: "eval"}
	_ = dr.CreateDataset(ctx, ds)
	run := &entity.BenchmarkRun{DatasetID: ds.ID, Status: entity.BenchmarkRunSucceeded, Total: 1}
	_ = dr.CreateRun(ctx, run)
	resItem := &entity.BenchmarkItemResult{RunID: run.ID, Matched: true, Score: 1}
	_ = dr.CreateItemResult(ctx, resItem)
	at := time.Now()
	if err := dr.UpdateFeedback(ctx, resItem.ID, "agree with the call", at); err != nil {
		t.Fatalf("update feedback: %v", err)
	}
	items, _ := dr.ListItemResults(ctx, run.ID)
	if len(items) != 1 || items[0].Feedback != "agree with the call" || items[0].FeedbackAt == nil {
		t.Fatalf("feedback roundtrip: %+v", items)
	}
}
