package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestGoldenRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.GoldenCaseModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewGoldenRepository(db)
	ctx := context.Background()
	if err := repo.Create(ctx, &entity.GoldenCase{
		ID: "golden-1", CaseID: "case-1", Question: "ship?",
		ExpectedDecision: entity.VoteDecisionApprove,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := repo.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if list[0].ExpectedDecision != entity.VoteDecisionApprove {
		t.Fatalf("roundtrip = %+v", list[0])
	}
	if err := repo.Delete(ctx, "golden-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = repo.List(ctx)
	if len(list) != 0 {
		t.Fatalf("expected empty after delete, got %d", len(list))
	}
}
