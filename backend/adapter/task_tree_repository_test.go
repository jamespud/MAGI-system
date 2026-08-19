package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestTaskTreeRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.TaskNodeModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewTaskTreeRepository(db)
	ctx := context.Background()
	if err := repo.RecordAgent(ctx, "case-1", "run-1", "melchior", "completed"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := repo.RecordAgent(ctx, "case-1", "run-2", "casper", "failed"); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	nodes, err := repo.ListByCase(ctx, "case-1")
	if err != nil || len(nodes) != 2 {
		t.Fatalf("list: %v %d", err, len(nodes))
	}
	if nodes[0].Kind != entity.TaskNodeKindAgent || nodes[0].Title != "melchior" || nodes[0].Status != "completed" {
		t.Fatalf("node = %+v", nodes[0])
	}
	other, _ := repo.ListByCase(ctx, "case-other")
	if len(other) != 0 {
		t.Fatalf("other case nodes = %d", len(other))
	}
}

func TestTaskTreeRepository_RecordIsIdempotent(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.TaskNodeModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewTaskTreeRepository(db)
	ctx := context.Background()

	// same (case, run, agent) recorded twice must yield ONE node (upsert),
	// with the latest status, even after re-running a case (stable run ID).
	if err := repo.RecordAgent(ctx, "case-1", "run-1", "melchior", "completed"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := repo.RecordAgent(ctx, "case-1", "run-1", "melchior", "failed"); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := repo.RecordAgent(ctx, "case-1", "run-2", "casper", "completed"); err != nil {
		t.Fatalf("record 3: %v", err)
	}
	nodes, err := repo.ListByCase(ctx, "case-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 distinct nodes after upsert, got %d", len(nodes))
	}
	byTitle := map[string]string{}
	for _, n := range nodes {
		byTitle[n.Title] = n.Status
	}
	if byTitle["melchior"] != "failed" {
		t.Fatalf("melchior status should be updated to failed: %+v", byTitle)
	}
	if byTitle["casper"] != "completed" {
		t.Fatalf("casper status: %+v", byTitle)
	}
}

func TestTaskTreeRepository_DeleteByCase(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.TaskNodeModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewTaskTreeRepository(db)
	cleaner, ok := repo.(port.TaskTreeCleaner)
	if !ok {
		t.Fatal("repo must implement TaskTreeCleaner")
	}
	ctx := context.Background()
	if err := repo.RecordAgent(ctx, "case-1", "run-1", "melchior", "completed"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := repo.RecordAgent(ctx, "case-2", "run-1", "casper", "completed"); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := cleaner.DeleteByCase(ctx, "case-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	remaining, err := repo.ListByCase(ctx, "case-2")
	if err != nil || len(remaining) != 1 {
		t.Fatalf("case-2 nodes should remain: %v %d", err, len(remaining))
	}
	gone, _ := repo.ListByCase(ctx, "case-1")
	if len(gone) != 0 {
		t.Fatalf("case-1 nodes should be deleted, got %d", len(gone))
	}
}
