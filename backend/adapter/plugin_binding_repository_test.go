package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
)

func TestPluginBindingRepository_RoundTrip(t *testing.T) {
	db := openDatasetDB(t)
	repo := magi.NewPluginBindingRepository(db)
	ctx := context.Background()

	b := &entity.PluginBinding{UserID: 7, PluginID: 11, ToolID: 22, Enabled: true}
	if err := repo.Create(ctx, b); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(ctx, b.ID)
	if err != nil || got == nil || got.PluginID != 11 || !got.Enabled {
		t.Fatalf("get: %v %+v", err, got)
	}
	list, err := repo.ListByUser(ctx, 7)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %d", err, len(list))
	}
	if err := repo.UpdateEnabled(ctx, b.ID, false); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ = repo.Get(ctx, b.ID)
	if got.Enabled {
		t.Fatal("enabled should be false")
	}
	if err := repo.Delete(ctx, b.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get(ctx, b.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
}
