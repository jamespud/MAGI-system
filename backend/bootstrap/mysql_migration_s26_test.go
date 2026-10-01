package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"gorm.io/gorm"
)

const preS26JobDDL = `
CREATE TABLE decision_job (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT '',
    attempt INT NOT NULL DEFAULT 0,
    execution_generation BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME NULL,
    updated_at DATETIME NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func assertClaimTokenShape(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row struct {
		DataType   string `gorm:"column:data_type"`
		IsNullable string `gorm:"column:is_nullable"`
		MaxLength  int64  `gorm:"column:max_length"`
	}
	if err := db.Raw(`SELECT DATA_TYPE AS data_type, IS_NULLABLE AS is_nullable,
			CHARACTER_MAXIMUM_LENGTH AS max_length
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'decision_job' AND COLUMN_NAME = 'claim_token'`).
		Scan(&row).Error; err != nil {
		t.Fatalf("read claim_token shape: %v", err)
	}
	if row.DataType != "varchar" || row.IsNullable != "YES" || row.MaxLength != 36 {
		t.Fatalf("claim_token shape = %+v, want varchar(36) nullable", row)
	}
	indexes, err := uniqueIndexColumns(db, "decision_job")
	if err != nil {
		t.Fatalf("read claim_token index: %v", err)
	}
	found := false
	for _, cols := range indexes {
		if len(cols) == 1 && cols[0] == "claim_token" {
			found = true
		}
	}
	if !found {
		t.Fatal("decision_job missing unique claim_token index")
	}
}

func mysqlTableFingerprint(t *testing.T, db *gorm.DB, table string) string {
	t.Helper()
	var cols []struct {
		ColumnName string `gorm:"column:column_name"`
		ColumnType string `gorm:"column:column_type"`
		IsNullable string `gorm:"column:is_nullable"`
		DefaultVal string `gorm:"column:default_value"`
		Extra      string `gorm:"column:extra"`
	}
	if err := db.Raw(`SELECT COLUMN_NAME AS column_name, COLUMN_TYPE AS column_type,
			IS_NULLABLE AS is_nullable, COALESCE(COLUMN_DEFAULT, '<NULL>') AS default_value,
			EXTRA AS extra
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&cols).Error; err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	var idx []struct {
		IndexName  string `gorm:"column:index_name"`
		NonUnique  int    `gorm:"column:non_unique"`
		SeqInIndex int    `gorm:"column:seq_in_index"`
		ColumnName string `gorm:"column:column_name"`
	}
	if err := db.Raw(`SELECT INDEX_NAME AS index_name, NON_UNIQUE AS non_unique,
			SEQ_IN_INDEX AS seq_in_index, COLUMN_NAME AS column_name
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&idx).Error; err != nil {
		t.Fatalf("read %s indexes: %v", table, err)
	}
	lines := make([]string, 0, len(cols)+len(idx))
	for _, c := range cols {
		lines = append(lines, fmt.Sprintf("col|%s|%s|%s|%s|%s",
			c.ColumnName, c.ColumnType, c.IsNullable, c.DefaultVal, c.Extra))
	}
	for _, i := range idx {
		lines = append(lines, fmt.Sprintf("idx|%s|%d|%d|%s",
			i.IndexName, i.NonUnique, i.SeqInIndex, i.ColumnName))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestMySQLMigration_S26AddsClaimRecoveryIdentity(t *testing.T) {
	dsn := newMySQLSchema(t)
	db := openSchema(t, dsn)
	if err := db.Exec(preS26JobDDL).Error; err != nil {
		t.Fatalf("seed pre-S26 job table: %v", err)
	}
	if err := db.Exec(`INSERT INTO decision_job (id, case_id, status) VALUES
		('legacy-1','case-1','succeeded'),('legacy-2','case-2','failed')`).Error; err != nil {
		t.Fatalf("seed legacy jobs: %v", err)
	}
	applyMySQLScript(t, db, s26MigrationPath)
	assertClaimTokenShape(t, db)
	scriptedClaimRegistry := mysqlTableFingerprint(t, db, "decision_job_claim")
	if !db.Migrator().HasTable(&magi.DecisionJobClaimModel{}) {
		t.Fatal("S26 scripted migration missing decision_job_claim registry")
	}

	var nulls int64
	if err := db.Raw(`SELECT COUNT(*) FROM decision_job WHERE claim_token IS NULL`).Scan(&nulls).Error; err != nil {
		t.Fatalf("count legacy NULL tokens: %v", err)
	}
	if nulls != 2 {
		t.Fatalf("legacy NULL claim_token rows = %d, want 2", nulls)
	}
	jobs := magi.NewDecisionJobRepository(db)
	if recovered, err := jobs.GetByClaimToken(context.Background(), uuid.NewString()); !errors.Is(err, gorm.ErrRecordNotFound) || recovered != nil {
		t.Fatalf("legacy NULL-token row recovered by token lookup: job=%+v err=%v", recovered, err)
	}

	token := "11111111-1111-4111-8111-111111111111"
	if err := db.Exec(`UPDATE decision_job SET claim_token=? WHERE id='legacy-1'`, token).Error; err != nil {
		t.Fatalf("write first token: %v", err)
	}
	if err := db.Exec(`UPDATE decision_job SET claim_token=? WHERE id='legacy-2'`, token).Error; err == nil {
		t.Fatal("duplicate claim_token must violate unique index")
	}

	fresh := newMySQLSchema(t)
	started := provideDBForTest(t, fresh)
	assertClaimTokenShape(t, started)
	if !started.Migrator().HasColumn(&magi.DecisionJobModel{}, "claim_token") {
		t.Fatal("fresh AutoMigrate missing claim_token")
	}
	if !started.Migrator().HasTable(&magi.DecisionJobClaimModel{}) {
		t.Fatal("fresh AutoMigrate missing decision_job_claim registry")
	}
	freshClaimRegistry := mysqlTableFingerprint(t, started, "decision_job_claim")
	if freshClaimRegistry != scriptedClaimRegistry {
		t.Fatalf("decision_job_claim schema drift between S26 and fresh AutoMigrate:\n--- scripted ---\n%s\n--- fresh ---\n%s",
			scriptedClaimRegistry, freshClaimRegistry)
	}
}
