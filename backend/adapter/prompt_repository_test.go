package magi_test

import (
	"context"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openPromptDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&magi.PromptTemplateModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestPromptRepository_VersioningAndRestore(t *testing.T) {
	repo := magi.NewPromptRepository(openPromptDB(t))
	ctx := context.Background()

	if tpl, _ := repo.Get(ctx, "commander.normalize"); tpl != nil {
		t.Fatal("expected no active template before seed")
	}

	// First save creates v1 and marks it active.
	v1, err := repo.Save(ctx, "commander.normalize", "v1 content")
	if err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if v1.Version != 1 || !v1.Active {
		t.Fatalf("expected v1 active, got %+v", v1)
	}

	// Update bumps to v2 and flips active; v1 becomes inactive.
	v2, err := repo.Save(ctx, "commander.normalize", "v2 content")
	if err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if v2.Version != 2 || !v2.Active {
		t.Fatalf("expected v2 active, got %+v", v2)
	}
	active, _ := repo.Get(ctx, "commander.normalize")
	if active == nil || active.Content != "v2 content" || active.Version != 2 {
		t.Fatalf("Get must return the active version, got %+v", active)
	}

	// Restore resets to the built-in default (keeps version number).
	def, err := repo.Restore(ctx, "commander.normalize", "default content")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if def.Content != "default content" || !def.Active || def.Version != 2 {
		t.Fatalf("restore should keep version and flip active, got %+v", def)
	}

	// List returns every version. Restore overwrote v2 in place, so there are
	// two distinct versions (v1 and v2) with v2 active.
	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(all))
	}
	active, _ = repo.Get(ctx, "commander.normalize")
	if active == nil || active.Content != "default content" || !active.Active {
		t.Fatalf("after restore the active version must be the default, got %+v", active)
	}
}

func TestPromptProvider_Load(t *testing.T) {
	repo := magi.NewPromptRepository(openPromptDB(t))
	ctx := context.Background()
	if _, err := repo.Save(ctx, "commander.report", "report template"); err != nil {
		t.Fatalf("save: %v", err)
	}
	prov := magi.NewDBPromptProvider(repo)
	if got, ok := prov.Load(ctx, "commander.report"); !ok || got != "report template" {
		t.Fatalf("provider load: %q %v", got, ok)
	}
	if _, ok := prov.Load(ctx, "missing.key"); ok {
		t.Fatal("missing key must not load")
	}
}
