package bootstrap

import (
	"testing"

	"gorm.io/gorm"
)

// preS25CaseDDL and preS25JobDDL are the S24-era shapes of the two tables S25
// touches. Only the columns the assertions read are declared: the Atlas path
// only adds the generation column, and the AutoMigrate path is expected to
// reconcile the rest on its own.
const preS25CaseDDL = `
CREATE TABLE decision_case (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    question TEXT,
    status VARCHAR(32) NOT NULL DEFAULT '',
    created_at DATETIME NULL,
    updated_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

const preS25JobDDL = `
CREATE TABLE decision_job (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT '',
    attempt INT NOT NULL DEFAULT 0,
    created_at DATETIME NULL,
    updated_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// seedPreS25Tables creates the S24-era tables and one legacy Case with its job,
// so the upgrade has real rows to preserve.
func seedPreS25Tables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, ddl := range []string{preS25CaseDDL, preS25JobDDL} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("seed pre-S25 table: %v", err)
		}
	}
	if err := db.Exec(`INSERT INTO decision_case (id, question, status) VALUES ('case-legacy-s25', 'legacy question', 'RESOLVED')`).Error; err != nil {
		t.Fatalf("seed legacy case: %v", err)
	}
	if err := db.Exec(`INSERT INTO decision_job (id, case_id, status, attempt) VALUES ('job-legacy-s25', 'case-legacy-s25', 'succeeded', 2)`).Error; err != nil {
		t.Fatalf("seed legacy job: %v", err)
	}
}

// assertExecutionGenerationShape pins the migrated column shape on both tables.
func assertExecutionGenerationShape(t *testing.T, db *gorm.DB, table string) {
	t.Helper()
	var row struct {
		DataType   string  `gorm:"column:data_type"`
		IsNullable string  `gorm:"column:is_nullable"`
		ColumnDef  *string `gorm:"column:column_default"`
		ColumnType string  `gorm:"column:column_type"`
	}
	if err := db.Raw(`SELECT DATA_TYPE AS data_type, IS_NULLABLE AS is_nullable,
			COLUMN_DEFAULT AS column_default, COLUMN_TYPE AS column_type
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'execution_generation'`, table).
		Scan(&row).Error; err != nil {
		t.Fatalf("read %s.execution_generation: %v", table, err)
	}
	if row.DataType == "" {
		t.Fatalf("%s is missing execution_generation", table)
	}
	if row.DataType != "bigint" {
		t.Fatalf("%s.execution_generation type = %q, want bigint", table, row.ColumnType)
	}
	if row.IsNullable != "NO" {
		t.Fatalf("%s.execution_generation must be NOT NULL, got %q", table, row.IsNullable)
	}
	if row.ColumnDef == nil || *row.ColumnDef != "0" {
		t.Fatalf("%s.execution_generation default = %v, want 0", table, row.ColumnDef)
	}
}

// assertLegacyRowsAtGenerationZero proves the upgrade neither backfills a
// positive generation nor disturbs the pre-existing fields.
func assertLegacyRowsAtGenerationZero(t *testing.T, db *gorm.DB) {
	t.Helper()
	var c struct {
		Generation int64  `gorm:"column:execution_generation"`
		Status     string `gorm:"column:status"`
		Question   string `gorm:"column:question"`
	}
	if err := db.Raw(`SELECT execution_generation, status, question FROM decision_case WHERE id = 'case-legacy-s25'`).Scan(&c).Error; err != nil {
		t.Fatalf("read migrated case: %v", err)
	}
	if c.Generation != 0 || c.Status != "RESOLVED" || c.Question != "legacy question" {
		t.Fatalf("migrated case changed or was backfilled: %+v", c)
	}

	var j struct {
		Generation int64  `gorm:"column:execution_generation"`
		Status     string `gorm:"column:status"`
		Attempt    int    `gorm:"column:attempt"`
	}
	if err := db.Raw(`SELECT execution_generation, status, attempt FROM decision_job WHERE id = 'job-legacy-s25'`).Scan(&j).Error; err != nil {
		t.Fatalf("read migrated job: %v", err)
	}
	if j.Generation != 0 || j.Status != "succeeded" || j.Attempt != 2 {
		t.Fatalf("migrated job changed or was backfilled: %+v", j)
	}

	// generation 0 must round-trip unchanged.
	if err := db.Exec(`UPDATE decision_case SET execution_generation = 0 WHERE id = 'case-legacy-s25'`).Error; err != nil {
		t.Fatalf("round-trip update: %v", err)
	}
	var got int64
	if err := db.Raw(`SELECT execution_generation FROM decision_case WHERE id = 'case-legacy-s25'`).Scan(&got).Error; err != nil {
		t.Fatalf("round-trip read: %v", err)
	}
	if got != 0 {
		t.Fatalf("generation 0 did not round-trip: %d", got)
	}
}

// T1 (#19): real MySQL must show that the S25 upgrade preserves existing rows at
// generation 0, and that the Atlas script and the startup AutoMigrate converge
// on the same column shape.
func TestMySQLMigration_S25PersistsExecutionGeneration(t *testing.T) {
	// Path 1: S24-shaped schema with real rows, upgraded by the Atlas script.
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS25Tables(t, admin)
	applyMySQLScript(t, admin, s25MigrationPath)
	for _, table := range []string{"decision_case", "decision_job"} {
		assertExecutionGenerationShape(t, admin, table)
	}
	assertLegacyRowsAtGenerationZero(t, admin)

	// Path 2: the startup AutoMigrate over the same upgraded schema agrees with
	// the script.
	upgraded := provideDBForTest(t, dsn)
	for _, table := range []string{"decision_case", "decision_job"} {
		assertExecutionGenerationShape(t, upgraded, table)
	}
	assertLegacyRowsAtGenerationZero(t, upgraded)

	// Path 3: a fresh schema reaches the same shape.
	fresh := newMySQLSchema(t)
	started := provideDBForTest(t, fresh)
	for _, table := range []string{"decision_case", "decision_job"} {
		assertExecutionGenerationShape(t, started, table)
	}
}
