package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestSelfImproveRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.SelfImproveModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewSelfImproveRepository(db)
	ctx := context.Background()
	now := time.Now()
	s := &entity.SelfImproveSuggestion{
		ID: "selfimp-1", CaseID: "case-1", RunID: "run-1", AgentCode: "melchior",
		Category: entity.SelfImproveGateFailure, Failure: "gate", Summary: "s",
		SuggestedRule: "rule", PromptKey: "agent.workflow_tools", PromptContent: "content",
		Status: entity.SelfImproveOpen, CreatedAt: now,
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(ctx, "selfimp-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Category != entity.SelfImproveGateFailure || got.PromptContent != "content" {
		t.Fatalf("roundtrip = %+v", got)
	}
	if err := repo.UpdateStatus(ctx, "selfimp-1", entity.SelfImproveApplied); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = repo.Get(ctx, "selfimp-1")
	if got.Status != entity.SelfImproveApplied || got.AppliedAt == nil {
		t.Fatalf("applied = %+v", got)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
}
