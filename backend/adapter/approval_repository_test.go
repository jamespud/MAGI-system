package magi_test

import (
	"context"
	"errors"
	"testing"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
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
		IntentDigest: "digest-code-runner", InvocationID: "inv-lifecycle",
		Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected generated ID")
	}
	found, err := repo.FindByInvocation(ctx, "c1", "inv-lifecycle")
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

// Issue #9 (follow-up): the authoritative approval identity is the logical
// invocation, so a unique constraint and the concurrent-conflict path must make
// "one logical call, one approval request" hold under a race.
func TestApprovalRepository_ScopesUniquenessToTheInvocation(t *testing.T) {
	repo := magi.NewApprovalRepository(openApprovalDB(t))
	ctx := context.Background()

	first := &entity.ApprovalRequest{
		CaseID: "c9", RunID: "r9", ToolName: "rollout", Arguments: `{"percent":5}`,
		IntentDigest: "digest-percent-5", InvocationID: "inv-1",
		Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.FindByInvocation(ctx, "c9", "inv-1")
	if err != nil || got == nil || got.ID != first.ID {
		t.Fatalf("find by invocation: got=%+v err=%v", got, err)
	}

	// A concurrent writer on the same invocation must not create a second
	// authoritative request; it is told to re-read the winner.
	dup := &entity.ApprovalRequest{
		CaseID: "c9", RunID: "r9", ToolName: "rollout", Arguments: `{"percent":5}`,
		IntentDigest: "digest-percent-5", InvocationID: "inv-1",
		Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, dup); !errors.Is(err, port.ErrApprovalConflict) {
		t.Fatalf("a second request for the same invocation must conflict, got %v", err)
	}

	// A different invocation that happens to use the same tool and identical
	// arguments is a different logical call and gets its own request.
	second := &entity.ApprovalRequest{
		CaseID: "c9", RunID: "r9", ToolName: "rollout", Arguments: `{"percent":5}`,
		IntentDigest: "digest-percent-5", InvocationID: "inv-2",
		Status: entity.ApprovalPending, RequestedAt: time.Now(),
	}
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("a different invocation must be creatable: %v", err)
	}
	list, err := repo.List(ctx, "c9")
	if err != nil || len(list) != 2 {
		t.Fatalf("expected exactly two authoritative requests, got %d (%v)", len(list), err)
	}
}

// A decision persisted before invocation binding existed has no invocation and
// must never be resolved by the invocation lookup.
func TestApprovalRepository_LegacyRowWithoutInvocationIsNeverReused(t *testing.T) {
	db := openApprovalDB(t)
	repo := magi.NewApprovalRepository(db)
	ctx := context.Background()
	now := time.Now()
	if err := db.Exec(`INSERT INTO magi_approval_request
		(id, case_id, run_id, agent_code, tool_name, arguments, intent_digest, status, reason, decided_by, requested_at, created_at)
		VALUES ('appr-legacy-null', 'c10', 'r10', 'melchior', 'rollout', '{"percent":5}', 'digest-percent-5', 'approved', 'ok', 'human-1', ?, ?)`,
		now, now).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	if got, err := repo.FindByInvocation(ctx, "c10", "inv-any"); err != nil || got != nil {
		t.Fatalf("a row without an invocation must never be reused: got=%+v err=%v", got, err)
	}
}
