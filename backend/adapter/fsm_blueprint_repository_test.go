package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestFSMBlueprintRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.FSMBlueprintModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewFSMBlueprintRepository(db)
	ctx := context.Background()
	if got, _ := repo.Get(ctx); got != nil {
		t.Fatalf("expected no stored blueprint, got %+v", got)
	}
	blueprint := entity.FSMBlueprint{Transitions: []entity.StateTransition{
		{From: "DRAFT", To: "NORMALIZING"},
	}}
	if err := repo.Save(ctx, blueprint); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Transitions) != 1 || got.Transitions[0].To != "NORMALIZING" {
		t.Fatalf("roundtrip = %+v", got)
	}
}
