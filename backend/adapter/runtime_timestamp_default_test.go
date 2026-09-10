package magi_test

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	magi "github.com/jamespud/magi/backend/adapter"
)

// TestRuntimeKernelSchemaIsAutoMigrated guards two P0 runtime-kernel schema
// requirements:
//
//  1. The new runtime tables must be part of AutoMigrate's model list. Local
//     dev does not run Atlas migrations, so a model missing from AllModels()
//     leaves a table that only the first real invocation discovers is absent.
//  2. The new timestamp columns need a DDL default. Adding a NOT NULL datetime
//     column without one fails on a table that already has rows ("Incorrect
//     datetime value: '0000-00-00 00:00:00'"), and a default whose
//     fractional-seconds precision does not match the column type fails as
//     "Invalid default value".
func TestRuntimeKernelSchemaIsAutoMigrated(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(magi.AllModelsWithoutEventSequence()...); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	for _, table := range []string{"runtime_invocation", "runtime_invocation_attempt", "magi_agent_checkpoint"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("AutoMigrate did not create %s; add it to AllModels()", table)
		}
	}
	for _, table := range []string{"magi_agent_checkpoint", "runtime_invocation"} {
		var ddl string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&ddl).Error; err != nil {
			t.Fatalf("read ddl for %s: %v", table, err)
		}
		lower := strings.ToLower(ddl)
		idx := strings.Index(lower, "updated_at")
		if idx < 0 {
			t.Fatalf("%s: updated_at column missing from DDL: %s", table, ddl)
		}
		tail := lower[idx:]
		if end := strings.IndexAny(tail, ",)"); end >= 0 {
			tail = tail[:end]
		}
		if !strings.Contains(tail, "default") {
			t.Fatalf("%s: updated_at column has no DDL default: %s", table, tail)
		}
	}
}
