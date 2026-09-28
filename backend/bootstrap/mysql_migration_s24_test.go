package bootstrap

import (
	"testing"

	"gorm.io/gorm"
)

// preS24ApprovalDDL is the approval table as S10 published it, before the
// invocation identity and intent digest existed. TEXT columns omit MySQL's
// illegal literal default so the fixture itself is valid on MySQL.
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

func approvalBindingShape(t *testing.T, db *gorm.DB, column string) (width int, present bool) {
	t.Helper()
	var row struct {
		MaxLength *int64 `gorm:"column:max_length"`
	}
	if err := db.Raw(`SELECT CHARACTER_MAXIMUM_LENGTH AS max_length
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'magi_approval_request' AND COLUMN_NAME = ?`, column).
		Scan(&row).Error; err != nil {
		t.Fatalf("read %s column: %v", column, err)
	}
	if row.MaxLength == nil {
		return 0, false
	}
	return int(*row.MaxLength), true
}

func hasApprovalInvocationUniqueIndex(t *testing.T, db *gorm.DB) bool {
	t.Helper()
	indexes, err := uniqueIndexColumns(db, "magi_approval_request")
	if err != nil {
		t.Fatalf("read unique indexes: %v", err)
	}
	for _, columns := range indexes {
		if len(columns) == 2 && columns[0] == "case_id" && columns[1] == "invocation_id" {
			return true
		}
	}
	return false
}

// seedPreS24ApprovalTable inserts two decisions for the same case and run. They
// predate the invocation identity entirely, which is exactly the shape the
// upgrade has to tolerate: both rows carry a NULL invocation, so the new unique
// key must not collide them.
func seedPreS24ApprovalTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(preS24ApprovalDDL).Error; err != nil {
		t.Fatalf("seed pre-S24 approval table: %v", err)
	}
	for _, id := range []string{"appr-legacy-1", "appr-legacy-2"} {
		if err := db.Exec(`INSERT INTO magi_approval_request
			(id, case_id, run_id, agent_code, tool_name, arguments, status, reason, decided_by)
			VALUES (?, 'case-legacy', 'run-legacy', 'melchior', 'rollout', '{"percent":5}', 'approved', 'looks fine', 'human-1')`, id).Error; err != nil {
			t.Fatalf("seed legacy approval row %s: %v", id, err)
		}
	}
}

func assertApprovalBinding(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, column := range []string{"invocation_id", "intent_digest"} {
		width, present := approvalBindingShape(t, db, column)
		if !present {
			t.Fatalf("approval table is missing %s after migration", column)
		}
		if width != 64 {
			t.Fatalf("%s width = %d, want 64", column, width)
		}
	}
	if !hasApprovalInvocationUniqueIndex(t, db) {
		t.Fatal("approval table is missing UNIQUE (case_id, invocation_id)")
	}
}

func assertLegacyApprovalsSurviveUpgrade(t *testing.T, db *gorm.DB) {
	t.Helper()
	var nullInvocations int64
	if err := db.Raw(`SELECT COUNT(*) FROM magi_approval_request
		WHERE case_id = 'case-legacy' AND invocation_id IS NULL`).Scan(&nullInvocations).Error; err != nil {
		t.Fatalf("count legacy rows: %v", err)
	}
	if nullInvocations != 2 {
		t.Fatalf("both pre-S24 rows must survive with a NULL invocation, got %d", nullInvocations)
	}
}

// assertInvocationUniqueness pins the constraint the runtime relies on: one
// logical invocation may own at most one authoritative request, while distinct
// invocations stay independent and legacy NULLs stay tolerable.
func assertInvocationUniqueness(t *testing.T, db *gorm.DB) {
	t.Helper()
	insert := func(id string, invocation any) error {
		return db.Exec(`INSERT INTO magi_approval_request
			(id, case_id, run_id, agent_code, tool_name, arguments, intent_digest, invocation_id, status, reason, decided_by)
			VALUES (?, 'case-uniq', 'run-uniq', 'melchior', 'rollout', '{"percent":5}', 'digest', ?, 'pending', '', '')`, id, invocation).Error
	}
	if err := insert("appr-u1", "inv-shared"); err != nil {
		t.Fatalf("first request for an invocation: %v", err)
	}
	if err := insert("appr-u2", "inv-shared"); err == nil {
		t.Fatal("a second request for the same invocation must violate the unique key")
	}
	if err := insert("appr-u3", "inv-other"); err != nil {
		t.Fatalf("a different invocation must be insertable: %v", err)
	}
	if err := insert("appr-u4", nil); err != nil {
		t.Fatalf("repeated legacy NULL invocations must stay insertable: %v", err)
	}
	if err := insert("appr-u5", nil); err != nil {
		t.Fatalf("repeated legacy NULL invocations must stay insertable: %v", err)
	}
}

// TestMySQLMigration_S24BindsApprovalToInvocation covers issue #9 on real MySQL.
// The authoritative approval identity became the logical invocation, with a
// unique key that must hold under a concurrent create, tolerate the legacy rows
// that predate it, and stay identical across both upgrade paths an existing
// database can take: startup AutoMigrate and the explicit forward-only script.
func TestMySQLMigration_S24BindsApprovalToInvocation(t *testing.T) {
	dsn := newMySQLSchema(t)
	admin := openSchema(t, dsn)
	seedPreS24ApprovalTable(t, admin)
	if _, present := approvalBindingShape(t, admin, "invocation_id"); present {
		t.Fatal("pre-S24 approval table must not have invocation_id")
	}

	// Path 1: the normal startup path adds the binding, and restarting the
	// service against the upgraded schema stays idempotent.
	started := provideDBForTest(t, dsn)
	assertApprovalBinding(t, started)
	assertLegacyApprovalsSurviveUpgrade(t, started)
	assertInvocationUniqueness(t, started)
	restarted := provideDBForTest(t, dsn)
	assertApprovalBinding(t, restarted)

	// Path 2: an operator who applies the Atlas script instead of relying on
	// startup gets the same shape and the same legacy tolerance.
	fresh := newMySQLSchema(t)
	scripted := openSchema(t, fresh)
	seedPreS24ApprovalTable(t, scripted)
	applyMySQLScript(t, scripted, s24MigrationPath)
	assertApprovalBinding(t, scripted)
	assertLegacyApprovalsSurviveUpgrade(t, scripted)
	assertInvocationUniqueness(t, scripted)
}
