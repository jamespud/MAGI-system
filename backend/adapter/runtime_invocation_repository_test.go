package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openRuntimeInvocationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedRuntimeInvocation(t *testing.T, db *gorm.DB, id, idempotencyKey string) {
	t.Helper()
	if err := db.Create(&magi.RuntimeInvocationModel{
		InvocationID: id,
		RunID:        "run-1",
		StepID:       "step-1",
		Kind:         "tool",
		Status:       string(entity.InvocationPending),
		InputJSON:    `{"input":"value"}`,
		IdempotencyKey: func() *string {
			if idempotencyKey == "" {
				return nil
			}
			return &idempotencyKey
		}(),
	}).Error; err != nil {
		t.Fatalf("seed invocation: %v", err)
	}
}

func TestRuntimeInvocationRepository(t *testing.T) {
	t.Run("BeginIsSingleWinner", TestInvocationRepository_BeginIsSingleWinner)
	t.Run("CompletedResultIsReusable", TestInvocationRepository_CompletedResultIsReusable)
	t.Run("LateAttemptCannotOverwriteNewAttempt", TestInvocationRepository_LateAttemptCannotOverwriteNewAttempt)
	t.Run("IdempotencyKeyUnique", TestInvocationRepository_IdempotencyKeyUnique)
}

func TestInvocationRepository_BeginIsSingleWinner(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-1", "idem-1")
	repo := magi.NewRuntimeInvocationRepository(db)

	first, won, err := repo.BeginAttempt(context.Background(), "invocation-1", "attempt-1")
	if err != nil || !won {
		t.Fatalf("first begin: invocation=%+v won=%v err=%v", first, won, err)
	}
	if first.Status != entity.InvocationRunning || first.AttemptCount != 1 {
		t.Fatalf("first begin state: %+v", first)
	}

	second, won, err := repo.BeginAttempt(context.Background(), "invocation-1", "attempt-2")
	if err != nil || won {
		t.Fatalf("second begin: invocation=%+v won=%v err=%v", second, won, err)
	}
	if second.Status != entity.InvocationRunning || second.AttemptCount != 1 {
		t.Fatalf("second begin changed invocation: %+v", second)
	}

	var attempts int64
	if err := db.Model(&magi.RuntimeInvocationAttemptModel{}).Where("invocation_id = ?", "invocation-1").Count(&attempts).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempt rows = %d, want 1", attempts)
	}
}

func TestInvocationRepository_CompletedResultIsReusable(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-completed", "idem-completed")
	repo := magi.NewRuntimeInvocationRepository(db)
	ctx := context.Background()

	if _, won, err := repo.BeginAttempt(ctx, "invocation-completed", "attempt-1"); err != nil || !won {
		t.Fatalf("begin: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-completed", "attempt-1", `{"answer":42}`); err != nil || !won {
		t.Fatalf("complete: won=%v err=%v", won, err)
	}

	got, won, err := repo.BeginAttempt(ctx, "invocation-completed", "attempt-2")
	if err != nil || won {
		t.Fatalf("replay begin: invocation=%+v won=%v err=%v", got, won, err)
	}
	if got.Status != entity.InvocationSucceeded || got.OutputJSON != `{"answer":42}` || got.AttemptCount != 1 {
		t.Fatalf("completed result was not reusable: %+v", got)
	}
}

func TestInvocationRepository_LateAttemptCannotOverwriteNewAttempt(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-late", "idem-late")
	repo := magi.NewRuntimeInvocationRepository(db)
	ctx := context.Background()

	if _, won, err := repo.BeginAttempt(ctx, "invocation-late", "attempt-old"); err != nil || !won {
		t.Fatalf("begin old: won=%v err=%v", won, err)
	}
	if won, err := repo.Fail(ctx, "invocation-late", "attempt-old", "retryable"); err != nil || !won {
		t.Fatalf("fail old: won=%v err=%v", won, err)
	}
	if _, won, err := repo.BeginAttempt(ctx, "invocation-late", "attempt-new"); err != nil || !won {
		t.Fatalf("begin new: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-late", "attempt-old", `{"answer":"stale"}`); err != nil || won {
		t.Fatalf("late complete: won=%v err=%v", won, err)
	}
	if won, err := repo.Complete(ctx, "invocation-late", "attempt-new", `{"answer":"fresh"}`); err != nil || !won {
		t.Fatalf("complete new: won=%v err=%v", won, err)
	}

	got, err := repo.Get(ctx, "invocation-late")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != entity.InvocationSucceeded || got.OutputJSON != `{"answer":"fresh"}` || got.AttemptCount != 2 {
		t.Fatalf("late attempt overwrote newer result: %+v", got)
	}
}

func TestInvocationRepository_IdempotencyKeyUnique(t *testing.T) {
	db := openRuntimeInvocationDB(t)
	seedRuntimeInvocation(t, db, "invocation-idempotency-1", "idem-unique")

	key := "idem-unique"
	err := db.Create(&magi.RuntimeInvocationModel{
		InvocationID:   "invocation-idempotency-2",
		RunID:          "run-1",
		StepID:         "step-2",
		Kind:           "tool",
		Status:         string(entity.InvocationPending),
		InputJSON:      `{}`,
		IdempotencyKey: &key,
	}).Error
	if err == nil {
		t.Fatal("duplicate idempotency key was accepted")
	}
}
