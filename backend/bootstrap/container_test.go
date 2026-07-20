package bootstrap_test

import (
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAllModels_MigrateWithoutError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModels()...); err != nil {
		t.Fatalf("auto-migrate all models: %v", err)
	}
	for _, m := range magi.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("table for %T not created", m)
		}
	}
}
