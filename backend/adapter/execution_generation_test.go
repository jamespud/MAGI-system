package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

// T1 (#19): the Case generation must survive the persistence boundary. Before
// this plumbing existed, Create/Get silently flattened a non-zero generation to
// 0. This locks both caseToModel and caseFromModel.
func TestCaseRepository_ExecutionGenerationRoundTrip(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	want := &entity.DecisionCase{
		ID:                  "case-generation-roundtrip",
		Question:            "q",
		ExecutionGeneration: 7,
	}
	if err := repo.CaseRepo().Create(ctx, want); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.CaseRepo().Get(ctx, want.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ExecutionGeneration != 7 {
		t.Fatalf("generation = %d, want 7", got.ExecutionGeneration)
	}
}

// A negative generation is a programming error, so the storage boundary must
// fail closed: rejecting the call is not enough if the row still lands.
func TestCaseRepository_RejectsNegativeExecutionGeneration(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	err := repo.CaseRepo().Create(ctx, &entity.DecisionCase{
		ID:                  "case-negative-generation",
		Question:            "q",
		ExecutionGeneration: -1,
	})
	if err == nil {
		t.Fatal("negative execution generation must be rejected")
	}
	var count int64
	if err := db.Model(&magi.CaseModel{}).Where("id = ?", "case-negative-generation").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid case was persisted: count=%d", count)
	}
}

// T2 will read the claimed generation from the job, so the persisted linkage
// must be readable through the job repository. T1 only reads it here; advancing
// it belongs to the claim path.
func TestDecisionJobRepository_ExecutionGenerationRoundTrip(t *testing.T) {
	db := openAdmissionDB(t)
	ctx := context.Background()

	const caseID = "case-job-generation"
	if err := db.Create(&magi.CaseModel{ID: caseID}).Error; err != nil {
		t.Fatalf("seed case: %v", err)
	}
	if err := db.Create(&magi.DecisionJobModel{
		ID:                  "job-generation",
		CaseID:              caseID,
		Status:              string(entity.DecisionJobQueued),
		ExecutionGeneration: 7,
	}).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}

	repo := magi.NewDecisionJobRepository(db)
	got, err := repo.GetByCase(ctx, caseID)
	if err != nil {
		t.Fatalf("get by case: %v", err)
	}
	if got.ExecutionGeneration != 7 {
		t.Fatalf("generation = %d, want 7", got.ExecutionGeneration)
	}
}
