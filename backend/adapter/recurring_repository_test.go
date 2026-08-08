package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestRecurringRepository_RoundTrip(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRecurringRepository(db)
	ctx := context.Background()

	rc := &entity.RecurringCase{UserID: 7, Name: "daily", Question: "q?", Interval: time.Minute, Enabled: true}
	if err := repo.Create(ctx, rc); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(ctx, rc.ID)
	if err != nil || got == nil || got.Interval != time.Minute {
		t.Fatalf("get: %v %+v", err, got)
	}
	enabled, err := repo.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 {
		t.Fatalf("list enabled: %v %d", err, len(enabled))
	}
	now := time.Now()
	if err := repo.UpdateLastRun(ctx, rc.ID, now); err != nil {
		t.Fatalf("update last run: %v", err)
	}
	got, _ = repo.Get(ctx, rc.ID)
	if got.LastRunAt == nil || !got.LastRunAt.Equal(now) {
		t.Fatalf("last run: %+v", got.LastRunAt)
	}
	if err := repo.UpdateEnabled(ctx, rc.ID, false); err != nil {
		t.Fatalf("update enabled: %v", err)
	}
	enabled, _ = repo.ListEnabled(ctx)
	if len(enabled) != 0 {
		t.Fatalf("expected no enabled after disable, got %d", len(enabled))
	}
	if err := repo.Delete(ctx, rc.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, rc.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}
