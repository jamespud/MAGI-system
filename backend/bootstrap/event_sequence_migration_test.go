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

func setupRawEventSchema(t *testing.T, db *gorm.DB, seqNotNull, withUnique bool, cursorNext int64) {
	t.Helper()
	if err := db.Exec("DROP TABLE IF EXISTS magi_event_cursor").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS magi_event").Error; err != nil {
		t.Fatal(err)
	}
	seqDef := "BIGINT NULL"
	if seqNotNull {
		seqDef = "BIGINT NOT NULL"
	}
	ddl := "CREATE TABLE magi_event (id VARCHAR(64) NOT NULL PRIMARY KEY, case_id VARCHAR(64) NOT NULL, seq " + seqDef + ", timestamp DATETIME)"
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatal(err)
	}
	if withUnique {
		if err := db.Exec("CREATE UNIQUE INDEX uq_magi_event_case_seq ON magi_event (case_id, seq)").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("CREATE TABLE magi_event_cursor (case_id VARCHAR(64) NOT NULL PRIMARY KEY, next_seq BIGINT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO magi_event (id, case_id, seq, timestamp) VALUES ('ev-1', 'c1', 1, CURRENT_TIMESTAMP)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO magi_event_cursor (case_id, next_seq) VALUES ('c1', ?)", cursorNext).Error; err != nil {
		t.Fatal(err)
	}
}

func TestEnsureEventSequenceSchema_S16CompleteContractPasses(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setupRawEventSchema(t, db, true, true, 2)
	if err := ensureEventSequenceSchema(db); err != nil {
		t.Fatalf("complete S16 contract must pass: %v", err)
	}
}

func TestEnsureEventSequenceSchema_RejectsNullableSeq(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setupRawEventSchema(t, db, false, true, 2)
	err = ensureEventSequenceSchema(db)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "nullable") {
		t.Fatalf("nullable seq error = %v, want nullable guidance", err)
	}
}

func TestEnsureEventSequenceSchema_RejectsMissingUniqueKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setupRawEventSchema(t, db, true, false, 2)
	err = ensureEventSequenceSchema(db)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("missing unique key error = %v, want unique guidance", err)
	}
}

func TestEnsureEventSequenceSchema_RejectsStaleCursor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setupRawEventSchema(t, db, true, true, 5)
	err = ensureEventSequenceSchema(db)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "stale") {
		t.Fatalf("stale cursor error = %v, want cursor guidance", err)
	}
}

func TestEnsureEventSequenceSchema_RejectsEventCaseWithoutCursor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	setupRawEventSchema(t, db, true, true, 2)
	if err := db.Exec("DELETE FROM magi_event_cursor WHERE case_id = ?", "c1").Error; err != nil {
		t.Fatal(err)
	}
	err = ensureEventSequenceSchema(db)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "missing cursor") {
		t.Fatalf("missing cursor error = %v, want missing cursor guidance", err)
	}
}
