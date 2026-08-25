package bootstrap

import (
	"strings"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureEventSequenceSchema_FreshInstallCreatesBothTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := ensureEventSequenceSchema(db); err != nil {
		t.Fatalf("ensure fresh event schema: %v", err)
	}
	if !db.Migrator().HasTable(&magi.EventModel{}) || !db.Migrator().HasTable(&magi.EventCursorModel{}) {
		t.Fatal("fresh event schema must create both event tables")
	}
}

func TestEnsureEventSequenceSchema_BothPresentIsNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.EventModel{}, &magi.EventCursorModel{}); err != nil {
		t.Fatalf("migrate event schema: %v", err)
	}
	if err := ensureEventSequenceSchema(db); err != nil {
		t.Fatalf("ensure existing event schema: %v", err)
	}
}

func TestEnsureEventSequenceSchema_PartialSchemaRequiresAtlas(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&magi.EventModel{}); err != nil {
		t.Fatalf("migrate partial event schema: %v", err)
	}
	err = ensureEventSequenceSchema(db)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "atlas") {
		t.Fatalf("partial schema error = %v, want Atlas migration guidance", err)
	}
}
