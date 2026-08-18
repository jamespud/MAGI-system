package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestAuditRepository_Roundtrip(t *testing.T) {
	db := openDatasetDB(t)
	if err := db.AutoMigrate(&magi.AuditLogModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := magi.NewAuditRepository(db)
	ctx := context.Background()

	if events, total, err := repo.List(ctx, 50, 0); err != nil || total != 0 || len(events) != 0 {
		t.Fatalf("empty list = %v %d %v", events, total, err)
	}

	for i := 0; i < 3; i++ {
		if err := repo.Record(ctx, &entity.AuditEvent{
			Action: "PUT", Resource: "/admin/x", Username: "admin", Role: "admin", Status: 200,
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	events, total, err := repo.List(ctx, 50, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 3 || len(events) != 3 {
		t.Fatalf("total=%d len=%d", total, len(events))
	}
	if events[0].Action != "PUT" || events[0].Username != "admin" || events[0].Status != 200 {
		t.Fatalf("event = %+v", events[0])
	}
	if events[0].CreatedAt.IsZero() {
		t.Fatal("created_at must be set")
	}

	// Limit + offset pagination.
	page, total, err := repo.List(ctx, 2, 1)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if total != 3 || len(page) != 2 {
		t.Fatalf("page total=%d len=%d", total, len(page))
	}
}
