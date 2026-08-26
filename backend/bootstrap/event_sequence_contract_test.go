package bootstrap

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openEventContractDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("CREATE TABLE magi_event (id VARCHAR(64) NOT NULL PRIMARY KEY, case_id VARCHAR(64) NOT NULL, seq BIGINT NOT NULL, timestamp DATETIME)").Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestHasCaseSeqUniqueIndex_RejectsWrongColumnsDespiteName(t *testing.T) {
	db := openEventContractDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX uq_magi_event_case_seq ON magi_event (seq, case_id)").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := hasCaseSeqUniqueIndex(db, "magi_event"); err != nil || ok {
		t.Fatalf("wrong-column index accepted: ok=%v err=%v", ok, err)
	}
}

func TestHasCaseSeqUniqueIndex_RejectsNonUniqueSameName(t *testing.T) {
	db := openEventContractDB(t)
	if err := db.Exec("CREATE INDEX uq_magi_event_case_seq ON magi_event (case_id, seq)").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := hasCaseSeqUniqueIndex(db, "magi_event"); err != nil || ok {
		t.Fatalf("non-unique index accepted: ok=%v err=%v", ok, err)
	}
}

func TestHasCaseSeqUniqueIndex_AcceptsDifferentUniqueNameCorrectColumns(t *testing.T) {
	db := openEventContractDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX idx_event_case_seq ON magi_event (case_id, seq)").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := hasCaseSeqUniqueIndex(db, "magi_event"); err != nil || !ok {
		t.Fatalf("correct-column differently-named index rejected: ok=%v err=%v", ok, err)
	}
}

func TestHasCaseSeqUniqueIndex_AcceptsExactIndex(t *testing.T) {
	db := openEventContractDB(t)
	if err := db.Exec("CREATE UNIQUE INDEX uq_magi_event_case_seq ON magi_event (case_id, seq)").Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := hasCaseSeqUniqueIndex(db, "magi_event"); err != nil || !ok {
		t.Fatalf("exact index rejected: ok=%v err=%v", ok, err)
	}
}
