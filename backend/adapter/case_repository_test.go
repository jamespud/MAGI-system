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

func openCaseDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCaseRepository_ListPagedScopesByUserAndCounts(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	cr := repo.CaseRepo()
	ctx := context.Background()

	base := time.Now().Add(-time.Hour)
	for i, uid := range []int64{1, 1, 2} {
		c := &entity.DecisionCase{ID: "case-paged-" + string(rune('a'+i)), UserID: uid, Question: "q", CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := cr.Create(ctx, c); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	// User 1 owns 2 cases.
	cases, total, err := cr.ListPaged(ctx, 1, 1, 10)
	if err != nil {
		t.Fatalf("list paged: %v", err)
	}
	if total != 2 || len(cases) != 2 {
		t.Fatalf("user 1: total=%d len=%d", total, len(cases))
	}
	// User 2 owns 1 case.
	if _, total2, _ := cr.ListPaged(ctx, 2, 1, 10); total2 != 1 {
		t.Fatalf("user 2 total=%d", total2)
	}
	// Page size respected.
	if _, total3, _ := cr.ListPaged(ctx, 1, 1, 1); total3 != 2 {
		t.Fatalf("paging total=%d", total3)
	}
}

func TestCaseRepository_UpdateFlagsAndDelete(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	cr := repo.CaseRepo()
	ctx := context.Background()

	c := &entity.DecisionCase{ID: "case-flags", UserID: 1, Question: "q", CreatedAt: time.Now()}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatalf("create: %v", err)
	}
	pinned, archived := true, true
	if err := cr.UpdateFlags(ctx, c.ID, &pinned, &archived); err != nil {
		t.Fatalf("update flags: %v", err)
	}
	got, err := cr.Get(ctx, c.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Pinned || !got.Archived {
		t.Fatalf("flags not persisted: %+v", got)
	}
	if err := cr.Delete(ctx, c.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got2, _ := cr.Get(ctx, c.ID); got2 != nil {
		t.Fatal("case should be deleted")
	}
}

func TestAgentRunRepo_CountByUser(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	cr := repo.CaseRepo()
	ar := repo.AgentRunRepo()
	c := &entity.DecisionCase{ID: "case-count", UserID: 5, Question: "q", CreatedAt: time.Now()}
	if err := cr.Create(ctx, c); err != nil {
		t.Fatalf("create case: %v", err)
	}
	for i := 0; i < 3; i++ {
		r := &entity.AgentRun{ID: "run-" + string(rune('a'+i)), CaseID: c.ID, Status: entity.AgentRunStatusCompleted}
		if err := ar.Create(ctx, r); err != nil {
			t.Fatalf("create run: %v", err)
		}
	}
	n, err := ar.CountByUser(ctx, 5)
	if err != nil {
		t.Fatalf("count by user: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 runs for user 5, got %d", n)
	}
}

// TestCaseRepository_DeleteRemovesCursorAndA2ABinding guards that deleting a
// Case also removes its event cursor and A2A submission binding, so a stale
// cursor can never block startup and a binding cannot dangle on a deleted task.
func TestCaseRepository_DeleteRemovesCursorAndA2ABinding(t *testing.T) {
	db := openCaseDB(t)
	repo := magi.NewRepository(db)
	cr := repo.CaseRepo()
	ctx := context.Background()

	victimID := "case-victim"
	keepID := "case-keep"
	for _, id := range []string{victimID, keepID} {
		if err := cr.Create(ctx, &entity.DecisionCase{ID: id, UserID: 9, Question: "q", CreatedAt: time.Now()}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	if err := db.Create(&magi.EventModel{ID: "ev-victim", CaseID: victimID, Seq: 1, Type: string(entity.EventCaseStatusChanged), Timestamp: time.Now()}).Error; err != nil {
		t.Fatalf("seed victim event: %v", err)
	}
	if err := db.Create(&magi.EventCursorModel{CaseID: victimID, NextSeq: 2}).Error; err != nil {
		t.Fatalf("seed victim cursor: %v", err)
	}
	if err := db.Create(&magi.A2ASubmissionModel{ID: "sub-victim", UserID: 9, MessageID: "msg-victim", RequestHash: "h", TaskID: victimID, ContextID: "ctx-v", State: "STARTED"}).Error; err != nil {
		t.Fatalf("seed victim submission: %v", err)
	}
	if err := db.Create(&magi.EventModel{ID: "ev-keep", CaseID: keepID, Seq: 1, Type: string(entity.EventCaseStatusChanged), Timestamp: time.Now()}).Error; err != nil {
		t.Fatalf("seed keep event: %v", err)
	}
	if err := db.Create(&magi.EventCursorModel{CaseID: keepID, NextSeq: 2}).Error; err != nil {
		t.Fatalf("seed keep cursor: %v", err)
	}
	if err := db.Create(&magi.A2ASubmissionModel{ID: "sub-keep", UserID: 9, MessageID: "msg-keep", RequestHash: "h2", TaskID: keepID, ContextID: "ctx-k", State: "STARTED"}).Error; err != nil {
		t.Fatalf("seed keep submission: %v", err)
	}

	if err := cr.Delete(ctx, victimID); err != nil {
		t.Fatalf("delete victim: %v", err)
	}

	var victimEvent, victimCursor, victimSub int64
	db.Model(&magi.EventModel{}).Where("case_id = ?", victimID).Count(&victimEvent)
	db.Model(&magi.EventCursorModel{}).Where("case_id = ?", victimID).Count(&victimCursor)
	db.Model(&magi.A2ASubmissionModel{}).Where("task_id = ?", victimID).Count(&victimSub)
	if victimEvent != 0 || victimCursor != 0 || victimSub != 0 {
		t.Fatalf("victim left rows: event=%d cursor=%d submission=%d", victimEvent, victimCursor, victimSub)
	}

	var keepEvent, keepCursor, keepSub int64
	db.Model(&magi.EventModel{}).Where("case_id = ?", keepID).Count(&keepEvent)
	db.Model(&magi.EventCursorModel{}).Where("case_id = ?", keepID).Count(&keepCursor)
	db.Model(&magi.A2ASubmissionModel{}).Where("task_id = ?", keepID).Count(&keepSub)
	if keepEvent != 1 || keepCursor != 1 || keepSub != 1 {
		t.Fatalf("keep rows lost: event=%d cursor=%d submission=%d", keepEvent, keepCursor, keepSub)
	}
}
