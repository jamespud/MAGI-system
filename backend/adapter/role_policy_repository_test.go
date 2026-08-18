package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestRolePolicyRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.RolePolicyModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRolePolicyRepository(db)
	ctx := context.Background()
	if got, _ := repo.Get(ctx, "melchior"); got != nil {
		t.Fatalf("expected no stored spec, got %+v", got)
	}
	policy := entity.RolePolicy{
		Role: "melchior", EnforceAssessment: true,
		RequiredAssessment: entity.RoleAssessmentTechnical, MinTechnicalScore: 75,
	}
	if err := repo.Save(ctx, "melchior", policy); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := repo.Get(ctx, "melchior")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.MinTechnicalScore != 75 || got.RequiredAssessment != entity.RoleAssessmentTechnical {
		t.Fatalf("roundtrip = %+v", got)
	}
}
