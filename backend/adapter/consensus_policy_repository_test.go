package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/consensus"
)

func TestConsensusPolicyRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.ConsensusPolicyModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewConsensusPolicyRepository(db)
	ctx := context.Background()
	if got, _ := repo.Get(ctx); got != nil {
		t.Fatalf("expected no stored policy, got %+v", got)
	}
	policy := consensus.ConsensusPolicy{Quorum: 3, FirstSplitGoesToDebate: false}
	if err := repo.Save(ctx, policy); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.Get(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Quorum != 3 || got.FirstSplitGoesToDebate {
		t.Fatalf("roundtrip = %+v", got)
	}
}
