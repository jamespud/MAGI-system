package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRagIndexJobRepo(t *testing.T) port.RagIndexJobRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.RagIndexJobModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return magi.NewRagIndexJobRepository(db)
}

func TestRagIndexJobRepo_EnqueueAndListRunnable(t *testing.T) {
	repo := newRagIndexJobRepo(t)
	ctx := context.Background()

	job, err := repo.Enqueue(ctx, entity.RagIndexJobKindIndex, "case_memory", "case-1", 0)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if job.Status != entity.RagIndexJobQueued {
		t.Errorf("status = %q, want queued", job.Status)
	}
	if job.MaxAttempts != 3 {
		t.Errorf("max_attempts = %d, want default 3", job.MaxAttempts)
	}

	runnable, err := repo.ListRunnable(ctx, time.Now().Add(time.Second))
	if err != nil {
		t.Fatalf("list runnable: %v", err)
	}
	if len(runnable) != 1 || runnable[0].ID != job.ID {
		t.Errorf("runnable = %+v, want 1 job %s", runnable, job.ID)
	}
}

func TestRagIndexJobRepo_ClaimOnlyOnce(t *testing.T) {
	repo := newRagIndexJobRepo(t)
	ctx := context.Background()
	job, _ := repo.Enqueue(ctx, entity.RagIndexJobKindDelete, "case_memory", "case-9", 3)
	lease := time.Now().Add(time.Minute)

	claimed1, ok1, _ := repo.Claim(ctx, job.ID, "w1", lease)
	if !ok1 || claimed1.WorkerID != "w1" || claimed1.Attempt != 1 {
		t.Fatalf("first claim: ok=%v worker=%q attempt=%d", ok1, claimed1.WorkerID, claimed1.Attempt)
	}
	_, ok2, _ := repo.Claim(ctx, job.ID, "w2", lease.Add(time.Minute))
	if ok2 {
		t.Error("second claim must fail (already running)")
	}
}

func TestRagIndexJobRepo_RequeueExpired(t *testing.T) {
	repo := newRagIndexJobRepo(t)
	ctx := context.Background()
	job, _ := repo.Enqueue(ctx, entity.RagIndexJobKindIndex, "knowledge_doc", "kd-1", 3)
	_, ok, _ := repo.Claim(ctx, job.ID, "w1", time.Now().Add(-time.Minute))
	if !ok {
		t.Fatal("claim failed")
	}
	if err := repo.RequeueExpired(ctx, time.Now()); err != nil {
		t.Fatalf("requeue expired: %v", err)
	}
	runnable, _ := repo.ListRunnable(ctx, time.Now())
	if len(runnable) != 1 {
		t.Fatalf("runnable = %d, want 1 after requeue", len(runnable))
	}
}

func TestRagIndexJobRepo_MarkFailedRetryBackoff(t *testing.T) {
	repo := newRagIndexJobRepo(t)
	ctx := context.Background()
	job, _ := repo.Enqueue(ctx, entity.RagIndexJobKindIndex, "case_memory", "case-3", 3)
	_, ok, _ := repo.Claim(ctx, job.ID, "w1", time.Now().Add(time.Minute))
	if !ok {
		t.Fatal("claim failed")
	}
	future := time.Now().Add(5 * time.Second)
	if err := repo.MarkFailed(ctx, job.ID, "w1", "boom", &future); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	runnableNow, _ := repo.ListRunnable(ctx, time.Now())
	if len(runnableNow) != 0 {
		t.Errorf("job should not be runnable before retry_at; got %d", len(runnableNow))
	}
	runnableLater, _ := repo.ListRunnable(ctx, future.Add(time.Second))
	if len(runnableLater) != 1 {
		t.Errorf("job should be runnable after retry_at; got %d", len(runnableLater))
	}
}
