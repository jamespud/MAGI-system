package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
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
