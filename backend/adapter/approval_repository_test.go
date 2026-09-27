package magi_test

import (
	"context"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openApprovalDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.ApprovalModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestApprovalRepository_Lifecycle(t *testing.T) {
	repo := magi.NewApprovalRepository(openApprovalDB(t))
	ctx := context.Background()
	a := &entity.ApprovalRequest{
		CaseID: "c1", RunID: "r1", ToolName: "code_runner", Arguments: `{}`,
		IntentDigest: "digest-code-runner", Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected generated ID")
	}
	found, err := repo.FindByKey(ctx, "c1", "r1", "code_runner", "digest-code-runner")
	if err != nil || found == nil || found.ID != a.ID {
		t.Fatalf("find by key: %v %+v", err, found)
	}
	if err := repo.Approve(ctx, a.ID, "human-1", "ok"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, _ := repo.Get(ctx, a.ID)
	if got.Status != entity.ApprovalApproved || got.DecidedBy != "human-1" {
		t.Fatalf("approved state: %+v", got)
	}
	if err := repo.Approve(ctx, a.ID, "human-2", "again"); err == nil {
		t.Fatal("second approve should fail")
	}
	list, err := repo.List(ctx, "c1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
}

func TestApprovalRepository_Expire(t *testing.T) {
	repo := magi.NewApprovalRepository(openApprovalDB(t))
	ctx := context.Background()
	a := &entity.ApprovalRequest{CaseID: "c2", RunID: "r2", ToolName: "calc", Status: entity.ApprovalPending, RequestedAt: time.Now()}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.MarkExpired(ctx, a.ID); err != nil {
		t.Fatalf("expire: %v", err)
	}
	got, _ := repo.Get(ctx, a.ID)
	if got.Status != entity.ApprovalExpired {
		t.Fatalf("state: %+v", got)
	}
}

// Issue #9: the lookup key must include the canonical intent digest, and a row
// persisted without one (a legacy row) must never be reused.
func TestApprovalRepository_FindByKey_RequiresExactIntentDigest(t *testing.T) {
	repo := magi.NewApprovalRepository(openApprovalDB(t))
	ctx := context.Background()

	approved := &entity.ApprovalRequest{
		CaseID: "c3", RunID: "r3", ToolName: "rollout", Arguments: `{"percent":5}`,
		IntentDigest: "digest-percent-5", Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, approved); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The same tool with a different argument set must not resolve to the earlier row.
	got, err := repo.FindByKey(ctx, "c3", "r3", "rollout", "digest-percent-10")
	if err != nil || got != nil {
		t.Fatalf("different intent must not reuse the earlier approval: got=%+v err=%v", got, err)
	}

	// The exact same intent still resolves, so a safe retry keeps reusing its decision.
	got, err = repo.FindByKey(ctx, "c3", "r3", "rollout", "digest-percent-5")
	if err != nil || got == nil || got.ID != approved.ID {
		t.Fatalf("exact intent lookup: got=%+v err=%v", got, err)
	}

	// A row stored before intent digests existed has an empty digest and must
	// stay fail-closed: an empty lookup key can never match it.
	legacy := &entity.ApprovalRequest{
		CaseID: "c4", RunID: "r4", ToolName: "legacy", Arguments: `{}`,
		Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, legacy); err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if got, err := repo.FindByKey(ctx, "c4", "r4", "legacy", ""); err != nil || got != nil {
		t.Fatalf("legacy row without a digest must never be reused: got=%+v err=%v", got, err)
	}
}
