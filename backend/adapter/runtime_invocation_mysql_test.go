package magi_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"

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

	// Unique per run: a fixed ID left the row behind, so a second run of this
	// test saw a stale "running" invocation and began no attempt at all — the
	// same re-runnability defect fixed for the admission test in 670896f.
	caseID := "case-" + uuid.NewString()                      // 41 chars, same shape as case-<uuid>
	longRunID := caseID + "-balthasar-a1-r1-investigate"      // 69
	longAttemptID := caseID + "-casper-r1-investigate-retry1" // 70
	sum := sha256.Sum256([]byte(caseID))
	invocationID := hex.EncodeToString(sum[:]) // 64 chars, the invocation_id column width

	t.Cleanup(func() {
		db.Exec("DELETE FROM runtime_invocation_attempt WHERE invocation_id = ?", invocationID)
		db.Exec("DELETE FROM runtime_invocation WHERE invocation_id = ?", invocationID)
	})

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
