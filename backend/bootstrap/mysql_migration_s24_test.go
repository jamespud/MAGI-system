package bootstrap

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// preS24ApprovalDDL is the approval table as S10 published it, before the
// intent digest existed. TEXT columns omit MySQL's illegal literal default so
// the fixture itself is valid on MySQL.
const preS24ApprovalDDL = `
CREATE TABLE magi_approval_request (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    case_id VARCHAR(64) NOT NULL,
    run_id VARCHAR(64) NOT NULL DEFAULT '',
    agent_code VARCHAR(32) NOT NULL DEFAULT '',
    tool_name VARCHAR(128) NOT NULL DEFAULT '',
    arguments TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    reason TEXT NOT NULL,
    decided_by VARCHAR(128) NOT NULL DEFAULT '',
    requested_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    decided_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_approval_case (case_id),
    INDEX idx_approval_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func approvalIntentShape(t *testing.T, db *gorm.DB) (width int, present, notNull bool) {
	t.Helper()
	var row struct {
		MaxLength  *int64 `gorm:"column:max_length"`
		IsNullable string `gorm:"column:is_nullable"`
	}
	if err := db.Raw(`SELECT CHARACTER_MAXIMUM_LENGTH AS max_length, IS_NULLABLE AS is_nullable
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_approval_request' AND COLUMN_NAME = 'intent_digest'`).
		Scan(&row).Error; err != nil {
		t.Fatalf("read intent_digest column: %v", err)
	}
	if row.MaxLength == nil {
		return 0, false, false
	}
	return int(*row.MaxLength), true, !strings.EqualFold(strings.TrimSpace(row.IsNullable), "YES")
}

func hasApprovalIntentIndex(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_approval_request' AND INDEX_NAME = 'idx_approval_intent'`).
		Scan(&n).Error; err != nil {
		t.Fatalf("read idx_approval_intent: %v", err)
	}
	return n > 0
}

func seedPreS24ApprovalTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(preS24ApprovalDDL).Error; err != nil {
		t.Fatalf("seed pre-S24 approval table: %v", err)
	}
	if err := db.Exec(`INSERT INTO magi_approval_request
		(id, case_id, run_id, agent_code, tool_name, arguments, status, reason, decided_by)
		VALUES ('appr-legacy', 'case-legacy', 'run-legacy', 'melchior', 'rollout', '{"percent":5}', 'approved', 'looks fine', 'human-1')`).Error; err != nil {
		t.Fatalf("seed legacy approval row: %v", err)
	}
}

func assertApprovalIntentBinding(t *testing.T, db *gorm.DB) {
	t.Helper()
	width, present, notNull := approvalIntentShape(t, db)
	if !present {
		t.Fatal("approval table is missing intent_digest after migration")
	}
	if width != 64 {
		t.Fatalf("intent_digest width = %d, want 64", width)
	}
	if !notNull {
		t.Fatal("intent_digest must be NOT NULL")
	}
	if _, ok := mysqlColumnDefault(t, db, "magi_approval_request", "intent_digest"); !ok {
		t.Fatal("intent_digest must declare a default so existing rows backfill")
	}
	if !hasApprovalIntentIndex(t, db) {
		t.Fatal("approval table is missing the idx_approval_intent lookup index")
	}
}

func assertLegacyApprovalIsFailClosed(t *testing.T, db *gorm.DB) {
	t.Helper()
	var digest string
	if err := db.Raw(`SELECT intent_digest FROM magi_approval_request WHERE id = 'appr-legacy'`).Row().Scan(&digest); err != nil {
		t.Fatalf("read legacy digest: %v", err)
	}
	if digest != "" {
		t.Fatalf("legacy decision must keep an empty digest, got %q", digest)
	}
}

// TestMySQLMigration_S24BindsApprovalToIntent covers issue #9 on real MySQL.
// The approval lookup key gained an intent digest so one decision can no longer
// authorize a later call to the same tool with different arguments. A pre-S24
// row keeps an empty digest and stays fail-closed (the repository refuses an
// empty lookup key), and both upgrade paths an existing database can take —
// startup AutoMigrate and the explicit forward-only Atlas script — must produce
// the same column and index.
func TestMySQLMigration_S24BindsApprovalToIntent(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS24ApprovalTable(t, admin)
	if _, present, _ := approvalIntentShape(t, admin); present {
		t.Fatal("pre-S24 approval table must not have intent_digest")
	}

	// Path 1: the normal startup path adds the binding, and restarting the
	// service against the upgraded schema stays idempotent.
	started := provideDBForTest(t, dsn)
	assertApprovalIntentBinding(t, started)
	assertLegacyApprovalIsFailClosed(t, started)
	restarted := provideDBForTest(t, dsn)
	assertApprovalIntentBinding(t, restarted)

	// Path 2: an operator who applies the Atlas script instead of relying on
	// startup gets the same shape.
	fresh := newMySQLSchema(t)
	scripted := openSchema(t, fresh)
	seedPreS24ApprovalTable(t, scripted)
	applyMySQLScript(t, scripted, s24MigrationPath)
	assertApprovalIntentBinding(t, scripted)
	assertLegacyApprovalIsFailClosed(t, scripted)
}
