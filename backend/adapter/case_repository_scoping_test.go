package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestCaseRepository_UpdatePausedRoundtrip(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	cr := repo.CaseRepo()
	if err := cr.Create(ctx, &entity.DecisionCase{
		ID: "c-pause", Question: "q", Status: entity.CaseStatusInvestigating,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	writer, ok := cr.(port.PauseStatusWriter)
	if !ok {
		t.Fatal("case repo must implement PauseStatusWriter")
	}
	if err := writer.UpdatePaused(ctx, "c-pause", entity.CaseStatusPaused, entity.CaseStatusInvestigating); err != nil {
		t.Fatalf("pause: %v", err)
	}
	c, err := cr.Get(ctx, "c-pause")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if c.Status != entity.CaseStatusPaused || c.PausedFromStatus != entity.CaseStatusInvestigating {
		t.Fatalf("paused case = %+v", c)
	}
	if err := writer.UpdatePaused(ctx, "c-pause", entity.CaseStatusInvestigating, ""); err != nil {
		t.Fatalf("resume: %v", err)
	}
	c, err = cr.Get(ctx, "c-pause")
	if err != nil {
		t.Fatalf("get after resume: %v", err)
	}
	if c.Status != entity.CaseStatusInvestigating || c.PausedFromStatus != "" {
		t.Fatalf("resumed case = %+v", c)
	}
}

// TestCaseRepository_ListForUser verifies the multi-tenant, paginated listing
// is scoped in SQL: a user sees only their own cases plus unowned (owner 0)
// cases, and never rows owned by another user (P0: D2).
func TestCaseRepository_ListForUser_ScopesByOwner(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	cr := repo.CaseRepo()

	create := func(id string, userID int64) {
		t.Helper()
		if err := cr.Create(ctx, &entity.DecisionCase{ID: id, UserID: userID, Question: "q " + id, Status: entity.CaseStatusDraft}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	create("c-u7-1", 7)
	create("c-u7-2", 7)
	create("c-u8-1", 8)
	create("c-open", 0)

	// user 7 sees own + open, not user 8's
	list, err := cr.(port.CaseListFilter).
		ListForUser(ctx, 7, 100, 0)
	if err != nil {
		t.Fatalf("list user 7: %v", err)
	}
	got := map[string]bool{}
	for _, c := range list {
		got[c.ID] = true
	}
	if len(list) != 3 {
		t.Fatalf("user 7 expected 3 cases (2 own + 1 open), got %d: %+v", len(list), got)
	}
	if !got["c-u7-1"] || !got["c-u7-2"] || !got["c-open"] {
		t.Fatalf("user 7 missing expected cases: %+v", got)
	}
	if got["c-u8-1"] {
		t.Fatalf("user 7 must NOT see user 8's case: %+v", got)
	}

	// user 8 sees own + open, not user 7's
	list8, err := cr.(port.CaseListFilter).
		ListForUser(ctx, 8, 100, 0)
	if err != nil {
		t.Fatalf("list user 8: %v", err)
	}
	if len(list8) != 2 {
		t.Fatalf("user 8 expected 2 cases (1 own + 1 open), got %d", len(list8))
	}

	// open mode (userID 0) sees everything
	listOpen, err := cr.(port.CaseListFilter).
		ListForUser(ctx, 0, 100, 0)
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	if len(listOpen) != 4 {
		t.Fatalf("open mode expected 4 cases, got %d", len(listOpen))
	}
}

func TestCaseRepository_ListForUser_Paginates(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	cr := repo.CaseRepo()

	for i := 0; i < 7; i++ {
		id := "p-" + string(rune('a'+i))
		if err := cr.Create(ctx, &entity.DecisionCase{ID: id, UserID: 7, Question: "q " + id, Status: entity.CaseStatusDraft}); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	filter := cr.(port.CaseListFilter)

	page1, err := filter.ListForUser(ctx, 7, 3, 0)
	if err != nil || len(page1) != 3 {
		t.Fatalf("page1: %v %d", err, len(page1))
	}
	page2, err := filter.ListForUser(ctx, 7, 3, 3)
	if err != nil || len(page2) != 3 {
		t.Fatalf("page2: %v %d", err, len(page2))
	}
	page3, err := filter.ListForUser(ctx, 7, 3, 6)
	if err != nil || len(page3) != 1 {
		t.Fatalf("page3: %v %d", err, len(page3))
	}
	// pages are disjoint and ordered newest-first
	seen := map[string]bool{}
	for _, p := range [][]*entity.DecisionCase{page1, page2, page3} {
		for _, c := range p {
			if seen[c.ID] {
				t.Fatalf("duplicate id across pages: %s", c.ID)
			}
			seen[c.ID] = true
		}
	}
	if len(seen) != 7 {
		t.Fatalf("expected 7 unique cases across pages, got %d", len(seen))
	}
}

// TestMemoryRepository_Search_LIKE verifies the deterministic LIKE fallback
// search returns matching projections. Ownership scoping lives in the memory
// service (application layer), not this repository (P1 design).
func TestMemoryRepository_Search_LIKE(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()

	mr := repo.MemoryRepo()
	for _, id := range []string{"m-one", "m-two"} {
		if err := mr.Save(ctx, &entity.CaseMemoryProjection{CaseID: id, QuestionSummary: "shared keyword", Resolution: "approve"}); err != nil {
			t.Fatalf("save projection: %v", err)
		}
	}
	res, err := mr.Search(ctx, "shared", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 LIKE matches, got %d: %+v", len(res), res)
	}
	resMiss, err := mr.Search(ctx, "no-such-term", 10)
	if err != nil || len(resMiss) != 0 {
		t.Fatalf("expected 0 matches, got %d err=%v", len(resMiss), err)
	}
}

func TestMemoryRepository_AnnotationTagsAndDelete(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewRepository(db)
	ctx := context.Background()
	mr := repo.MemoryRepo()

	proj := &entity.CaseMemoryProjection{
		CaseID:            "memory-edit",
		QuestionSummary:   "database choice",
		ContextSummary:    "latency matters",
		Resolution:        "use postgres",
		Annotation:        "verified by SRE",
		Tags:              []string{"ops", "database"},
		Outcome:           &entity.CaseOutcome{Status: "resolved", Learned: "use managed postgres"},
		ProjectionVersion: 3,
	}
	if err := mr.Save(ctx, proj); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := mr.Get(ctx, proj.CaseID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.Annotation != proj.Annotation || len(loaded.Tags) != 2 || loaded.Outcome.Learned != proj.Outcome.Learned {
		t.Fatalf("roundtrip: %+v", loaded)
	}

	byTag, err := mr.Search(ctx, "database", 10)
	if err != nil || len(byTag) != 1 {
		t.Fatalf("tag search: len=%d err=%v", len(byTag), err)
	}
	byAnnotation, err := mr.Search(ctx, "verified", 10)
	if err != nil || len(byAnnotation) != 1 {
		t.Fatalf("annotation search: len=%d err=%v", len(byAnnotation), err)
	}

	if err := mr.Delete(ctx, proj.CaseID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	deleted, err := mr.Get(ctx, proj.CaseID)
	if err == nil || deleted != nil {
		t.Fatalf("expected deleted memory, got=%v err=%v", deleted, err)
	}
}
