package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDecisionJobRepository_LifecycleAndRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.DecisionJobModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewDecisionJobRepository(db)
	job, err := repo.Enqueue(context.Background(), "case-1", 2)
	if err != nil || job.Status != entity.DecisionJobQueued {
		t.Fatalf("enqueue: job=%+v err=%v", job, err)
	}
	lease := time.Now().Add(time.Minute)
	claimed, ok, err := repo.Claim(context.Background(), job.ID, "worker-1", lease)
	if err != nil || !ok || claimed.Attempt != 1 || claimed.Status != entity.DecisionJobRunning {
		t.Fatalf("claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := repo.Heartbeat(context.Background(), job.ID, "worker-1", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	retryAt := time.Now().Add(-time.Second)
	if err := repo.MarkFailed(context.Background(), job.ID, "worker-1", "transient", &retryAt); err != nil {
		t.Fatalf("mark retry: %v", err)
	}
	runnable, err := repo.ListRunnable(context.Background(), time.Now())
	if err != nil || len(runnable) != 1 || runnable[0].LastError != "transient" {
		t.Fatalf("runnable retry: jobs=%+v err=%v", runnable, err)
	}
	claimed, ok, err = repo.Claim(context.Background(), job.ID, "worker-2", lease)
	if err != nil || !ok || claimed.Attempt != 2 {
		t.Fatalf("second claim: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if err := repo.MarkSucceeded(context.Background(), job.ID, "worker-2"); err != nil {
		t.Fatalf("succeed: %v", err)
	}
	final, err := repo.GetByCase(context.Background(), "case-1")
	if err != nil || final.Status != entity.DecisionJobSucceeded {
		t.Fatalf("final: job=%+v err=%v", final, err)
	}
}
