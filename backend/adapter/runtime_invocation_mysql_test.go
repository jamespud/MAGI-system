package magi_test

import (
	"context"
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

// Production run IDs are built from the case UUID plus role/attempt/round/phase
// and legitimately exceed 64 characters; the columns used to be varchar(64).
func TestRuntimeInvocation_LongIDsOnMySQL(t *testing.T) {
	db := openA2AMySQL(t)
	if err := db.AutoMigrate(&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewRuntimeInvocationRepository(db)
	ctx := context.Background()

	caseID := "case-" + strings.Repeat("a", 36)               // 41 chars, same shape as case-<uuid>
	longRunID := caseID + "-balthasar-a1-r1-investigate"      // 69
	longAttemptID := caseID + "-casper-r1-investigate-retry1" // 70
	invocationID := strings.Repeat("d", 64)

	if _, err := repo.Ensure(ctx, &entity.RuntimeInvocation{
		InvocationID: invocationID, RunID: longRunID, StepID: strings.Repeat("e", 64),
		Kind: "model", Status: entity.InvocationPending, RetrySafety: "unsafe",
		InputDigest: strings.Repeat("f", 64), InputJSON: "{}",
	}); err != nil {
		t.Fatalf("ensure with %d-char run id: %v", len(longRunID), err)
	}

	// The dispatcher retry path uses the full run ID as the physical attempt ID.
	inv, won, err := repo.BeginAttempt(ctx, invocationID, longAttemptID)
	if err != nil {
		t.Fatalf("begin attempt with %d-char attempt id: %v", len(longAttemptID), err)
	}
	if !won || inv == nil || inv.Status != entity.InvocationRunning {
		t.Fatalf("begin attempt = %+v won=%v", inv, won)
	}
	if stored, err := repo.Get(ctx, invocationID); err != nil || stored.RunID != longRunID {
		t.Fatalf("stored run id = %q err=%v, want %q", stored.RunID, err, longRunID)
	}
}
